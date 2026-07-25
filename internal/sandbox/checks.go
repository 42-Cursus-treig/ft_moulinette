package sandbox

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Norme (norminette)

type norminetteReport struct {
	Files []norminetteFile `json:"files"`
}

type norminetteFile struct {
	Path   string            `json:"path"`
	Status string            `json:"status"`
	Errors []norminetteError `json:"errors"`
}

type norminetteError struct {
	Name       string                `json:"name"`
	Text       string                `json:"text"`
	Highlights []norminetteHighlight `json:"highlights"`
}

type norminetteHighlight struct {
	Lineno int `json:"lineno"`
	Column int `json:"column"`
}

// checkNorm lance norminette et renvoie les violations (vide si conforme).
// extraRules = flags -R additionnels demandés par le sujet.
func checkNorm(exDir, sourceFile string, extraRules []string) ([]string, error) {
	args := []string{
		"run", "--rm",
		"--network", "none",
		"--memory", "128m",
		"--cpus", "0.5",
		"-v", fmt.Sprintf("%s:/work:ro", exDir),
		"-w", "/work",
		sandboxImage,
		"norminette", "-f", "json", "--no-colors",
	}
	for _, rule := range extraRules {
		args = append(args, "-R", rule)
	}
	args = append(args, sourceFile)

	out, _, _, _ := runDocker(args, 10*time.Second, nil)

	report, err := parseNorminetteJSON(out)
	if err != nil {
		return nil, fmt.Errorf("sortie norminette illisible: %w", err)
	}
	if len(report.Files) == 0 {
		return nil, fmt.Errorf("norminette n'a produit aucun résultat pour %s", sourceFile)
	}

	file := report.Files[0]
	if file.Status == "OK" {
		return nil, nil
	}

	violations := make([]string, 0, len(file.Errors))
	for _, e := range file.Errors {
		line := 0
		if len(e.Highlights) > 0 {
			line = e.Highlights[0].Lineno
		}
		violations = append(violations, fmt.Sprintf("%s (ligne %d): %s", e.Name, line, e.Text))
	}
	if len(violations) == 0 {
		violations = []string{"erreur de norme (détail indisponible)"}
	}
	return violations, nil
}

// parseNorminetteJSON saute le préfixe "Setting locale to ..." que norminette
// ajoute avant son JSON.
func parseNorminetteJSON(raw string) (*norminetteReport, error) {
	idx := strings.IndexByte(raw, '{')
	if idx < 0 {
		return nil, fmt.Errorf("pas de JSON trouvé dans: %s", raw)
	}
	var report norminetteReport
	if err := json.Unmarshal([]byte(raw[idx:]), &report); err != nil {
		return nil, err
	}
	return &report, nil
}

// Fonctions autorisées

// symbolAliases mappe un symbole exporté par la glibc vers le nom logique
// attendu dans allowed_functions. basename(3) est exporté __xpg_basename,
// errno est en réalité la fonction __errno_location, etc. Sans cette table, un
// rendu conforme serait flagué en triche sur la passe binaire.
var symbolAliases = map[string]string{
	"__xpg_basename":  "basename",
	"__isoc99_sscanf": "sscanf",
	"__isoc99_scanf":  "scanf",
	"__printf_chk":    "printf",
	"__fprintf_chk":   "fprintf",
	"__memcpy_chk":    "memcpy",
	"__memmove_chk":   "memmove",
	"__strcpy_chk":    "strcpy",
	"__strcat_chk":    "strcat",
	"__snprintf_chk":  "snprintf",
}

// runtimeSymbols : symboles injectés par le linker et le runtime C, jamais
// appelés explicitement par l'étudiant. Ne concerne que la passe binaire
// (l'analyse par objet ne les voit pas).
var runtimeSymbols = map[string]bool{
	"__libc_start_main":           true,
	"__libc_csu_init":             true,
	"__libc_csu_fini":             true,
	"__stack_chk_fail":            true,
	"__cxa_finalize":              true,
	"__gmon_start__":              true,
	"_ITM_deregisterTMCloneTable": true,
	"_ITM_registerTMCloneTable":   true,
	"_init":                       true,
	"_fini":                       true,
	"_edata":                      true,
	"_end":                        true,
	"__bss_start":                 true,
	"__data_start":                true,
	"__dso_handle":                true,
	"__errno_location":            true,
}

// ForbiddenReport détaille le résultat du contrôle des fonctions autorisées.
// Skipped permet d'afficher dans le rapport de job les fichiers qu'on n'a pas
// pu analyser : sans ça, un .c volontairement non compilable seul serait un
// angle mort silencieux.
type ForbiddenReport struct {
	Symbols []string // fonctions interdites trouvées (trié, dédupliqué)
	Skipped []string // .c non analysables isolément
}

// Cheating dit si le rendu doit être noté -42.
func (r ForbiddenReport) Cheating() bool { return len(r.Symbols) > 0 }

// collectSourceFiles liste récursivement les .c d'un rendu, en chemins
// relatifs à exDir.
//
// Indispensable en build_mode "make" : source_file y vaut "Makefile" et
// l'arborescence des sources est libre (srcs/, includes/…). L'ancienne version
// bouclait sur source_file et sautait tout ce qui ne finissait pas par ".c" :
// sur un rendu make, aucun fichier n'était analysé et le contrôle était
// inopérant.
//
// Les répertoires cachés (.git) et les objets de contrôle sont ignorés.
func collectSourceFiles(exDir string) ([]string, error) {
	var srcs []string
	err := filepath.WalkDir(exDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if path != exDir && strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".c") || strings.HasPrefix(name, "_check_") {
			return nil
		}
		rel, err := filepath.Rel(exDir, path)
		if err != nil {
			return err
		}
		srcs = append(srcs, rel)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(srcs)
	return srcs, nil
}

// checkForbiddenFunctions contrôle qu'un rendu n'appelle que les fonctions
// autorisées par le sujet, en deux passes complémentaires :
//
//  1. Par objet : chaque .c est compilé seul et ses symboles indéfinis relevés.
//     Fine (voit les appels avant résolution du linker) mais faillible : un
//     fichier qui ne compile pas isolément est sauté.
//  2. Sur le binaire linké, quand binaryPath est non vide : filet de sécurité.
//     Un appel qui a survécu jusqu'à l'édition de liens ne peut plus se cacher,
//     y compris depuis un fichier sauté en passe 1.
//
// allowed == nil désactive le contrôle (exercice sans restriction).
// allowed == []string{} interdit toute fonction externe ("Aucune" au sujet).
func checkForbiddenFunctions(exDir string, sourceFiles []string, allowed []string, binaryPath string) (ForbiddenReport, error) {
	var report ForbiddenReport
	if allowed == nil {
		return report, nil
	}

	allowedSet := make(map[string]bool, len(allowed))
	for _, f := range allowed {
		if f = strings.TrimSpace(f); f != "" {
			allowedSet[f] = true
		}
	}

	defined := make(map[string]bool)
	undefined := make(map[string]bool)

	// ---- Passe 1 : analyse par objet ----
	for i, sourceFile := range sourceFiles {
		if !strings.HasSuffix(sourceFile, ".c") {
			continue
		}

		objName := fmt.Sprintf("_check_%d.o", i)
		compileArgs := []string{
			"run", "--rm",
			"--network", "none",
			"--memory", "128m",
			"--cpus", "0.5",
			"-v", fmt.Sprintf("%s:/work", exDir),
			"-w", "/work",
			sandboxImage,
			"gcc", "-fno-builtin", "-fno-stack-protector",
			"-I", ".", "-I", "includes", "-I", "include",
			"-c", "-o", objName, sourceFile,
		}
		if _, _, _, err := runDocker(compileArgs, 15*time.Second, nil); err != nil {
			// Dépend d'un prototype ou d'un header défini ailleurs : pas un cas
			// de triche en soi. On le note pour le rapport, la passe 2 couvrira.
			report.Skipped = append(report.Skipped, sourceFile)
			continue
		}

		nmArgs := []string{
			"run", "--rm",
			"--network", "none",
			"--memory", "64m",
			"--cpus", "0.5",
			"-v", fmt.Sprintf("%s:/work:ro", exDir),
			"-w", "/work",
			sandboxImage,
			"nm", objName,
		}
		nmOut, _, _, err := runDocker(nmArgs, 10*time.Second, nil)
		os.Remove(filepath.Join(exDir, objName))
		if err != nil {
			return report, fmt.Errorf("analyse des symboles de %s impossible: %w", sourceFile, err)
		}
		scanObjectSymbols(nmOut, defined, undefined)
	}

	// ---- Passe 2 : analyse du binaire linké ----
	if binaryPath != "" {
		nmArgs := []string{
			"run", "--rm",
			"--network", "none",
			"--memory", "64m",
			"--cpus", "0.5",
			"-v", fmt.Sprintf("%s:/work:ro", exDir),
			"-w", "/work",
			sandboxImage,
			"nm", "-u", binaryPath,
		}
		nmOut, _, _, err := runDocker(nmArgs, 10*time.Second, nil)
		if err != nil {
			// Binaire strippé ou absent : on ne conclut pas à la triche, la
			// passe 1 fait foi.
			report.Skipped = append(report.Skipped, fmt.Sprintf("%s (binaire non analysable)", binaryPath))
		} else {
			scanBinarySymbols(nmOut, undefined)
		}
	}

	seen := make(map[string]bool)
	for sym := range undefined {
		logical := sym
		if alias, ok := symbolAliases[sym]; ok {
			logical = alias
		}
		if allowedSet[logical] || defined[sym] || defined[logical] || seen[logical] {
			continue
		}
		seen[logical] = true
		report.Symbols = append(report.Symbols, logical)
	}
	sort.Strings(report.Symbols)
	sort.Strings(report.Skipped)
	return report, nil
}

// scanObjectSymbols lit une sortie `nm <objet>` et répartit les symboles.
// Ligne indéfinie : "U symbol" (2 champs, pas d'adresse). Ligne définie :
// "<adresse> <type> symbol" (3 champs, type in {T,t,D,d,B,b,...}).
func scanObjectSymbols(nmOut string, defined, undefined map[string]bool) {
	scanner := bufio.NewScanner(strings.NewReader(nmOut))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		switch {
		case len(fields) == 2 && fields[0] == "U":
			if runtimeSymbols[fields[1]] {
				continue
			}
			undefined[fields[1]] = true
		case len(fields) == 3:
			defined[fields[2]] = true
		}
	}
}

// scanBinarySymbols lit une sortie `nm -u <binaire>` et retient les symboles
// indéfinis hors runtime C. Le suffixe de version (@GLIBC_2.2.5) est retiré ;
// les symboles faibles (colonne "w") sont ignorés, ils ne sont pas des appels.
func scanBinarySymbols(nmOut string, undefined map[string]bool) {
	scanner := bufio.NewScanner(strings.NewReader(nmOut))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 2 || fields[0] != "U" {
			continue
		}
		sym := fields[1]
		if at := strings.IndexByte(sym, '@'); at >= 0 {
			sym = sym[:at]
		}
		if sym == "" || runtimeSymbols[sym] {
			continue
		}
		undefined[sym] = true
	}
}
