package sandbox

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/tristan-reig/ft-moulinette/internal/models"
	"github.com/tristan-reig/ft-moulinette/internal/testdef"
)

const (
	sandboxImage   = "ft-moulinette-sandbox:latest" // buildée depuis Dockerfile.sandbox
	testsDir       = "tests"
	defaultTimeout = 5 * time.Second
	timeoutMessage = "timeout dépassé"
)

// Run est le models.Handler branché sur la queue. report est appelé après
// chaque exercice traité, pour une progression visible en temps réel.
func Run(job models.Job, report func(done, total int)) (*models.Result, error) {
	def, err := testdef.Load(filepath.Join(testsDir, job.Exercise+".yaml"))
	if err != nil {
		return nil, fmt.Errorf("définition de tests introuvable pour %q: %w", job.Exercise, err)
	}

	var workDir string
	switch {
	case job.ArchivePath != "":
		defer os.Remove(job.ArchivePath)
		workDir, err = extractArchive(job.ArchivePath)
	case job.RepoURL != "":
		workDir, err = fetchSource(job.RepoURL)
	default:
		return nil, fmt.Errorf("aucune source fournie (ni archive, ni repo_url)")
	}
	if err != nil {
		return nil, fmt.Errorf("récupération du code source: %w", err)
	}
	defer os.RemoveAll(workDir)

	total := len(def.Exercises)
	report(0, total)

	result := &models.Result{}
	for i, ex := range def.Exercises {
		exDir := workDir
		if ex.Dir != "" && ex.Dir != "." {
			exDir = filepath.Join(workDir, ex.Dir)
		}

		exResult := runExercise(exDir, ex, def.NormExtraRules)
		result.ExerciseResults = append(result.ExerciseResults, exResult)
		report(i+1, total)
	}

	result.Score = computeScore(result.ExerciseResults, def.Points)
	result.Passed = result.Score >= 50
	return result, nil
}

// computeScore applique le barème 42 : score = nombre d'exercices réussis
// *consécutivement* depuis le premier. Un échec stoppe le décompte même si les
// exercices suivants passent. Une triche n'importe où écrase tout : -42.
func computeScore(results []models.ExerciseResult, points []int) int {
	for _, ex := range results {
		if ex.Status == models.ExerciseCheating {
			return -42
		}
	}

	consecutiveOK := 0
	for _, ex := range results {
		if ex.Status != models.ExerciseOK {
			break
		}
		consecutiveOK++
	}

	if consecutiveOK == 0 || consecutiveOK > len(points) {
		return 0
	}
	return points[consecutiveOK-1]
}

// runExercise compile et teste un exercice dans son sous-dossier. Ordre calqué
// sur le sujet (la Moulinette ignore le code hors-norme) : présence -> norme ->
// compilation -> fonctions autorisées -> tests. Une erreur n'affecte que cet
// exercice.
func runExercise(exDir string, ex testdef.ExerciseSpec, normExtraRules []string) models.ExerciseResult {
	sourcePath := filepath.Join(exDir, ex.SourceFile)
	if _, err := os.Stat(sourcePath); err != nil {
		return models.ExerciseResult{
			Name:   ex.Name,
			Status: models.ExerciseMissing,
			Log:    fmt.Sprintf("fichier attendu introuvable : %s", filepath.Join(ex.Dir, ex.SourceFile)),
		}
	}

	if violations, err := checkNorm(exDir, ex.SourceFile, normExtraRules); err != nil {
		return models.ExerciseResult{
			Name:   ex.Name,
			Status: models.ExerciseCompileError,
			Log:    "Impossible de vérifier la norme:\n" + err.Error(),
		}
	} else if len(violations) > 0 {
		return models.ExerciseResult{
			Name:   ex.Name,
			Status: models.ExerciseNormeError,
			Log:    strings.Join(violations, "\n"),
		}
	}

	compileFiles := []string{ex.SourceFile}
	if ex.Harness != "" {
		// Le harnais (main() de test) vit côté serveur : on le copie dans le
		// dossier de compilation le temps du build.
		harnessSrc := filepath.Join(testsDir, ex.Harness)
		harnessDst := filepath.Join(exDir, "_harness.c")
		if err := copyFile(harnessSrc, harnessDst); err != nil {
			return models.ExerciseResult{
				Name:   ex.Name,
				Status: models.ExerciseCompileError,
				Log:    fmt.Sprintf("harnais de test introuvable côté serveur (%s): %v", ex.Harness, err),
			}
		}
		compileFiles = append(compileFiles, "_harness.c")
	}

	binPath, buildLog, err := compile(exDir, compileFiles...)
	if err != nil {
		return models.ExerciseResult{
			Name:   ex.Name,
			Status: models.ExerciseCompileError,
			Log:    "Échec de compilation:\n" + buildLog,
		}
	}

	forbidden, err := checkForbiddenFunctions(exDir, ex.SourceFile, ex.AllowedFunctions)
	if err != nil {
		return models.ExerciseResult{
			Name:   ex.Name,
			Status: models.ExerciseCompileError,
			Log:    "Impossible de vérifier les fonctions autorisées:\n" + err.Error(),
		}
	}
	if len(forbidden) > 0 {
		return models.ExerciseResult{
			Name:   ex.Name,
			Status: models.ExerciseCheating,
			Log:    "Fonction(s) non autorisée(s) appelée(s) : " + strings.Join(forbidden, ", "),
		}
	}

	exResult := models.ExerciseResult{Name: ex.Name}
	allPassed := true
	hasTimeout := false
	for _, tc := range ex.Tests {
		tr := runTest(binPath, tc)
		if !tr.Passed {
			allPassed = false
		}
		if tr.Error == timeoutMessage {
			hasTimeout = true
		}
		exResult.TestOutput = append(exResult.TestOutput, tr)
	}

	switch {
	case hasTimeout:
		exResult.Status = models.ExerciseTimeout
	case allPassed:
		exResult.Status = models.ExerciseOK
	default:
		exResult.Status = models.ExerciseKO
	}
	return exResult
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}

// fetchSource récupère le code dans un dossier temporaire. Accepte une URL git
// ou un chemin local (absolu, relatif, ou "file://") — pratique pour tester
// sans pousser sur GitHub à chaque essai.
func fetchSource(source string) (string, error) {
	dir, err := os.MkdirTemp("", "ft-moulinette-*")
	if err != nil {
		return "", err
	}

	if localPath, ok := isLocalPath(source); ok {
		if err := copyDir(localPath, dir); err != nil {
			os.RemoveAll(dir)
			return "", fmt.Errorf("copie de %s: %w", localPath, err)
		}
		return dir, nil
	}

	cmd := exec.Command("git", "clone", "--depth", "1", source, dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		os.RemoveAll(dir)
		return "", fmt.Errorf("%s: %s", err, out)
	}
	return dir, nil
}

func isLocalPath(source string) (string, bool) {
	if strings.HasPrefix(source, "file://") {
		return strings.TrimPrefix(source, "file://"), true
	}
	if strings.HasPrefix(source, "http://") ||
		strings.HasPrefix(source, "https://") ||
		strings.HasPrefix(source, "git@") {
		return "", false
	}
	if strings.HasPrefix(source, "/") || strings.HasPrefix(source, ".") {
		return source, true
	}
	return "", false
}

// copyDir copie récursivement le contenu de src vers dst (déjà créé) via `cp`.
func copyDir(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s n'est pas un dossier", src)
	}

	// Le "/." final copie le *contenu* de src, pas src lui-même.
	cmd := exec.Command("cp", "-r", strings.TrimSuffix(src, "/")+"/.", dst)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %s", err, out)
	}
	return nil
}

// compile lance gcc dans le conteneur et renvoie le chemin hôte du binaire.
func compile(dir string, sourceFiles ...string) (binPath string, log string, err error) {
	binName := "a.out"
	args := []string{
		"run", "--rm",
		"--network", "none",
		"--memory", "128m",
		"--cpus", "0.5",
		"--read-only",
		"-v", fmt.Sprintf("%s:/work", dir),
		"-w", "/work",
		sandboxImage,
	}
	gccArgs := append([]string{"gcc", "-Wall", "-Wextra", "-Werror", "-o", binName}, sourceFiles...)
	args = append(args, gccArgs...)

	out, _, runErr := runDocker(args, 15*time.Second, nil)
	if runErr != nil {
		return "", out, runErr
	}
	return filepath.Join(dir, binName), out, nil
}

func runTest(binPath string, tc testdef.TestCase) models.TestResult {
	timeout := defaultTimeout
	if tc.TimeoutSec > 0 {
		timeout = time.Duration(tc.TimeoutSec) * time.Second
	}

	dir, bin := filepath.Split(binPath)
	args := []string{
		"run", "--rm",
		"--network", "none",
		"--memory", "64m",
		"--cpus", "0.5",
		"-v", fmt.Sprintf("%s:/work:ro", dir),
		"-w", "/work",
		sandboxImage,
		"./" + bin,
	}
	args = append(args, tc.Args...)

	var stdin io.Reader
	if tc.Stdin != "" {
		stdin = strings.NewReader(tc.Stdin)
	}

	got, timedOut, runErr := runDocker(args, timeout, stdin)

	exitOK := (runErr == nil) == (tc.ExpectedExitCode == 0)
	passed := got == tc.ExpectedOut && exitOK

	tr := models.TestResult{
		Name:     tc.Name,
		Passed:   passed,
		Expected: tc.ExpectedOut,
		Got:      got,
	}
	if timedOut {
		tr.Error = timeoutMessage
	} else if !exitOK {
		tr.Error = fmt.Sprintf("code de sortie inattendu: %v", runErr)
	}
	return tr
}
