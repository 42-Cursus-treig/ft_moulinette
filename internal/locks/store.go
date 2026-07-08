// Package locks gère le verrouillage de sujets (ex: "c02") - utile pour
// bloquer/masquer un sujet avant même d'avoir écrit son fichier YAML de
// tests, ou pour désactiver temporairement un sujet déjà en place.
// Persisté sur disque, indépendant de internal/testdef.
package locks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// entry associe un ID de sujet (ex: "c02") à un libellé d'affichage utilisé
// uniquement quand aucun fichier YAML n'existe encore pour ce sujet.
type entry struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type Store struct {
	mu     sync.RWMutex
	path   string
	locked map[string]string // ID -> Label
}

// New charge (si présent) le fichier de verrous existant.
func New(path string) (*Store, error) {
	s := &Store{path: path, locked: make(map[string]string)}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	var entries []entry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	for _, e := range entries {
		s.locked[e.ID] = e.Label
	}
	return s, nil
}

// IsLocked indique si un sujet est verrouillé.
func (s *Store) IsLocked(id string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.locked[id]
	return ok
}

// Lock verrouille un sujet. label n'est utilisé que si aucun fichier YAML
// n'existe pour ce sujet (sinon son display_name prime à l'affichage).
func (s *Store) Lock(id, label string) error {
	s.mu.Lock()
	s.locked[id] = label
	s.mu.Unlock()
	return s.save()
}

// Unlock déverrouille un sujet (no-op silencieux s'il ne l'était pas).
func (s *Store) Unlock(id string) error {
	s.mu.Lock()
	delete(s.locked, id)
	s.mu.Unlock()
	return s.save()
}

// All renvoie tous les sujets verrouillés (ID -> Label), pour construire la
// grille (utilisateurs) et la page d'administration.
func (s *Store) All() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]string, len(s.locked))
	for id, label := range s.locked {
		out[id] = label
	}
	return out
}

func (s *Store) save() error {
	s.mu.RLock()
	entries := make([]entry, 0, len(s.locked))
	for id, label := range s.locked {
		entries = append(entries, entry{ID: id, Label: label})
	}
	s.mu.RUnlock()

	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
