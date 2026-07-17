package sandbox

import (
	"bufio"
	"encoding/json"
	"fmt"
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
func checkForbiddenFunctions(exDir string, sourceFiles []string, allowed []string) ([]string, error) {
	if allowed == nil {
		return nil, nil
	}

	allowedSet := make(map[string]bool, len(allowed))
	for _, f := range allowed {
		allowedSet[f] = true
	}

	defined := make(map[string]bool)
	undefined := make(map[string]bool)

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
			"--read-only",
			"-v", fmt.Sprintf("%s:/work", exDir),
			"-w", "/work",
			sandboxImage,
			"gcc", "-fno-builtin", "-fno-stack-protector", "-c", "-o", objName, sourceFile,
		}
		if _, _, _, err := runDocker(compileArgs, 15*time.Second, nil); err != nil {
			// Un fichier qui ne compile pas seul (ex: dépend d'un prototype défini
			// ailleurs) n'est pas un cas de triche : on saute son analyse.
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
			return nil, fmt.Errorf("analyse des symboles impossible: %w", err)
		}

		scanner := bufio.NewScanner(strings.NewReader(nmOut))
		for scanner.Scan() {
			fields := strings.Fields(scanner.Text())
			// Ligne "U symbol" : 2 champs (pas d'adresse). Ligne définie :
			// "<adresse> <type> symbol" : 3 champs, type in {T,t,D,d,B,b,...}.
			if len(fields) == 2 && fields[0] == "U" {
				undefined[fields[1]] = true
			} else if len(fields) == 3 {
				defined[fields[2]] = true
			}
		}
	}

	var forbidden []string
	for sym := range undefined {
		if allowedSet[sym] || defined[sym] {
			continue
		}
		forbidden = append(forbidden, sym)
	}
	sort.Strings(forbidden) // ordre stable pour l'affichage et les tests
	return forbidden, nil
}
