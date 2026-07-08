// Package visibility gère la visibilité des sections du site pour les membres
// non-admins (Moulinette, Classement, Historique, …). Un admin voit toujours
// tout : ce store ne concerne que les non-admins. Persisté sur disque, dans le
// même esprit que internal/locks.
package visibility

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// Clés des sections gérées.
const (
	SectionMoulinette = "moulinette"
	SectionClassement = "classement"
	SectionHistory    = "history"
)

// Section décrit une section togglable et son libellé pour le panel admin.
type Section struct {
	Key   string
	Label string
}

// Sections est la liste ordonnée des sections gérées. Ajouter une entrée ici
// (et la câbler dans le routeur) suffit à étendre le système.
var Sections = []Section{
	{SectionMoulinette, "Moulinette"},
	{SectionClassement, "Classement"},
	{SectionHistory, "Historique"},
}

func isKnown(section string) bool {
	for _, s := range Sections {
		if s.Key == section {
			return true
		}
	}
	return false
}

// Store retient les sections masquées aux membres. Tout ce qui n'est pas
// explicitement masqué est visible : le défaut (fichier absent) montre tout,
// donc le comportement reste inchangé tant qu'un admin ne masque rien.
type Store struct {
	mu     sync.RWMutex
	path   string
	hidden map[string]bool // section -> true si masquée aux membres
}

// New charge (si présent) l'état de visibilité existant.
func New(path string) (*Store, error) {
	s := &Store{path: path, hidden: make(map[string]bool)}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	// On persiste la liste des sections MASQUÉES (visible = défaut implicite).
	var hidden []string
	if err := json.Unmarshal(data, &hidden); err != nil {
		return nil, err
	}
	for _, sec := range hidden {
		if isKnown(sec) {
			s.hidden[sec] = true
		}
	}
	return s, nil
}

// Visible indique si une section est visible pour les membres non-admins.
func (s *Store) Visible(section string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return !s.hidden[section]
}

// Set règle la visibilité d'une section pour les membres.
func (s *Store) Set(section string, visible bool) error {
	if !isKnown(section) {
		return fmt.Errorf("section inconnue: %q", section)
	}
	s.mu.Lock()
	if visible {
		delete(s.hidden, section)
	} else {
		s.hidden[section] = true
	}
	s.mu.Unlock()
	return s.save()
}

// All renvoie l'état de visibilité de chaque section connue (clé -> visible).
func (s *Store) All() map[string]bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]bool, len(Sections))
	for _, sec := range Sections {
		out[sec.Key] = !s.hidden[sec.Key]
	}
	return out
}

func (s *Store) save() error {
	s.mu.RLock()
	hidden := make([]string, 0, len(s.hidden))
	for sec := range s.hidden {
		hidden = append(hidden, sec)
	}
	s.mu.RUnlock()
	sort.Strings(hidden)

	data, err := json.MarshalIndent(hidden, "", "  ")
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
