package api

import (
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/tristan-reig/ft-moulinette/internal/auth"
	"github.com/tristan-reig/ft-moulinette/internal/models"
)

const maxUploadSize = 20 << 20 // 20 Mo

func (h *handlers) jobsForUser(user auth.User) []models.Job {
	all := h.queue.List()
	jobs := make([]models.Job, 0, len(all))
	for _, job := range all {
		if job.Owner == user.Login {
			jobs = append(jobs, job)
		}
	}
	return jobs
}

func jobFinished(status models.Status) bool {
	return status == models.StatusPassed || status == models.StatusFailed || status == models.StatusError
}

// index (GET /) n'affiche que les jobs créés depuis le démarrage courant du
// serveur
func (h *handlers) index(w http.ResponseWriter, r *http.Request) {
	user, _ := userFromContext(r.Context())

	exercises, err := h.exerciseTiles()
	if err != nil {
		http.Error(w, "impossible de lister les exercices: "+err.Error(), http.StatusInternalServerError)
		return
	}

	var current []models.Job
	for _, job := range h.jobsForUser(user) {
		if job.ServerBootID == h.serverBootID {
			current = append(current, job)
		}
	}

	data := h.navFlags(user)
	data["Exercises"] = exercises
	data["Jobs"] = current
	data["User"] = user
	data["Page"] = "moulinette"
	if err := h.tmpl.ExecuteTemplate(w, "index", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// history (GET /history) liste les jobs terminés de l'utilisateur, relus depuis le disque.
func (h *handlers) history(w http.ResponseWriter, r *http.Request) {
	user, _ := userFromContext(r.Context())

	var finished []models.Job
	for _, job := range h.jobsForUser(user) {
		if jobFinished(job.Status) {
			finished = append(finished, job)
		}
	}

	data := h.navFlags(user)
	data["Jobs"] = finished
	data["User"] = user
	data["Page"] = "history"
	if err := h.tmpl.ExecuteTemplate(w, "history", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// submitJobUI (POST /ui/jobs) soumet un job depuis une archive uploadée OU
// un lien GitHub (repo_url), selon ce que le formulaire a rempli. Renvoie
// le fragment HTML de la ligne pour htmx.
func (h *handlers) submitJobUI(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		http.Error(w, "archive trop volumineuse (max 20 Mo) ou formulaire invalide", http.StatusBadRequest)
		return
	}

	exercise := r.FormValue("exercise")
	if exercise == "" {
		http.Error(w, "exercise est requis", http.StatusBadRequest)
		return
	}
	if h.locks.IsLocked(exercise) {
		http.Error(w, "ce sujet est verrouillé", http.StatusForbidden)
		return
	}

	user, _ := userFromContext(r.Context())

	if repoURL := strings.TrimSpace(r.FormValue("repo_url")); repoURL != "" {
		normalizedURL, err := normalizeRepoURL(repoURL)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		job := models.Job{
			ID:           uuid.NewString(),
			RepoURL:      normalizedURL,
			GitToken:     strings.TrimSpace(r.FormValue("github_token")),
			SourceLabel:  normalizedURL,
			Owner:        user.Login,
			Exercise:     exercise,
			Status:       models.StatusPending,
			CreatedAt:    time.Now(),
			ServerBootID: h.serverBootID,
		}
		h.queue.Submit(job)
		h.renderJobRow(w, job)
		return
	}

	file, header, err := r.FormFile("archive")
	if err != nil {
		http.Error(w, "archive manquante ou invalide", http.StatusBadRequest)
		return
	}
	defer file.Close()

	ext := archiveExt(header.Filename)
	if ext == "" {
		http.Error(w, "format non supporté : utilisez un .zip, .tar.gz ou .rar", http.StatusBadRequest)
		return
	}

	dst, err := os.CreateTemp("", "upload-*"+ext)
	if err != nil {
		http.Error(w, "erreur serveur (fichier temporaire)", http.StatusInternalServerError)
		return
	}
	if _, err := io.Copy(dst, file); err != nil {
		dst.Close()
		os.Remove(dst.Name())
		http.Error(w, "erreur lors de la sauvegarde de l'archive", http.StatusInternalServerError)
		return
	}
	dst.Close()

	job := models.Job{
		ID:           uuid.NewString(),
		ArchivePath:  dst.Name(),
		SourceLabel:  header.Filename,
		Owner:        user.Login,
		Exercise:     exercise,
		Status:       models.StatusPending,
		CreatedAt:    time.Now(),
		ServerBootID: h.serverBootID,
	}
	h.queue.Submit(job)

	h.renderJobRow(w, job)
}

func archiveExt(filename string) string {
	lower := strings.ToLower(filename)
	switch {
	case strings.HasSuffix(lower, ".tar.gz"):
		return ".tar.gz"
	case strings.HasSuffix(lower, ".tgz"):
		return ".tgz"
	case strings.HasSuffix(lower, ".zip"):
		return ".zip"
	case strings.HasSuffix(lower, ".rar"):
		return ".rar"
	default:
		return ""
	}
}

// jobStatusUI (GET /ui/jobs/{id}) est appelé en polling par htmx.
func (h *handlers) jobStatusUI(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	user, _ := userFromContext(r.Context())

	job, ok := h.queue.Get(id)
	if !ok || job.Owner != user.Login {
		http.Error(w, "job introuvable", http.StatusNotFound)
		return
	}
	h.renderJobRow(w, *job)
}

func (h *handlers) renderJobRow(w http.ResponseWriter, job models.Job) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.tmpl.ExecuteTemplate(w, "job_row", job); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
