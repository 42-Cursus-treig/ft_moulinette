package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tristan-reig/ft-moulinette/internal/fortytwo"
)

// dashboardTimeout borne le chargement des briques 42 : au-delà, on rend ce
// qu'on a plutôt que de faire attendre l'utilisateur indéfiniment.
const dashboardTimeout = 12 * time.Second

// dashboard (GET /) est l'accueil : l'intra de l'étudiant. Il agrège en
// parallèle profil, projets, corrections reçues et défenses à venir depuis
// l'API 42, avec le jeton de la session. Sans jeton 42 (arrivée via le cookie
// d'identité partagé, ou scope insuffisant), on rend un dashboard « light »
// qui invite à se reconnecter, sans planter.
func (h *handlers) dashboard(w http.ResponseWriter, r *http.Request) {
	user, _ := userFromContext(r.Context())

	data := h.navFlags(user)
	data["User"] = user
	data["Page"] = "dashboard"
	data["Live"] = false

	tok, err := h.sessions.FreshToken(r, h.oauth)
	if err != nil || h.fortytwo == nil {
		// Pas de jeton exploitable : dashboard light (profil de session seul).
		if err := h.tmpl.ExecuteTemplate(w, "dashboard", data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), dashboardTimeout)
	defer cancel()
	token := tok.AccessToken

	var (
		wg          sync.WaitGroup
		profile     fortytwo.Profile
		cats        []fortytwo.ProjectCategory
		corrections []fortytwo.Correction
		defenses    []fortytwo.Defense
		profileErr  error
		projectsErr error
		correcErr   error
		defenseErr  error
	)

	// Le profil est chargé en premier (rapide, et fournit le cursus pour la
	// liste des projets disponibles), puis les trois autres briques en parallèle.
	profile, profileErr = h.fortytwo.Profile(ctx, token)

	wg.Add(3)
	go func() {
		defer wg.Done()
		cats, projectsErr = h.loadProjects(ctx, token, user.ID, profile.CursusID)
	}()
	go func() {
		defer wg.Done()
		corrections, correcErr = h.fortytwo.CorrectionsReceived(ctx, token, user.ID)
	}()
	go func() {
		defer wg.Done()
		defenses, defenseErr = h.fortytwo.UpcomingDefenses(ctx, token, user.ID)
	}()
	wg.Wait()

	// Un 401 (jeton refusé malgré le refresh) bascule tout le dashboard en light.
	if isReconnectErr(profileErr) {
		if err := h.tmpl.ExecuteTemplate(w, "dashboard", data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	data["Live"] = true
	data["Profile"] = profile
	data["ProjectCats"] = cats
	data["Corrections"] = corrections
	data["Defenses"] = defenses
	data["ProfileErr"] = errMsg(profileErr)
	data["ProjectsErr"] = errMsg(projectsErr)
	data["CorrectionsErr"] = errMsg(correcErr)
	data["DefensesErr"] = errMsg(defenseErr)

	if err := h.tmpl.ExecuteTemplate(w, "dashboard", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// loadProjects assemble les projets de l'étudiant (par statut) puis, en
// best-effort, le catalogue des projets encore disponibles à l'inscription.
func (h *handlers) loadProjects(ctx context.Context, token string, userID, cursusID int) ([]fortytwo.ProjectCategory, error) {
	cats, err := h.fortytwo.Projects(ctx, token)
	if err != nil {
		return nil, err
	}
	done := map[int]bool{}
	for _, c := range cats {
		for _, it := range c.Items {
			done[it.ProjectID] = true
		}
	}
	// Best-effort : un catalogue indisponible ne fait pas échouer la brique.
	if avail, err := h.fortytwo.AvailableProjects(ctx, token, cursusID, done); err == nil && len(avail.Items) > 0 {
		cats = append(cats, avail)
	}
	return cats, nil
}

// registerProject (POST /ui/projects/register) inscrit l'utilisateur à un
// projet puis renvoie un fragment de confirmation htmx.
func (h *handlers) registerProject(w http.ResponseWriter, r *http.Request) {
	user, _ := userFromContext(r.Context())
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulaire invalide", http.StatusBadRequest)
		return
	}
	projectID, err := strconv.Atoi(strings.TrimSpace(r.FormValue("project_id")))
	if err != nil || projectID <= 0 {
		http.Error(w, "project_id invalide", http.StatusBadRequest)
		return
	}

	tok, err := h.sessions.FreshToken(r, h.oauth)
	if err != nil {
		http.Error(w, "reconnecte-toi pour t'inscrire à un projet", http.StatusUnauthorized)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), dashboardTimeout)
	defer cancel()
	if err := h.fortytwo.Register(ctx, tok.AccessToken, projectID, user.ID); err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		h.tmpl.ExecuteTemplate(w, "register_error", map[string]any{"Msg": inscriptionErr(err)})
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	h.tmpl.ExecuteTemplate(w, "register_ok", map[string]any{"Name": r.FormValue("name")})
}

// submitFeedback (POST /ui/corrections/feedback) envoie un feedback au
// correcteur puis renvoie l'état « envoyé ».
func (h *handlers) submitFeedback(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulaire invalide", http.StatusBadRequest)
		return
	}
	scaleTeamID, err := strconv.Atoi(strings.TrimSpace(r.FormValue("scale_team_id")))
	if err != nil || scaleTeamID <= 0 {
		http.Error(w, "scale_team_id invalide", http.StatusBadRequest)
		return
	}
	rating, _ := strconv.Atoi(r.FormValue("rating"))
	if rating < 1 {
		rating = 5
	}
	comment := strings.TrimSpace(r.FormValue("comment"))

	tok, err := h.sessions.FreshToken(r, h.oauth)
	if err != nil {
		http.Error(w, "reconnecte-toi pour envoyer un feedback", http.StatusUnauthorized)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), dashboardTimeout)
	defer cancel()
	if err := h.fortytwo.SendFeedback(ctx, tok.AccessToken, scaleTeamID, rating, comment); err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		h.tmpl.ExecuteTemplate(w, "feedback_error", map[string]any{"Msg": errMsg(err)})
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	h.tmpl.ExecuteTemplate(w, "feedback_done", nil)
}

// --- helpers ---

func isReconnectErr(err error) bool {
	return err != nil && err == fortytwo.ErrUnauthorized
}

func errMsg(err error) string {
	if err == nil {
		return ""
	}
	if err == fortytwo.ErrUnauthorized {
		return "reconnecte-toi pour rafraîchir cette donnée"
	}
	return "indisponible pour le moment"
}

func inscriptionErr(err error) string {
	if err == fortytwo.ErrUnauthorized {
		return "reconnecte-toi (scope insuffisant)"
	}
	// L'API 42 renvoie souvent la raison (prérequis manquant, déjà inscrit…).
	return "inscription refusée par 42"
}
