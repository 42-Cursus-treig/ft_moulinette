// Package testdef décrit le format des tests d'un projet de piscine, stocké en
// YAML dans tests/<projet>.yaml. Un projet regroupe un ou plusieurs
// exercices, chacun avec son sous-dossier, son fichier source et ses tests.
package testdef

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type TestCase struct {
	Name             string   `yaml:"name"`
	Args             []string `yaml:"args"`
	Stdin            string   `yaml:"stdin"`
	ExpectedOut      string   `yaml:"expected_stdout"`
	ExpectedExitCode int      `yaml:"expected_exit_code"`
	TimeoutSec       int      `yaml:"timeout_sec"`
}

type ExerciseSpec struct {
	Name             string     `yaml:"name"`
	Dir              string     `yaml:"dir"`
	SourceFile       string     `yaml:"source_file"`
	Harness          string     `yaml:"harness"`
	AllowedFunctions []string   `yaml:"allowed_functions"`
	Tests            []TestCase `yaml:"tests"`
}

type ProjectDef struct {
	Project        string         `yaml:"project"`
	DisplayName    string         `yaml:"display_name"`
	NormExtraRules []string       `yaml:"norm_extra_rules"` // règles -R additionnelles pour norminette
	Points         []int          `yaml:"points"`           // Points[k-1] = score si les k premiers exercices passent d'affilée
	Exercises      []ExerciseSpec `yaml:"exercises"`
}

// ExerciseOption est l'entrée affichée dans la grille : un bouton par projet.
type ExerciseOption struct {
	ID    string
	Label string
}

// Load lit et valide le fichier de définition d'un projet.
func Load(path string) (*ProjectDef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("lecture %s: %w", path, err)
	}

	var def ProjectDef
	if err := yaml.Unmarshal(data, &def); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	if len(def.Exercises) == 0 {
		return nil, fmt.Errorf("%s: aucun exercice défini (clé 'exercises' vide ou absente)", path)
	}
	if len(def.Points) > 0 && len(def.Points) != len(def.Exercises) {
		return nil, fmt.Errorf("%s: le barème 'points' a %d entrée(s) mais il y a %d exercice(s)", path, len(def.Points), len(def.Exercises))
	}
	return &def, nil
}

// ListExercises renvoie un projet par fichier .yaml du dossier de tests.
func ListExercises(dir string) ([]ExerciseOption, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("lecture du dossier %s: %w", dir, err)
	}

	var out []ExerciseOption
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		def, err := Load(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		label := def.DisplayName
		if label == "" {
			label = def.Project
		}
		out = append(out, ExerciseOption{ID: def.Project, Label: label})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
