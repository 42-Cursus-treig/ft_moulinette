package api

import (
	"sort"

	"github.com/tristan-reig/ft-moulinette/internal/testdef"
)

// ExerciseTile est une entrée de la grille : soit un sujet réel (fichier
// tests/<id>.yaml), soit un sujet verrouillé sans YAML.
type ExerciseTile struct {
	ID     string
	Label  string
	Locked bool
}

// exerciseTiles fusionne sujets réels et sujets verrouillés (un sujet peut
// être verrouillé sans avoir de YAML), triés par ID.
func (h *handlers) exerciseTiles() ([]ExerciseTile, error) {
	options, err := testdef.ListExercises(h.testsDir)
	if err != nil {
		return nil, err
	}

	tiles := make(map[string]*ExerciseTile, len(options))
	for _, o := range options {
		tiles[o.ID] = &ExerciseTile{ID: o.ID, Label: o.Label}
	}

	for id, label := range h.locks.All() {
		if t, ok := tiles[id]; ok {
			t.Locked = true
		} else {
			tiles[id] = &ExerciseTile{ID: id, Label: label, Locked: true}
		}
	}

	out := make([]ExerciseTile, 0, len(tiles))
	for _, t := range tiles {
		out = append(out, *t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
