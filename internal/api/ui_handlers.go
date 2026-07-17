package api

import (
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/tristan-reig/ft-moulinette/internal/auth"
	"github.com/tristan-reig/ft-moulinette/internal/models"
)

const maxUploadSize = 20 << 20 // 20 Mo
const historyPerPage = 10

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

// paginate borne une page demandée à [1, totalPages] et renvoie les indices de
// tranche sûrs (jamais hors limites), plus le numéro de page effectif et le
// nombre total de pages. Une liste vide donne "page 1/1" avec une tranche vide.
func paginate(total, reqPage, perPage int) (page, totalPages, start, end int) {
	totalPages = (total + perPage - 1) / perPage
	if totalPages < 1 {
		totalPages = 1
	}
	page = reqPage
	if page < 1 {
		page = 1
	}
	if page > totalPages {
		page = totalPages
	}
	start = (page - 1) * perPage
	end = start + perPage
	if start > total {
		start = total
	}
	if end > total {
		end = total
	}
	return
}

// pageWindow renvoie la liste de numéros de page à afficher autour de la page
// courante, avec au plus `span` pages de part et d'autre. Évite d'afficher 200
// liens quand l'historique est long.
func pageWindow(current, total, span int) []int {
	lo := current - span
	if lo < 1 {
		lo = 1
	}
	hi := current + span
	if hi > total {
		hi = total
	}
	nums := make([]int, 0, hi-lo+1)
	for i := lo; i <= hi; i++ {
		nums = append(nums, i)
	}
	return nums
}

// history (GET /history) liste les jobs terminés de l'utilisateur, paginés.
// ?page=N (1-based) ; hors bornes, N est ramené dans l'intervalle valide.
func (h *handlers) history(w http.ResponseWriter, r *http.Request) {
	user, _ := userFromContext(r.Context())

	var finished []models.Job
	for _, job := range h.jobsForUser(user) {
		if jobFinished(job.Status) {
			finished = append(finished, job)
		}
	}

	reqPage, _ := strconv.Atoi(r.URL.Query().Get("page"))
	page, totalPages, start, end := paginate(len(finished), reqPage, historyPerPage)

	data := h.navFlags(user)
	data["Jobs"] = finished[start:end]
	data["User"] = user
	data["Page"] = "history"
	data["CurrentPage"] = page
	data["TotalPages"] = totalPages
	data["HasPrev"] = page > 1
	data["HasNext"] = page < totalPages
	data["PrevPage"] = page - 1
	data["NextPage"] = page + 1
	data["PageNums"] = pageWindow(page, totalPages, 2)
	data["TotalJobs"] = len(finished)

	// Requête htmx (navigation entre pages) : ne renvoyer que le fragment.
	if r.Header.Get("HX-Request") == "true" {
		if err := h.tmpl.ExecuteTemplate(w, "history_content", data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}
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

	// Si les deux champs se retrouvent remplis (ne devrait plus arriver
	// depuis la correction du changement d'onglet côté JS, qui vide le
	// champ quitté - mais on ne prend pas de risque côté serveur aussi) :
	// l'archive prime. Sélectionner un fichier est un geste plus explicite
	// qu'une URL qui aurait pu simplement rester dans le champ par erreur.
	file, header, fileErr := r.FormFile("archive")
	hasArchive := fileErr == nil && header != nil && header.Filename != ""

	if !hasArchive {
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

// closeSession (POST /ui/session/close) flush l'historique en attente de
// l'utilisateur. Appelé côté client via navigator.sendBeacon() sur
// l'événement pagehide (fermeture d'onglet/navigateur) - best effort :
// aucun signal navigateur ne garantit un déclenchement à 100% (crash,
// coupure brutale). Le flush à la déconnexion et à l'arrêt propre du
// serveur couvrent les autres cas raisonnables. Répond vite et sans corps
// significatif : sendBeacon n'attend pas de réponse exploitée.
func (h *handlers) closeSession(w http.ResponseWriter, r *http.Request) {
	user, _ := userFromContext(r.Context())
	if err := h.queue.FlushSession(user.Login); err != nil {
		log.Printf("flush de session à la fermeture pour %s: %v", user.Login, err)
	}
	w.WriteHeader(http.StatusNoContent)
}
