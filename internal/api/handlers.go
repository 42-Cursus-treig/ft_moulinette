package api

import (
	"encoding/json"
	"html/template"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/tristan-reig/ft-moulinette/internal/auth"
	"github.com/tristan-reig/ft-moulinette/internal/fortytwo"
	"github.com/tristan-reig/ft-moulinette/internal/locks"
	"github.com/tristan-reig/ft-moulinette/internal/models"
	"github.com/tristan-reig/ft-moulinette/internal/pool"
	"github.com/tristan-reig/ft-moulinette/internal/queue"
)

const (
	SectionMoulinette = "moulinette"
	SectionClassement = "classement"
	SectionHistory    = "history"
)

type handlers struct {
	queue        *queue.Queue
	tmpl         *template.Template
	testsDir     string
	oauth        auth.Config
	sessions     *auth.Store
	serverBootID string
	locks        *locks.Store
	adminLogins  map[string]bool
	pool         *pool.Service
	ft           *fortytwo.Client
	icsSnap      *icsStore
	layouts      *layoutStore
}

func (h *handlers) isAdmin(user auth.User) bool {
	return h.adminLogins[user.Login]
}

// sectionVisible indique si un utilisateur a accès à une section.
// Le paramètre de section est ignoré car l'accès est maintenant global pour les admins.
func (h *handlers) sectionVisible(user auth.User, section string) bool {
	if section == SectionClassement {
		return true
	}
	return h.isAdmin(user)
}

// navFlags renvoie les drapeaux de navigation (sections accessibles pour cet
// utilisateur, statut admin) à fusionner dans les données d'un template de page.
func (h *handlers) navFlags(user auth.User) map[string]any {
	flags := map[string]any{
		"IsAdmin":       h.isAdmin(user),
		"NavMoulinette": h.sectionVisible(user, SectionMoulinette),
		"NavClassement": h.sectionVisible(user, SectionClassement),
		"NavHistory":    h.sectionVisible(user, SectionHistory),
	}
	// Bannière exam commune à toutes les pages (cache local du pool, pas d'API).
	if h.pool != nil {
		if b := buildExamBanner(h.pool.AllExamWindows(), time.Now()); b != nil {
			flags["ExamBanner"] = b
		}
	}
	return flags
}

type submitJobRequest struct {
	RepoURL     string `json:"repo_url"`
	GitHubToken string `json:"github_token,omitempty"` // dépôt privé ; jamais renvoyé dans les réponses (Job.GitToken est json:"-")
	Exercise    string `json:"exercise"`
}

// submitJob (POST /jobs) enregistre un job et répond immédiatement avec son
// ID ; le client poll ensuite GET /jobs/{id}.
func (h *handlers) submitJob(w http.ResponseWriter, r *http.Request) {
	var req submitJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "corps de requête invalide", http.StatusBadRequest)
		return
	}
	if req.RepoURL == "" || req.Exercise == "" {
		http.Error(w, "repo_url et exercise sont requis", http.StatusBadRequest)
		return
	}
	normalizedURL, err := normalizeRepoURL(req.RepoURL)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if h.locks.IsLocked(req.Exercise) {
		http.Error(w, "ce sujet est verrouillé", http.StatusForbidden)
		return
	}

	user, _ := userFromContext(r.Context())
	job := models.Job{
		ID:           uuid.NewString(),
		RepoURL:      normalizedURL,
		GitToken:     req.GitHubToken,
		SourceLabel:  normalizedURL,
		Owner:        user.Login,
		Exercise:     req.Exercise,
		Status:       models.StatusPending,
		CreatedAt:    time.Now(),
		ServerBootID: h.serverBootID,
	}
	h.queue.Submit(job)

	writeJSON(w, http.StatusAccepted, job)
}

// getJob (GET /jobs/{id}) renvoie 404 (pas 403) si le job appartient à
// quelqu'un d'autre, pour ne pas divulguer l'existence d'IDs tiers.
func (h *handlers) getJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	user, _ := userFromContext(r.Context())

	job, ok := h.queue.Get(id)
	if !ok || job.Owner != user.Login {
		http.Error(w, "job introuvable", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (h *handlers) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
