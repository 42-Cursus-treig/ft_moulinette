package pool

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Snapshot est un relevé quotidien du classement score d'une session :
// il porte niveau, score et coalition de chaque piscineux.
type Snapshot struct {
	Date string     `json:"date"` // AAAA-MM-JJ (heure locale serveur)
	At   time.Time  `json:"at"`
	Rows []ScoreRow `json:"rows"`
}

// historyStore conserve les relevés de progression sur disque, un par jour
// et par session. Le relevé du jour est mis à jour à chaque refresh du score,
// donc il reflète les dernières valeurs de la journée.
type historyStore struct {
	path string

	mu       sync.Mutex
	sessions map[string][]Snapshot
}

func newHistoryStore(path string) *historyStore {
	h := &historyStore{path: path, sessions: make(map[string][]Snapshot)}
	if path == "" {
		return h
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return h
	}
	// Fichier illisible : on repart d'un historique vide plutôt que d'échouer.
	_ = json.Unmarshal(data, &h.sessions)
	if h.sessions == nil {
		h.sessions = make(map[string][]Snapshot)
	}
	return h
}

func (h *historyStore) saveLocked() {
	if h.path == "" {
		return
	}
	data, err := json.Marshal(h.sessions)
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(h.path), 0o755)
	tmp := h.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, h.path)
}

// record ajoute le relevé du jour (ou remplace celui déjà pris aujourd'hui).
func (h *historyStore) record(session string, rows []ScoreRow) {
	h.mu.Lock()
	defer h.mu.Unlock()

	now := time.Now()
	snap := Snapshot{Date: now.Format("2006-01-02"), At: now, Rows: rows}

	snaps := h.sessions[session]
	if n := len(snaps); n > 0 && snaps[n-1].Date == snap.Date {
		snaps[n-1] = snap
	} else {
		snaps = append(snaps, snap)
	}
	h.sessions[session] = snaps
	h.saveLocked()
}

// series renvoie les relevés d'une session, du plus ancien au plus récent.
func (h *historyStore) series(session string) []Snapshot {
	h.mu.Lock()
	defer h.mu.Unlock()
	snaps := h.sessions[session]
	out := make([]Snapshot, len(snaps))
	copy(out, snaps)
	return out
}
