package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
)

// Disposition personnalisée du dashboard : chaque utilisateur choisit les
// widgets affichés, leur ordre et leur largeur ; le reste part en « réserve ».
// Persisté en JSON sur disque (même approche que internal/locks) — aucune
// donnée 42 là-dedans, uniquement des préférences d'affichage.

// dashWidget décrit un widget disponible. L'ordre du registre EST la
// disposition par défaut ; le registre doit rester aligné sur dashCards
// (un test le vérifie).
type dashWidget struct {
	ID      string
	Title   string
	Span    int    // largeur par défaut, en colonnes sur 12
	Trigger string // hx-trigger particulier ("" = "load")
}

var dashWidgets = []dashWidget{
	{ID: "hero", Title: "Profil", Span: 12},
	{ID: "defenses", Title: "Corrections à venir", Span: 12, Trigger: "load, every 120s [document.visibilityState === 'visible']"},
	{ID: "logtime", Title: "Temps de présence", Span: 6},
	{ID: "coalition", Title: "Coalition", Span: 6},
	{ID: "projects", Title: "Projets", Span: 6},
	{ID: "skills", Title: "Compétences", Span: 6},
	{ID: "top5", Title: "Classement coalition", Span: 6},
	{ID: "exam", Title: "Exam", Span: 6, Trigger: "load, every 180s [document.visibilityState === 'visible']"},
	{ID: "progress", Title: "Ta progression", Span: 6},
	{ID: "ready", Title: "Prêt à rendre", Span: 6},
	{ID: "evals", Title: "Évaluations", Span: 6},
	{ID: "points", Title: "Points de correction", Span: 6},
	{ID: "achievements", Title: "Succès", Span: 6},
	{ID: "events", Title: "Événements", Span: 6},
	{ID: "lookup", Title: "Chercher un student", Span: 12},
}

// dashSpanAllowed : les trois tailles de l'éditeur — Petite (⅓), Moyenne (½),
// Grande (pleine largeur). Rien d'autre : une valeur inconnue retombe sur la
// taille par défaut du widget.
var dashSpanAllowed = map[int]bool{4: true, 6: true, 12: true}

func widgetByID(id string) (dashWidget, bool) {
	for _, w := range dashWidgets {
		if w.ID == id {
			return w, true
		}
	}
	return dashWidget{}, false
}

// widgetPos est un widget placé sur le dashboard : id + largeur choisie.
type widgetPos struct {
	ID   string `json:"id"`
	Span int    `json:"span"`
}

func defaultLayout() []widgetPos {
	out := make([]widgetPos, len(dashWidgets))
	for i, w := range dashWidgets {
		out[i] = widgetPos{ID: w.ID, Span: w.Span}
	}
	return out
}

// sanitizeLayout ne garde que des widgets connus, sans doublon, avec une
// largeur autorisée (sinon celle par défaut du widget). Les entrées venant
// du client ou d'un vieux fichier sont donc inoffensives.
func sanitizeLayout(in []widgetPos) []widgetPos {
	seen := map[string]bool{}
	var out []widgetPos
	for _, wp := range in {
		w, ok := widgetByID(wp.ID)
		if !ok || seen[wp.ID] {
			continue
		}
		seen[wp.ID] = true
		if !dashSpanAllowed[wp.Span] {
			wp.Span = w.Span
		}
		out = append(out, wp)
	}
	return out
}

// layoutStore persiste la disposition de chaque login dans un fichier JSON.
type layoutStore struct {
	mu   sync.Mutex
	path string
	m    map[string][]widgetPos
}

func newLayoutStore(path string) (*layoutStore, error) {
	s := &layoutStore{path: path, m: map[string][]widgetPos{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lecture des dispositions: %w", err)
	}
	if err := json.Unmarshal(data, &s.m); err != nil {
		return nil, fmt.Errorf("dispositions illisibles (%s): %w", path, err)
	}
	return s, nil
}

func (s *layoutStore) Get(login string) ([]widgetPos, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	lay, ok := s.m[login]
	if !ok {
		return nil, false
	}
	return append([]widgetPos(nil), lay...), true
}

func (s *layoutStore) Set(login string, lay []widgetPos) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[login] = lay
	return s.persistLocked()
}

func (s *layoutStore) Reset(login string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, login)
	return s.persistLocked()
}

func (s *layoutStore) persistLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o644)
}

// --- Endpoint d'enregistrement ---

type layoutRequest struct {
	Reset   bool        `json:"reset"`
	Widgets []widgetPos `json:"widgets"`
}

// saveLayout (POST /ui/dashboard/layout) enregistre la disposition envoyée
// par l'éditeur, ou la remet aux valeurs par défaut avec {"reset": true}.
func (h *handlers) saveLayout(w http.ResponseWriter, r *http.Request) {
	user, _ := userFromContext(r.Context())

	var req layoutRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&req); err != nil {
		http.Error(w, `{"error":"requête illisible"}`, http.StatusBadRequest)
		return
	}
	if req.Reset {
		if err := h.layouts.Reset(user.Login); err != nil {
			http.Error(w, `{"error":"écriture impossible"}`, http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	lay := sanitizeLayout(req.Widgets)
	if len(lay) == 0 {
		http.Error(w, `{"error":"garde au moins un widget sur le dashboard"}`, http.StatusBadRequest)
		return
	}
	if err := h.layouts.Set(user.Login, lay); err != nil {
		http.Error(w, `{"error":"écriture impossible"}`, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
