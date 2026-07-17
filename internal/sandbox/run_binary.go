package sandbox

import (
	"fmt"
	"io"
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

// runBinaryTest lance un test. Si ex.ReferenceCmd est défini, il calcule le
// stdout attendu en exécutant la commande de référence dans le sandbox ; sinon
// il s'appuie sur tc.ExpectedOut / tc.ExpectedErr.
func runBinaryTest(exDir string, ex testdef.ExerciseSpec, tc testdef.TestCase) models.TestResult {
	tc2 := tc // copie locale : on peut réécrire ExpectedOut depuis la référence

	if ex.ReferenceCmd != "" {
		refOut, _, refErr := runReference(exDir, ex.ReferenceCmd, tc.Args, tc.Stdin, tc.TimeoutSec)
		if refErr != nil {
			return models.TestResult{
				Name:  tc.Name,
				Error: "commande de référence indisponible (" + ex.ReferenceCmd + "): " + refErr.Error(),
			}
		}
		tc2.ExpectedOut = refOut
		// stderr n'est comparé à la référence que si le YAML le demande
		// explicitement (préfixes de programme différents sinon).
	}

	// Exécuter le binaire de l'élève via le runTest existant (compare stdout,
	// stderr si ExpectedErr!=nil, et code de sortie). Pas de valgrind ici.
	return runTest(filepath.Join(exDir, ex.RunArtifact), tc2, false)
}

// runReference exécute la commande système de référence dans le sandbox, sur
// les mêmes fichiers (montés en lecture seule) et avec les mêmes arguments que
// le test de l'élève. Renvoie son stdout, son stderr et une erreur d'exécution
// éventuelle.
func runReference(exDir, cmd string, args []string, stdin string, timeoutSec int) (stdout, stderr string, err error) {
	timeout := defaultTimeout
	if timeoutSec > 0 {
		timeout = time.Duration(timeoutSec) * time.Second
	}
	dArgs := []string{
		"run", "--rm",
		"--network", "none",
		"--memory", "64m",
		"--cpus", "0.5",
		"-v", fmt.Sprintf("%s:/work:ro", exDir),
		"-w", "/work",
		sandboxImage,
		cmd,
	}
	dArgs = append(dArgs, args...)

	var in io.Reader
	if stdin != "" {
		in = strings.NewReader(stdin)
	}
	out, errOut, _, runErr := runDocker(dArgs, timeout, in)
	return out, errOut, runErr
}
