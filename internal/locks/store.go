// Package locks gère le verrouillage de sujets (ex: "c02"), y compris un
// sujet qui n'a pas encore de fichier YAML de tests du tout, ainsi que leur
// ordre d'affichage personnalisé (glisser-déposer côté /admin).
//
// Comportement PAR DÉFAUT (depuis la migration vers le format v2) : un
// sujet est VERROUILLÉ tant qu'il n'a pas été explicitement déverrouillé
// via /admin. Ainsi, tout nouveau projet ajouté (nouveau tests/<id>.yaml)
// reste inaccessible aux élèves jusqu'à ce qu'il soit décidé prêt.
//
// Pour ne pas casser les sujets déjà en place au moment de ce changement,
// une migration automatique et unique déverrouille, la toute première fois
// que le nouveau format est utilisé, tous les sujets qui ont déjà un
// fichier YAML à cet instant précis — voir SeedExisting.
package locks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

const currentSchema = 2

// entry est l'état connu d'un sujet. LockDecided distingue "jamais touché"
// (retombe sur le défaut verrouillé) de "explicitement décidé" — sans ce
// distinguo, on ne pourrait pas donner un Order à un sujet sans, du même
// coup, le faire sortir du comportement par défaut.
type entry struct {
	ID          string `json:"id"`
	Locked      bool   `json:"locked"`
	LockDecided bool   `json:"lock_decided"`
	Label       string `json:"label,omitempty"` // uniquement utile si aucun YAML n'existe pour ce sujet
	Order       int    `json:"order,omitempty"` // 0 = jamais réordonné manuellement (trié en dernier, par ID)
}

// fileV2 est le format de persistance actuel.
type fileV2 struct {
	Schema  int              `json:"schema"`
	Entries map[string]entry `json:"entries"`
}

type Store struct {
	mu      sync.RWMutex
	path    string
	entries map[string]entry

	// freshlyMigrated est vrai si ce Store vient de migrer depuis l'ancien
	// format (ou de démarrer sans aucun fichier existant) — signal pour
	// l'appelant (main.go) qu'il doit lancer SeedExisting une fois.
	freshlyMigrated bool
}

// New charge (si présent) le fichier de verrous existant, en migrant
// automatiquement l'ancien format si besoin.
func New(path string) (*Store, error) {
	s := &Store{path: path, entries: make(map[string]entry)}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			s.freshlyMigrated = true // aucun fichier du tout : premier démarrage
			return s, nil
		}
		return nil, err
	}

	var v2 fileV2
	if err := json.Unmarshal(data, &v2); err == nil && v2.Schema == currentSchema {
		s.entries = v2.Entries
		if s.entries == nil {
			s.entries = make(map[string]entry)
		}
		return s, nil
	}

	// Pas au format v2 : ancien format (simple liste de sujets verrouillés,
	// l'absence signifiait "déverrouillé"). On migre : tout ce qui y
	// figurait reste explicitement verrouillé, et freshlyMigrated=true
	// signale à l'appelant qu'il doit déverrouiller les sujets déjà en
	// place pour préserver leur disponibilité actuelle.
	var oldEntries []entry
	if err := json.Unmarshal(data, &oldEntries); err != nil {
		// Fichier illisible dans les deux formats : on repart de zéro
		// plutôt que de faire échouer le démarrage du serveur.
		s.freshlyMigrated = true
		return s, nil
	}
	for _, e := range oldEntries {
		s.entries[e.ID] = entry{ID: e.ID, Locked: true, LockDecided: true, Label: e.Label}
	}
	s.freshlyMigrated = true
	return s, nil
}

// NeedsSeeding indique si ce Store vient de migrer (ou de démarrer sans
// fichier) et n'a donc encore reçu aucun déverrouillage explicite pour les
// sujets déjà en place — voir SeedExisting.
func (s *Store) NeedsSeeding() bool {
	return s.freshlyMigrated
}

// SeedExisting déverrouille explicitement chaque ID de la liste s'il n'a
// pas déjà de décision explicite prise pour lui. À appeler une seule fois
// au démarrage, uniquement si NeedsSeeding() est vrai, avec la liste des
// projets qui ont déjà un fichier YAML à cet instant — pour que les sujets
// déjà en place ne se retrouvent pas verrouillés du jour au lendemain par
// le nouveau comportement par défaut.
func (s *Store) SeedExisting(ids []string) error {
	s.mu.Lock()
	for _, id := range ids {
		if e, ok := s.entries[id]; !ok || !e.LockDecided {
			e.ID = id
			e.Locked = false
			e.LockDecided = true
			s.entries[id] = e
		}
	}
	s.freshlyMigrated = false
	s.mu.Unlock()

	return s.save()
}

// IsLocked indique si un sujet est verrouillé. Par défaut (aucune décision
// explicite prise pour cet ID), un sujet est considéré VERROUILLÉ.
func (s *Store) IsLocked(id string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.entries[id]
	if !ok || !e.LockDecided {
		return true
	}
	return e.Locked
}

// Lock verrouille explicitement un sujet. label n'est utilisé que si aucun
// fichier YAML n'existe pour ce sujet (sinon son display_name prime à
// l'affichage). Préserve l'Order existant s'il y en avait déjà un.
func (s *Store) Lock(id, label string) error {
	s.mu.Lock()
	e := s.entries[id]
	e.ID = id
	e.Locked = true
	e.LockDecided = true
	if label != "" {
		e.Label = label
	} else if e.Label == "" {
		e.Label = id
	}
	s.entries[id] = e
	s.mu.Unlock()
	return s.save()
}

// Unlock déverrouille explicitement un sujet — décision persistée, ne
// revient jamais au comportement par défaut. Préserve l'Order existant.
func (s *Store) Unlock(id string) error {
	s.mu.Lock()
	e := s.entries[id]
	e.ID = id
	e.Locked = false
	e.LockDecided = true
	s.entries[id] = e
	s.mu.Unlock()
	return s.save()
}

// Delete retire toute décision connue pour un sujet. Pour un sujet sans
// fichier YAML (placeholder), il disparaît alors entièrement de la grille
// et de la page admin. Pour un sujet avec un vrai fichier YAML, il
// réapparaîtra au prochain chargement (le fichier existe toujours sur
// disque) mais retombe sur le comportement par défaut : verrouillé, ordre
// non personnalisé.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	delete(s.entries, id)
	s.mu.Unlock()
	return s.save()
}

// SetOrder fixe l'ordre d'affichage explicite de chaque ID de la liste,
// dans l'ordre où ils apparaissent (le premier reçoit Order=1, etc. — 0
// reste réservé à "jamais réordonné manuellement"). Un ID qui n'avait
// encore aucune décision de verrouillage explicite en reçoit une neutre
// (verrouillé par défaut) simplement pour pouvoir porter son Order — ça ne
// change pas son état de verrouillage apparent.
func (s *Store) SetOrder(ids []string) error {
	s.mu.Lock()
	for i, id := range ids {
		e, ok := s.entries[id]
		if !ok {
			e = entry{ID: id, Locked: true, LockDecided: false}
		}
		e.Order = i + 1
		s.entries[id] = e
	}
	s.mu.Unlock()
	return s.save()
}

// OrderOf renvoie l'ordre explicite d'un sujet (0 si jamais réordonné
// manuellement — à trier après tout sujet ayant un ordre explicite).
func (s *Store) OrderOf(id string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.entries[id].Order
}

// All renvoie, pour chaque sujet verrouillé sans correspondance YAML, son
// libellé — utilisé pour afficher les sujets verrouillés qui n'ont pas
// encore de fichier YAML (voir exerciseTiles côté api). Les sujets
// déverrouillés n'ont pas besoin d'un libellé de secours : ils ont
// forcément un vrai fichier YAML, sinon rien n'existerait à déverrouiller.
func (s *Store) All() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]string, len(s.entries))
	for id, e := range s.entries {
		if e.LockDecided && e.Locked {
			label := e.Label
			if label == "" {
				label = id
			}
			out[id] = label
		}
	}
	return out
}

// Decisions renvoie l'état verrouillé/déverrouillé de chaque sujet ayant
// une décision explicite — utilisé par la page admin pour afficher aussi
// les sujets déjà déverrouillés (pas seulement les verrouillés).
func (s *Store) Decisions() map[string]bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]bool, len(s.entries))
	for id, e := range s.entries {
		if e.LockDecided {
			out[id] = e.Locked
		}
	}
	return out
}

func (s *Store) save() error {
	s.mu.RLock()
	v2 := fileV2{Schema: currentSchema, Entries: make(map[string]entry, len(s.entries))}
	for id, e := range s.entries {
		v2.Entries[id] = e
	}
	s.mu.RUnlock()

	// json.Marshal trie déjà les clés d'une map par ordre alphabétique :
	// le fichier produit est stable et lisible sans tri manuel.
	data, err := json.MarshalIndent(v2, "", "  ")
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
