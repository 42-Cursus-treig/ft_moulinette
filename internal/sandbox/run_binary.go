package sandbox

import (
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/tristan-reig/ft-moulinette/internal/models"
	"github.com/tristan-reig/ft-moulinette/internal/testdef"
)

// runBinaryTests exécute les tests d'un exercice dont l'artefact est un binaire
// déjà construit (RunArtifact) présent dans exDir.
func runBinaryTests(exDir string, ex testdef.ExerciseSpec) models.ExerciseResult {
	// Déposer les fichiers d'entrée fournis par le serveur (fichiers texte,
	// binaires de test…). Copiés sous leur basename dans le dossier.
	for _, hf := range ex.HarnessFiles {
		src := filepath.Join(testsDir, hf)
		dst := filepath.Join(exDir, filepath.Base(hf))
		if err := copyFile(src, dst); err != nil {
			return compileErr(ex, fmt.Sprintf("fichier de test introuvable côté serveur (%s): %v", hf, err))
		}
	}

	binPath := filepath.Join(exDir, ex.RunArtifact)
	if !pathExists(binPath) {
		return compileErr(ex, "le binaire attendu n'a pas été produit par le build : "+ex.RunArtifact)
	}

	exResult := models.ExerciseResult{Name: ex.Name}
	allPassed := true
	hasTimeout := false
	for _, tc := range ex.Tests {
		tr := runBinaryTest(exDir, ex, tc)
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

func runBinaryTest(exDir string, ex testdef.ExerciseSpec, tc testdef.TestCase) models.TestResult {
	tc2 := tc

	if ex.ReferenceCmd != "" {
		refOut, refErrOut, refExit, timedOut, startErr := runReference(exDir, ex.ReferenceCmd, tc.Args, tc.Stdin, tc.TimeoutSec)
		if timedOut {
			return models.TestResult{
				Name:  tc.Name,
				Error: "la commande de référence a dépassé le timeout (" + ex.ReferenceCmd + ")",
			}
		}
		// startErr = la commande n'a pas pu être lancée (binaire absent du
		// sandbox, ex: hexdump non installé). ÇA, c'est une vraie indisponibilité.
		// Un exit non-nul (fichier inexistant) n'en est PAS une : on le compare.
		if startErr != nil {
			return models.TestResult{
				Name:  tc.Name,
				Error: "commande de référence indisponible (" + ex.ReferenceCmd + "): " + startErr.Error(),
			}
		}
		tc2.ExpectedOut = refOut
		// La référence fixe aussi le code de sortie attendu : 0 si elle a réussi,
		// non-nul sinon. L'élève doit reproduire ce comportement.
		if refExit == 0 {
			tc2.ExpectedExitCode = 0
		} else {
			tc2.ExpectedExitCode = refExit
		}
		// stderr comparé seulement si le YAML le demande (compare_stderr).
		if tc.CompareStderr {
			tc2.ExpectedErr = &refErrOut
		}
	}

	return runTest(filepath.Join(exDir, ex.RunArtifact), tc2, false)
}

func runReference(exDir, cmd string, args []string, stdin string, timeoutSec int) (stdout, stderr string, exitCode int, timedOut bool, startErr error) {
	timeout := defaultTimeout
	if timeoutSec > 0 {
		timeout = time.Duration(timeoutSec) * time.Second
	}
	full := append([]string{cmd}, args...)
	dockerArgs := []string{
		"run", "--rm", "--network", "none", "--memory", "64m", "--cpus", "0.5",
		"-v", fmt.Sprintf("%s:/work:ro", exDir), "-w", "/work",
		sandboxImage,
	}
	dockerArgs = append(dockerArgs, full...)

	var in io.Reader
	if stdin != "" {
		in = strings.NewReader(stdin)
	}
	out, errOut, timedOut, runErr := runDocker(dockerArgs, timeout, in)

	// Distinguer "n'a pas démarré" de "a tourné et sort non-zéro".
	exitCode = 0
	startErr = nil
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			exitCode = exitErr.ExitCode() // la commande a tourné, exit non-nul : normal
		} else {
			startErr = runErr // docker n'a pas pu lancer la commande : vraie panne
		}
	}
	return out, errOut, exitCode, timedOut, startErr
}
