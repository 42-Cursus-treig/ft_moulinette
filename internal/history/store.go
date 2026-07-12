// Package history persiste l'historique des corrections dans UN fichier
// JSON par utilisateur (<login>_history.json), plutôt qu'un fichier par
// job - le volume de fichiers ne grossit plus indéfiniment avec le nombre
// de corrections.
//
// Les jobs terminés (passed/failed/error) sont d'abord accumulés dans une
// "session" en mémoire par utilisateur (RecordFinished), puis regroupés et
// écrits en une seule fois sur disque (FlushSession) quand la session se
// termine : déconnexion, fermeture de l'onglet (best-effort, voir le
// commentaire sur FlushSession), ou arrêt propre du serveur (FlushAll).
//
// Compromis assumé : un job encore pending/running au moment d'un crash
// serveur (pas un arrêt propre) n'aura jamais été écrit sur disque et
// disparaît silencieusement au redémarrage, plutôt que d'apparaître comme
// "interrompu" - c'est le prix de ne plus écrire à chaque changement de
// statut. Un job déjà terminé mais pas encore flush au moment du crash est
// logé de la même façon.
package history

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/tristan-reig/ft-moulinette/internal/models"
)

type Store struct {
	dir string

	fileMu sync.Mutex // sérialise les lectures/écritures de fichiers utilisateur

	sessionMu sync.Mutex
	sessions  map[string][]models.Job // login -> jobs terminés pas encore flush sur disque
}

// New crée (si besoin) le dossier de stockage et renvoie un Store prêt à
// l'emploi.
func New(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("création du dossier d'historique %s: %w", dir, err)
	}
	return &Store{dir: dir, sessions: make(map[string][]models.Job)}, nil
}

// RecordFinished ajoute un job terminé à la session en mémoire de son
// propriétaire. Pas encore écrit sur disque - voir FlushSession.
func (s *Store) RecordFinished(job models.Job) {
	if job.Owner == "" {
		return
	}
	s.sessionMu.Lock()
	s.sessions[job.Owner] = append(s.sessions[job.Owner], job)
	s.sessionMu.Unlock()
}

// FlushSession regroupe les jobs en attente d'un utilisateur avec son
// fichier existant et réécrit celui-ci en une seule fois. Sans effet s'il
// n'y a rien en attente pour cet utilisateur - safe à appeler "au cas où"
// (ex: à chaque déconnexion, même si l'utilisateur n'a rien soumis).
func (s *Store) FlushSession(login string) error {
	if login == "" {
		return nil
	}
	s.sessionMu.Lock()
	pending := s.sessions[login]
	delete(s.sessions, login)
	s.sessionMu.Unlock()

	if len(pending) == 0 {
		return nil
	}
	return s.appendToUserFile(login, pending)
}

// FlushAll flush toutes les sessions en attente, tous utilisateurs
// confondus - à appeler à l'arrêt propre du serveur pour limiter le risque
// de perte par rapport à une simple coupure.
func (s *Store) FlushAll() error {
	s.sessionMu.Lock()
	logins := make([]string, 0, len(s.sessions))
	for login := range s.sessions {
		logins = append(logins, login)
	}
	s.sessionMu.Unlock()

	var firstErr error
	for _, login := range logins {
		if err := s.FlushSession(login); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (s *Store) userPath(login string) string {
	return filepath.Join(s.dir, sanitizeLogin(login)+"_history.json")
}

// appendToUserFile fusionne pending avec le contenu déjà présent sur disque
// pour cet utilisateur et réécrit le fichier de façon atomique (fichier
// temporaire puis renommage, pour ne jamais laisser un fichier à moitié
// écrit en cas de crash au mauvais moment).
func (s *Store) appendToUserFile(login string, pending []models.Job) error {
	s.fileMu.Lock()
	defer s.fileMu.Unlock()

	path := s.userPath(login)
	existing, err := readJobsFile(path)
	if err != nil {
		return fmt.Errorf("lecture de %s: %w", path, err)
	}

	all := append(existing, pending...)

	data, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return fmt.Errorf("sérialisation de l'historique de %s: %w", login, err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("écriture de %s: %w", tmp, err)
	}
	return os.Rename(tmp, path)
}

// readJobsFile lit un fichier d'historique utilisateur. Un fichier absent
// ou corrompu est traité comme vide plutôt que de faire échouer
// l'appelant - un historique partiel/reparti à zéro vaut mieux qu'un
// serveur qui refuse de démarrer ou une session qui ne peut plus flush.
func readJobsFile(path string) ([]models.Job, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var jobs []models.Job
	if err := json.Unmarshal(data, &jobs); err != nil {
		return nil, nil
	}
	return jobs, nil
}

// LoadAll relit l'historique persisté de TOUS les utilisateurs (un fichier
// par utilisateur), pour réhydrater la file en mémoire au démarrage -
// nécessaire pour que /history retrouve les corrections des sessions
// précédentes après un redémarrage.
func (s *Store) LoadAll() ([]models.Job, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, fmt.Errorf("lecture du dossier d'historique %s: %w", s.dir, err)
	}

	var all []models.Job
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), "_history.json") {
			continue
		}
		jobs, err := readJobsFile(filepath.Join(s.dir, entry.Name()))
		if err != nil {
			continue
		}
		all = append(all, jobs...)
	}
	return all, nil
}

var unsafeLoginChars = regexp.MustCompile(`[^A-Za-z0-9_-]`)

// sanitizeLogin réduit un login à des caractères sûrs pour un nom de
// fichier - les logins 42 sont déjà alphanumériques simples en pratique,
// mais on ne présume de rien (au cas où un login contiendrait un
// caractère qui casserait un nom de fichier, voire une tentative de path
// traversal du genre "../../etc").
func sanitizeLogin(login string) string {
	return unsafeLoginChars.ReplaceAllString(login, "_")
}
