// Package sandbox clone le dépôt de l'élève et exécute les tests dans
// un conteneur Docker isolé : pas de réseau, RAM et CPU limités, timeout
// par test. C'est la partie sensible du projet - ne jamais compiler ou
// exécuter du code élève directement sur la machine hôte.
package sandbox

import (
	"errors"
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

// Run est le models.Handler branché sur la queue. C'est le point d'entrée
// appelé par un worker pour traiter un job.
// Run est le models.Handler branché sur la queue. report est appelé après
// chaque exercice traité (compilé + testé) pour que la progression soit
// visible en temps réel côté UI, sans attendre la fin de tout le projet.
func Run(job models.Job, report func(done, total int)) (*models.Result, error) {
	def, err := testdef.Load(filepath.Join(testsDir, job.Exercise+".yaml"))
	if err != nil {
		return nil, fmt.Errorf("définition de tests introuvable pour %q: %w", job.Exercise, err)
	}

	var workDir string
	switch {
	case job.ArchivePath != "":
		defer os.Remove(job.ArchivePath) // l'archive brute n'est plus utile une fois extraite
		workDir, err = extractArchive(job.ArchivePath)
	case job.RepoURL != "":
		workDir, err = fetchSource(job.RepoURL, job.GitToken)
	default:
		return nil, fmt.Errorf("aucune source fournie (ni archive, ni repo_url)")
	}
	if err != nil {
		return nil, fmt.Errorf("récupération du code source: %w", err)
	}
	defer os.RemoveAll(workDir)

	projectRoot := resolveProjectRoot(workDir, def)

	total := len(def.Exercises)
	report(0, total)

	result := &models.Result{}
	for i, ex := range def.Exercises {
		exDir := projectRoot
		if ex.Dir != "" && ex.Dir != "." {
			exDir = filepath.Join(projectRoot, ex.Dir)
		}

		exResult := runExercise(exDir, ex, def.NormExtraRules)
		result.ExerciseResults = append(result.ExerciseResults, exResult)
		report(i+1, total)
	}

	result.Score = computeScore(result.ExerciseResults, def.Points)
	result.Passed = result.Score >= 50
	return result, nil
}

// resolveProjectRoot localise le vrai dossier racine du projet à l'intérieur
// de workDir. Cas simple : les exercices sont directement à la racine
// (upload d'une archive dédiée à ce seul projet, ou dépôt git dédié).
// Cas mono-repo : l'élève soumet le lien d'un dépôt qui regroupe TOUTE la
// piscine (un dossier par jour : C00/, C01/, ..., Rush00/, ...) - dans ce
// cas, on cherche le sous-dossier dont le nom correspond au projet demandé
// (ex: "C02" pour job.Exercise="c02") et on l'utilise comme racine à la
// place. On ne se base jamais sur la seule présence d'un dossier "ex00" :
// ce nom est réutilisé par tous les jours de piscine dans un tel dépôt, il
// faut d'abord identifier le bon dossier de PROJET.
func resolveProjectRoot(workDir string, def *testdef.ProjectDef) string {
	if hasExerciseFiles(workDir, def) {
		return workDir
	}
	if found := findProjectDir(workDir, def, 3); found != "" {
		return found
	}
	return workDir // rien trouvé : on laisse tel quel, l'erreur "Nothing turned in" habituelle s'affichera
}

// hasExerciseFiles vérifie si root contient déjà directement au moins un
// des fichiers source attendus par le projet - signe que root EST la
// racine du projet, pas un dossier parent qui l'englobe.
func hasExerciseFiles(root string, def *testdef.ProjectDef) bool {
	for _, ex := range def.Exercises {
		dir := ex.Dir
		if dir == "" {
			dir = "."
		}
		if pathExists(filepath.Join(root, dir, ex.SourceFile)) {
			return true
		}
	}
	return false
}

// findProjectDir cherche, jusqu'à maxDepth niveaux sous root, un sous-dossier
// dont le nom correspond (une fois normalisé) à l'ID ou au nom d'affichage
// du projet, ET qui contient bien les fichiers attendus. Les enfants
// immédiats sont vérifiés avant de redescendre d'un niveau.
func findProjectDir(root string, def *testdef.ProjectDef, maxDepth int) string {
	if maxDepth <= 0 {
		return ""
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return ""
	}

	wantID := normalizeProjectName(def.Project)
	wantLabel := normalizeProjectName(def.DisplayName)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := normalizeProjectName(e.Name())
		if name != wantID && (wantLabel == "" || name != wantLabel) {
			continue
		}
		candidate := filepath.Join(root, e.Name())
		if hasExerciseFiles(candidate, def) {
			return candidate
		}
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if found := findProjectDir(filepath.Join(root, e.Name()), def, maxDepth-1); found != "" {
			return found
		}
	}
	return ""
}

// normalizeProjectName réduit un nom à ses lettres/chiffres en minuscules,
// pour comparer "C02", "c02", "C 02" ou "c-02" comme équivalents.
func normalizeProjectName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func pathExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// computeScore applique le barème de la moulinette : le score correspond
// au nombre d'exercices réussis *consécutivement* depuis le premier - dès
// qu'un exercice échoue (quel que soit le statut d'échec), les exercices
// suivants ne comptent plus, même s'ils sont eux-mêmes réussis. Une triche
// détectée n'importe où dans le projet écrase tout : score = -42.
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

// runExercise compile et teste un seul exercice du projet, dans son propre
// sous-dossier (exDir). Ordre des vérifications, calqué sur la philosophie
// du sujet ("la Moulinette ne cherche pas à comprendre le code qui ne
// respecte pas la Norme") : fichier présent -> norme -> compilation ->
// fonctions autorisées -> tests fonctionnels. Une erreur à une étape
// n'affecte que CET exercice, les autres du même projet continuent.
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
	for _, extra := range ex.ExtraSources {
		if _, err := os.Stat(filepath.Join(exDir, extra)); err != nil {
			return models.ExerciseResult{
				Name:   ex.Name,
				Status: models.ExerciseMissing,
				Log:    fmt.Sprintf("fichier attendu introuvable : %s", filepath.Join(ex.Dir, extra)),
			}
		}
		compileFiles = append(compileFiles, extra)
	}
	if ex.Harness != "" {
		// Le harnais (main() de test) vit côté serveur, pas dans l'archive de
		// l'élève : on le copie dans son dossier de compilation le temps du build.
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

	studentSources := append([]string{ex.SourceFile}, ex.ExtraSources...)
	forbidden, err := checkForbiddenFunctions(exDir, studentSources, ex.AllowedFunctions)
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
	hasLeak := false
	for _, tc := range ex.Tests {
		tr := runTest(binPath, tc, ex.CheckLeaks)
		if !tr.Passed {
			allPassed = false
		}
		if tr.Error == timeoutMessage {
			hasTimeout = true
		}
		if tr.LeakLog != "" {
			hasLeak = true
		}
		exResult.TestOutput = append(exResult.TestOutput, tr)
	}

	switch {
	case hasTimeout:
		exResult.Status = models.ExerciseTimeout
	case hasLeak:
		exResult.Status = models.ExerciseLeak
	case allPassed:
		exResult.Status = models.ExerciseOK
	default:
		exResult.Status = models.ExerciseKO
	}
	return exResult
}

// copyFile copie un fichier (utilisé pour amener le harnais de test dans
// le dossier de compilation de l'exercice).
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}

// fetchSource récupère le code source à tester dans un dossier temporaire
// isolé. Accepte soit une URL git (https://... ou git@...), soit un chemin
// local (absolu, relatif, ou préfixé "file://") - pratique pour développer
// et tester sans avoir à pousser sur GitHub à chaque essai.
//
// token, s'il est non vide, sert à cloner un dépôt privé : il est transmis
// à git via GIT_ASKPASS (un petit script temporaire), jamais concaténé dans
// l'URL elle-même - sinon il apparaîtrait en clair dans la liste des
// process (`ps aux`) le temps du clone. Le jeton n'est jamais journalisé ;
// s'il apparaît dans la sortie de git (ex: message d'erreur d'auth), il est
// systématiquement retiré avant que le message ne remonte à l'appelant.
func fetchSource(source, token string) (string, error) {
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
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0") // jamais d'invite interactive bloquante

	if token != "" {
		askpass, cleanup, err := writeAskPassScript(token)
		if err != nil {
			os.RemoveAll(dir)
			return "", fmt.Errorf("préparation de l'authentification git: %w", err)
		}
		defer cleanup()
		cmd.Env = append(cmd.Env, "GIT_ASKPASS="+askpass)
	}

	if out, err := cmd.CombinedOutput(); err != nil {
		os.RemoveAll(dir)
		return "", fmt.Errorf("%s: %s", err, scrubToken(string(out), token))
	}
	return dir, nil
}

// writeAskPassScript écrit un script exécutable minimal qui répond le jeton
// à toute invite de git (nom d'utilisateur ou mot de passe) - GitHub
// accepte un jeton personnel indifféremment comme l'un ou l'autre. Le
// fichier est créé avec des permissions restreintes (0700, propriétaire
// seul) et sa suppression est renvoyée à l'appelant via cleanup.
func writeAskPassScript(token string) (path string, cleanup func(), err error) {
	f, err := os.CreateTemp("", "ft-moulinette-askpass-*.sh")
	if err != nil {
		return "", nil, err
	}
	script := "#!/bin/sh\nprintf '%s' " + shellSingleQuote(token) + "\n"
	if _, err := f.WriteString(script); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", nil, err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", nil, err
	}
	if err := os.Chmod(f.Name(), 0700); err != nil {
		os.Remove(f.Name())
		return "", nil, err
	}
	return f.Name(), func() { os.Remove(f.Name()) }, nil
}

// shellSingleQuote échappe une valeur pour une insertion sûre entre
// apostrophes dans un script shell POSIX (le jeton ne contient normalement
// que des caractères alphanumériques, mais on ne présume de rien).
func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// scrubToken retire toute occurrence du jeton d'un texte avant qu'il ne
// remonte à l'utilisateur (message d'erreur affiché dans l'UI) - le jeton
// appartient à l'utilisateur lui-même ici, mais autant ne jamais l'afficher
// en clair côté serveur (logs, etc.) par principe.
func scrubToken(text, token string) string {
	if token == "" {
		return text
	}
	return strings.ReplaceAll(text, token, "***")
}

// isLocalPath détecte les chemins locaux plutôt que les URLs git distantes.
func isLocalPath(source string) (string, bool) {
	if strings.HasPrefix(source, "file://") {
		return strings.TrimPrefix(source, "file://"), true
	}
	if strings.HasPrefix(source, "http://") ||
		strings.HasPrefix(source, "https://") ||
		strings.HasPrefix(source, "git@") {
		return "", false
	}
	// Chemin absolu (/...) ou relatif (./..., ../...) : on le traite comme local.
	if strings.HasPrefix(source, "/") || strings.HasPrefix(source, ".") {
		return source, true
	}
	return "", false
}

// copyDir copie récursivement le contenu de src vers dst (dst existe déjà).
// On passe par `cp` plutôt que de réimplémenter la copie récursive à la main.
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

// compile lance la compilation à l'intérieur du conteneur sandbox et
// renvoie le chemin du binaire produit sur l'hôte (dans dir). Accepte
// plusieurs fichiers source (le fichier de l'élève + un éventuel harnais).
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

	out, _, _, runErr := runDocker(args, 15*time.Second, nil)
	if runErr != nil {
		return "", out, runErr
	}
	return filepath.Join(dir, binName), out, nil
}

func runTest(binPath string, tc testdef.TestCase, checkLeaks bool) models.TestResult {
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
	}
	if checkLeaks {
		// --error-exitcode=42 : valgrind sort en 42 SI et seulement si une fuite
		// ou une erreur mémoire est détectée. Le rapport part sur stderr (séparé
		// du stdout comparé). --errors-for-leak-kinds=definite,indirect : on ne
		// pénalise que les vraies fuites, pas le "still reachable" (souvent des
		// allocations one-shot de la libc jamais libérées, hors de contrôle de l'élève).
		args = append(args,
			"valgrind",
			"--leak-check=full",
			"--errors-for-leak-kinds=definite,indirect",
			"--error-exitcode=42",
			"-q",
		)
	}
	args = append(args, "./"+bin)
	args = append(args, tc.Args...)

	var stdin io.Reader
	if tc.Stdin != "" {
		stdin = strings.NewReader(tc.Stdin)
	}

	got, errOut, timedOut, runErr := runDocker(args, timeout, stdin)

	tr := models.TestResult{
		Name:     tc.Name,
		Expected: tc.ExpectedOut,
		Got:      got,
	}

	if timedOut {
		tr.Error = timeoutMessage
		return tr
	}

	// Sous valgrind, le code de sortie 42 = fuite détectée. On le traite AVANT
	// la comparaison fonctionnelle : un programme correct qui fuit reste un échec.
	if checkLeaks && isLeakExit(runErr) {
		tr.Passed = false
		tr.LeakLog = errOut
		tr.Error = "fuite mémoire détectée"
		return tr
	}

	exitOK := (runErr == nil) == (tc.ExpectedExitCode == 0)
	tr.Passed = got == tc.ExpectedOut && exitOK
	if !exitOK {
		tr.Error = fmt.Sprintf("code de sortie inattendu: %v", runErr)
	}
	return tr
}

// isLeakExit vrai si le process s'est terminé avec le code 42, celui qu'on a
// demandé à valgrind d'utiliser en cas de fuite (--error-exitcode=42).
func isLeakExit(err error) bool {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode() == 42
	}
	return false
}
