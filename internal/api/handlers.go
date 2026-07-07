package api

import (
	"encoding/json"
	"html/template"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/tristan-reig/ft-moulinette/internal/auth"
	"github.com/tristan-reig/ft-moulinette/internal/locks"
	"github.com/tristan-reig/ft-moulinette/internal/models"
	"github.com/tristan-reig/ft-moulinette/internal/queue"
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
}

func (h *handlers) isAdmin(user auth.User) bool {
	return h.adminLogins[user.Login]
}

type submitJobRequest struct {
	RepoURL  string `json:"repo_url"`
	Exercise string `json:"exercise"`
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
	if h.locks.IsLocked(req.Exercise) {
		http.Error(w, "ce sujet est verrouillé", http.StatusForbidden)
		return
	}

	user, _ := userFromContext(r.Context())
	job := models.Job{
		ID:           uuid.NewString(),
		RepoURL:      req.RepoURL,
		SourceLabel:  req.RepoURL,
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
