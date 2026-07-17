package api

import (
	"encoding/json"
	"net/http"
	"sort"
	"time"

	"github.com/tristan-reig/ft-moulinette/internal/models"
)

// modStatRow agrège l'activité moulinette par exercice (données locales).
type modStatRow struct {
	Exercise string
	Total    int
	Passed   int
	Failed   int // échecs + erreurs
	Users    int
	AvgScore int
	PassPct  int
}

// buildModStats compile les statistiques moulinette depuis l'historique des
// jobs : volume, taux de réussite et score moyen par exercice.
func buildModStats(jobs []models.Job, now time.Time) (rows []modStatRow, total, today int) {
	type acc struct {
		total, passed, failed, scoreSum, scored int
		users                                   map[string]bool
	}
	m := map[string]*acc{}
	for _, j := range jobs {
		total++
		if sameDay(j.CreatedAt, now) {
			today++
		}
		a := m[j.Exercise]
		if a == nil {
			a = &acc{users: map[string]bool{}}
			m[j.Exercise] = a
		}
		a.total++
		a.users[j.Owner] = true
		switch j.Status {
		case models.StatusPassed:
			a.passed++
		case models.StatusFailed, models.StatusError:
			a.failed++
		}
		if j.Result != nil && jobFinished(j.Status) {
			a.scoreSum += j.Result.Score
			a.scored++
		}
	}
	for ex, a := range m {
		r := modStatRow{Exercise: ex, Total: a.total, Passed: a.passed, Failed: a.failed, Users: len(a.users)}
		if a.scored > 0 {
			r.AvgScore = a.scoreSum / a.scored
		}
		if fin := a.passed + a.failed; fin > 0 {
			r.PassPct = a.passed * 100 / fin
		}
		rows = append(rows, r)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Total > rows[j].Total })
	return rows, total, today
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Local().Date()
	by, bm, bd := b.Local().Date()
	return ay == by && am == bm && ad == bd
}

// requireAdmin exige une session valide ET un login admin. Renvoie 404
// pour ne pas révéler l'existence de la page.
func (h *handlers) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return h.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		user, _ := userFromContext(r.Context())
		if !h.isAdmin(user) {
			http.NotFound(w, r)
			return
		}
		next(w, r)
	})
}

func (h *handlers) adminPage(w http.ResponseWriter, r *http.Request) {
	user, _ := userFromContext(r.Context())

	tiles, err := h.exerciseTiles()
	if err != nil {
		http.Error(w, "impossible de lister les sujets: "+err.Error(), http.StatusInternalServerError)
		return
	}

	data := map[string]any{
		"Exercises": tiles,
		"User":      user,
	}
	if h.queue != nil {
		stats, total, today := buildModStats(h.queue.List(), time.Now())
		data["Stats"] = stats
		data["JobsTotal"] = total
		data["JobsToday"] = today
	}
	if err := h.tmpl.ExecuteTemplate(w, "admin", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// adminLock (POST /admin/lock) verrouille un sujet, qu'il ait déjà un YAML ou
// non
func (h *handlers) adminLock(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulaire invalide", http.StatusBadRequest)
		return
	}
	id := r.FormValue("id")
	label := r.FormValue("label")
	if id == "" {
		http.Error(w, "id est requis", http.StatusBadRequest)
		return
	}
	if label == "" {
		label = id
	}
	if err := h.locks.Lock(id, label); err != nil {
		http.Error(w, "verrouillage échoué: "+err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin", http.StatusFound)
}

func (h *handlers) adminUnlock(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulaire invalide", http.StatusBadRequest)
		return
	}
	id := r.FormValue("id")
	if id == "" {
		http.Error(w, "id est requis", http.StatusBadRequest)
		return
	}
	if err := h.locks.Unlock(id); err != nil {
		http.Error(w, "déverrouillage échoué: "+err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin", http.StatusFound)
}

// adminDelete (POST /admin/delete) supprime toute décision connue pour un
// sujet. Pour un placeholder (pas de fichier YAML), il disparaît
// entièrement de la liste ; pour un sujet réel, il retombe sur le défaut
// (verrouillé, ordre réinitialisé) mais réapparaîtra puisque son fichier
// existe toujours.
func (h *handlers) adminDelete(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulaire invalide", http.StatusBadRequest)
		return
	}
	id := r.FormValue("id")
	if id == "" {
		http.Error(w, "id est requis", http.StatusBadRequest)
		return
	}
	if err := h.locks.Delete(id); err != nil {
		http.Error(w, "suppression échouée: "+err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin", http.StatusFound)
}

// adminReorder (POST /admin/reorder) reçoit la liste complète des IDs dans
// leur nouvel ordre visuel (JSON, ex: ["c04","c00","c01",...]) suite à un
// glisser-déposer côté client, et persiste cet ordre.
func (h *handlers) adminReorder(w http.ResponseWriter, r *http.Request) {
	var ids []string
	if err := json.NewDecoder(r.Body).Decode(&ids); err != nil {
		http.Error(w, "corps de requête invalide", http.StatusBadRequest)
		return
	}
	if len(ids) == 0 {
		http.Error(w, "liste vide", http.StatusBadRequest)
		return
	}
	if err := h.locks.SetOrder(ids); err != nil {
		http.Error(w, "réordonnancement échoué: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
