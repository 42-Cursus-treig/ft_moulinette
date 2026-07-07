// Package history persiste les jobs sur disque (un fichier JSON par job) pour
// que l'historique survive à un redémarrage. Volontairement simple : pas de
// base de données, le volume attendu ne le justifie pas.
package history

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/tristan-reig/ft-moulinette/internal/models"
)

type Store struct {
	dir string
	mu  sync.Mutex // sérialise les écritures
}

func New(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("création du dossier d'historique %s: %w", dir, err)
	}
	return &Store{dir: dir}, nil
}

// Save écrit l'état d'un job de façon atomique (fichier temporaire puis
// renommage) pour ne jamais laisser un fichier à moitié écrit.
func (s *Store) Save(job models.Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.MarshalIndent(job, "", "  ")
	if err != nil {
		return fmt.Errorf("sérialisation du job %s: %w", job.ID, err)
	}

	path := filepath.Join(s.dir, job.ID+".json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("écriture de %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("renommage vers %s: %w", path, err)
	}
	return nil
}

// LoadAll relit tous les jobs. Un fichier corrompu est ignoré plutôt que de
// faire échouer tout le chargement.
func (s *Store) LoadAll() ([]models.Job, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, fmt.Errorf("lecture du dossier d'historique %s: %w", s.dir, err)
	}

	jobs := make([]models.Job, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.dir, entry.Name()))
		if err != nil {
			continue
		}
		var job models.Job
		if err := json.Unmarshal(data, &job); err != nil {
			continue
		}
		jobs = append(jobs, job)
	}
	return jobs, nil
}
