package api

import (
	"sort"

	"github.com/tristan-reig/ft-moulinette/internal/testdef"
)

// ExerciseTile est une entrée de la grille de sélection : soit un vrai
// sujet (fichier tests/<id>.yaml présent), soit un sujet verrouillé qui
// n'a pas encore de fichier YAML du tout.
type ExerciseTile struct {
	ID     string
	Label  string
	Locked bool
}

// exerciseTiles fusionne les sujets réels et les sujets verrouillés sans
// fichier YAML (placeholders admin), triés par ID. Un sujet réel est
// verrouillé par défaut tant qu'il n'a pas été explicitement déverrouillé
// via /admin (voir locks.Store.IsLocked) — donc un nouveau tests/<id>.yaml
// n'est jamais accessible tout de suite après son ajout.
func (h *handlers) exerciseTiles() ([]ExerciseTile, error) {
	options, err := testdef.ListExercises(h.testsDir)
	if err != nil {
		return nil, err
	}

	tiles := make(map[string]*ExerciseTile, len(options))
	for _, o := range options {
		tiles[o.ID] = &ExerciseTile{ID: o.ID, Label: o.Label, Locked: h.locks.IsLocked(o.ID)}
	}

	// Placeholders : sujets verrouillés qui n'ont pas encore de fichier
	// YAML du tout (réservés à l'avance depuis /admin).
	for id, label := range h.locks.All() {
		if _, ok := tiles[id]; !ok {
			tiles[id] = &ExerciseTile{ID: id, Label: label, Locked: true}
		}
	}

	out := make([]ExerciseTile, 0, len(tiles))
	for _, t := range tiles {
		out = append(out, *t)
	}
	// Un sujet réordonné manuellement (Order > 0) passe avant tout sujet
	// jamais réordonné, dans l'ordre choisi ; les non-réordonnés se trient
	// entre eux par ID, et arrivent après — un sujet nouvellement ajouté
	// n'a donc jamais l'air de sauter en tête de liste.
	sort.Slice(out, func(i, j int) bool {
		oi, oj := h.locks.OrderOf(out[i].ID), h.locks.OrderOf(out[j].ID)
		iOrdered, jOrdered := oi > 0, oj > 0
		if iOrdered != jOrdered {
			return iOrdered
		}
		if iOrdered && oi != oj {
			return oi < oj
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}
