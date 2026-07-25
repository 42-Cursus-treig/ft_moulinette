package sandbox

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/tristan-reig/ft-moulinette/internal/models"
	"github.com/tristan-reig/ft-moulinette/internal/testdef"
)

// runScriptExercise gère BuildMode == "script".
func runScriptExercise(exDir string, ex testdef.ExerciseSpec, normExtraRules []string) models.ExerciseResult {
	// 1. Présence du script + des sources rendues, puis norme sur les .c rendus.
	if !pathExists(filepath.Join(exDir, ex.BuildScript)) {
		return missing(ex, ex.BuildScript)
	}
	rules := normExtraRules
	if ex.NormExtraRules != nil {
		rules = ex.NormExtraRules
	}
	// Le sujet impose la norme sur les .c de l'élève (pas sur le script shell).
	for _, src := range studentCFiles(ex) {
		if !pathExists(filepath.Join(exDir, src)) {
			return missing(ex, src)
		}
		if v, err := checkNorm(exDir, src, rules); err != nil {
			return compileErr(ex, "Impossible de vérifier la norme:\n"+err.Error())
		} else if len(v) > 0 {
			return models.ExerciseResult{Name: ex.Name, Status: models.ExerciseNormeError, Log: strings.Join(v, "\n")}
		}
	}

	// 2. Lancer le script de build dans la sandbox.
	if out, err := runShell(exDir, "sh "+ex.BuildScript); err != nil {
		return compileErr(ex, "le script de build a échoué:\n"+out)
	}

	// 3. Vérifier artefacts + symboles.
	if res, ok := checkArtifacts(exDir, ex); !ok {
		return res
	}

	// 4. Fonctions autorisées (sur les .c rendus par l'élève).
	if res, ok := checkForbidden(exDir, ex); !ok {
		return res
	}

	// 5. Linker un harness contre l'artefact et exécuter les tests (optionnel).
	return linkAndTest(exDir, ex)
}

// runMakeExercise gère BuildMode == "make".
func runMakeExercise(exDir string, ex testdef.ExerciseSpec, _ []string) models.ExerciseResult {
	// L'élève ne rend que le Makefile ; on ne vérifie pas sa "norme" (norminette
	// ne norme pas les Makefiles). On vérifie sa présence.
	if !pathExists(filepath.Join(exDir, ex.SourceFile)) {
		return missing(ex, ex.SourceFile)
	}

	// 1. Déposer l'arbre de sources fourni par le serveur (srcs/, includes/).
	if ex.ProvidedTree != "" {
		src := filepath.Join(testsDir, ex.ProvidedTree)
		if err := copyDir(src, exDir); err != nil {
			return compileErr(ex, fmt.Sprintf("arbre de sources fourni introuvable (%s): %v", ex.ProvidedTree, err))
		}
	}

	// 2. make (= all).
	out, err := runShell(exDir, "make")
	if err != nil {
		return compileErr(ex, "`make` a échoué:\n"+out)
	}
	if res, ok := checkArtifacts(exDir, ex); !ok {
		return res
	}

	// 3. Non-recompilation : un 2e make ne doit rien reconstruire.
	if ex.CheckNoRebuild {
		out2, err := runShell(exDir, "make")
		if err != nil {
			return compileErr(ex, "2e `make` en erreur:\n"+out2)
		}
		if !isNothingToDo(out2) {
			return models.ExerciseResult{
				Name:   ex.Name,
				Status: models.ExerciseKO,
				Log:    "le Makefile recompile inutilement (un 2e `make` relance des commandes) :\n" + out2,
			}
		}
	}

	// 4. Cibles clean/fclean/re.
	// 4. Cibles clean/fclean/re.
	if res, ok := checkMakeTargets(exDir, ex); !ok {
		return res
	}

	// 4bis. Fonctions autorisées. Après les cibles make (fclean a pu supprimer
	// le binaire) : on reconstruit d'abord, puis on analyse sources + binaire.
	if ex.RunArtifact != "" || ex.LinkHarness != "" {
		if out, err := runShell(exDir, "make"); err != nil {
			return compileErr(ex, "reconstruction avant test impossible:\n"+out)
		}
	}
	if res, ok := checkForbidden(exDir, ex); !ok {
		return res
	}

	// 5. Tests.
	if ex.RunArtifact != "" {
		return runBinaryTests(exDir, ex)
	}
	return linkAndTest(exDir, ex)
}

// --- Helpers partagés ---

// runShell exécute une commande shell dans la sandbox, /work en lecture-écriture
// (le build produit des fichiers). Renvoie la sortie fusionnée.
func runShell(dir, cmd string) (string, error) {
	args := []string{
		"run", "--rm",
		"--network", "none",
		"--memory", "256m",
		"--cpus", "1.0",
		"-v", fmt.Sprintf("%s:/work", dir),
		"-w", "/work",
		sandboxImage,
		"sh", "-c", cmd,
	}
	stdout, stderr, _, err := runDocker(args, 30*time.Second, nil)
	return stdout + stderr, err
}

// checkArtifacts vérifie que chaque fichier de ExpectArtifacts existe, et que
// le 1er artefact .a contient les ArchiveSymbols demandés.
func checkArtifacts(exDir string, ex testdef.ExerciseSpec) (models.ExerciseResult, bool) {
	for _, art := range ex.ExpectArtifacts {
		if !pathExists(filepath.Join(exDir, art)) {
			return models.ExerciseResult{
				Name:   ex.Name,
				Status: models.ExerciseKO,
				Log:    "artefact attendu non produit par le build : " + art,
			}, false
		}
	}
	if len(ex.ArchiveSymbols) > 0 {
		archive := firstArchive(ex.ExpectArtifacts)
		if archive == "" {
			return compileErr(ex, "archive_symbols défini mais aucun artefact .a dans expect_artifacts"), false
		}
		out, _, _, err := runDocker([]string{
			"run", "--rm", "--network", "none", "--memory", "64m", "--cpus", "0.5",
			"-v", fmt.Sprintf("%s:/work:ro", exDir), "-w", "/work",
			sandboxImage, "nm", archive,
		}, 10*time.Second, nil)
		if err != nil {
			return compileErr(ex, "lecture des symboles de "+archive+" impossible:\n"+out), false
		}
		present := definedSymbols(out)
		var missingSyms []string
		for _, s := range ex.ArchiveSymbols {
			if !present[s] {
				missingSyms = append(missingSyms, s)
			}
		}
		if len(missingSyms) > 0 {
			return models.ExerciseResult{
				Name:   ex.Name,
				Status: models.ExerciseKO,
				Log:    "symbole(s) manquant(s) dans " + archive + " : " + strings.Join(missingSyms, ", "),
			}, false
		}
	}
	return models.ExerciseResult{}, true
}

// checkForbidden applique le contrôle des fonctions autorisées aux .c rendus.
// checkForbidden applique le contrôle des fonctions autorisées aux .c rendus.
// En build_mode "make", source_file vaut "Makefile" : studentCFiles renvoie une
// liste vide et le contrôle serait inopérant. On découvre alors les .c
// récursivement, et on analyse le binaire linké en second filet.
func checkForbidden(exDir string, ex testdef.ExerciseSpec) (models.ExerciseResult, bool) {
	srcs := studentCFiles(ex)
	if ex.BuildMode == "make" || ex.BuildMode == "script" {
		found, err := collectSourceFiles(exDir)
		if err != nil {
			return compileErr(ex, "Impossible de lister les sources:\n"+err.Error()), false
		}
		srcs = found
	}

	report, err := checkForbiddenFunctions(exDir, srcs, ex.AllowedFunctions, ex.RunArtifact)
	if err != nil {
		return compileErr(ex, "Impossible de vérifier les fonctions autorisées:\n"+err.Error()), false
	}
	if report.Cheating() {
		log := "Fonction(s) non autorisée(s) appelée(s) : " + strings.Join(report.Symbols, ", ")
		if len(report.Skipped) > 0 {
			log += "\n(fichiers non analysables isolément : " + strings.Join(report.Skipped, ", ") + ")"
		}
		return models.ExerciseResult{
			Name:   ex.Name,
			Status: models.ExerciseCheating,
			Log:    log,
		}, false
	}
	return models.ExerciseResult{}, true
}

// checkMakeTargets vérifie clean/fclean/re. Pour fclean, on s'assure que les
// artefacts disparaissent ; pour re, qu'ils réapparaissent.
func checkMakeTargets(exDir string, ex testdef.ExerciseSpec) (models.ExerciseResult, bool) {
	has := func(t string) bool {
		for _, x := range ex.MakeTargets {
			if x == t {
				return true
			}
		}
		return false
	}

	if has("fclean") {
		if out, err := runShell(exDir, "make fclean"); err != nil {
			return compileErr(ex, "`make fclean` a échoué:\n"+out), false
		}
		for _, art := range ex.ExpectArtifacts {
			if pathExists(filepath.Join(exDir, art)) {
				return models.ExerciseResult{
					Name: ex.Name, Status: models.ExerciseKO,
					Log: "`make fclean` ne supprime pas l'artefact : " + art,
				}, false
			}
		}
	}
	if has("re") {
		if out, err := runShell(exDir, "make re"); err != nil {
			return compileErr(ex, "`make re` a échoué:\n"+out), false
		}
		for _, art := range ex.ExpectArtifacts {
			if !pathExists(filepath.Join(exDir, art)) {
				return models.ExerciseResult{
					Name: ex.Name, Status: models.ExerciseKO,
					Log: "`make re` ne reconstruit pas l'artefact : " + art,
				}, false
			}
		}
	}
	return models.ExerciseResult{}, true
}

// linkAndTest linke le harness (s'il y en a un) contre le 1er artefact et
// exécute les tests. Sans harness ni tests, l'exercice est OK dès lors que le
// build a réussi.
func linkAndTest(exDir string, ex testdef.ExerciseSpec) models.ExerciseResult {
	if ex.LinkHarness == "" || len(ex.Tests) == 0 {
		return models.ExerciseResult{Name: ex.Name, Status: models.ExerciseOK, Log: "build réussi"}
	}

	// Copier le harness + son header éventuel dans le dossier.
	if err := copyFile(filepath.Join(testsDir, ex.LinkHarness), filepath.Join(exDir, "_harness.c")); err != nil {
		return compileErr(ex, fmt.Sprintf("harnais de test introuvable (%s): %v", ex.LinkHarness, err))
	}
	if ex.LinkHarnessInclude != "" {
		if err := copyFile(filepath.Join(testsDir, ex.LinkHarnessInclude), filepath.Join(exDir, filepath.Base(ex.LinkHarnessInclude))); err != nil {
			return compileErr(ex, fmt.Sprintf("header de test introuvable (%s): %v", ex.LinkHarnessInclude, err))
		}
	}

	// Linker : gcc -I. _harness.c <artefact> -o a.out  (l'artefact .a est passé
	// directement en entrée du linker, plus robuste que -L. -lft selon le nom).
	artifact := firstArchive(ex.ExpectArtifacts)
	if artifact == "" && len(ex.ExpectArtifacts) > 0 {
		artifact = ex.ExpectArtifacts[0]
	}
	out, err := runShell(exDir, fmt.Sprintf("gcc -Wall -Wextra -Werror -I. _harness.c %s -o _test_bin", artifact))
	if err != nil {
		return compileErr(ex, "link du harnais contre l'artefact impossible:\n"+out)
	}

	binPath := filepath.Join(exDir, "_test_bin")
	exResult := models.ExerciseResult{Name: ex.Name}
	allPassed := true
	hasTimeout := false
	for _, tc := range ex.Tests {
		tr := runTest(binPath, tc, false)
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

// --- Petits utilitaires ---

// studentCFiles = fichiers .c rendus par l'élève (source principal + extras),
// en excluant les .h et le Makefile.
func studentCFiles(ex testdef.ExerciseSpec) []string {
	var out []string
	if strings.HasSuffix(ex.SourceFile, ".c") {
		out = append(out, ex.SourceFile)
	}
	for _, e := range ex.ExtraSources {
		if strings.HasSuffix(e, ".c") {
			out = append(out, e)
		}
	}
	return out
}

func firstArchive(arts []string) string {
	for _, a := range arts {
		if strings.HasSuffix(a, ".a") {
			return a
		}
	}
	return ""
}

// isNothingToDo reconnaît la sortie d'un make qui n'a rien à reconstruire.
func isNothingToDo(out string) bool {
	o := strings.ToLower(out)
	return strings.Contains(o, "nothing to be done") ||
		strings.Contains(o, "is up to date") ||
		strings.Contains(o, "rien à faire")
}

// definedSymbols extrait les symboles définis (type T/t/D/etc.) d'une sortie nm.
func definedSymbols(nmOut string) map[string]bool {
	set := map[string]bool{}
	for _, line := range strings.Split(nmOut, "\n") {
		f := strings.Fields(line)
		if len(f) == 3 && f[1] != "U" {
			set[f[2]] = true
		}
	}
	return set
}

func missing(ex testdef.ExerciseSpec, file string) models.ExerciseResult {
	return models.ExerciseResult{
		Name:   ex.Name,
		Status: models.ExerciseMissing,
		Log:    fmt.Sprintf("fichier attendu introuvable : %s", filepath.Join(ex.Dir, file)),
	}
}

func compileErr(ex testdef.ExerciseSpec, msg string) models.ExerciseResult {
	return models.ExerciseResult{Name: ex.Name, Status: models.ExerciseCompileError, Log: msg}
}
