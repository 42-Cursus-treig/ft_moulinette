package api

import (
	"net/http"
)

// requireAdmin protège les routes du panneau d'administration : nécessite
// une session valide ET un login présent dans la liste des admins. Renvoie
// 404 (pas 403) pour ne pas révéler l'existence de la page à qui n'y a pas
// accès.
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

// GET /admin : liste tous les sujets (réels + verrouillés sans YAML), avec
// un bouton verrouiller/déverrouiller sur chacun, et un formulaire pour en
// verrouiller un nouveau qui n'a pas encore de fichier de tests.
func (h *handlers) adminPage(w http.ResponseWriter, r *http.Request) {
	user, _ := userFromContext(r.Context())

	tiles, err := h.exerciseTiles()
	if err != nil {
		http.Error(w, "impossible de lister les sujets: "+err.Error(), http.StatusInternalServerError)
		return
	}

	data := map[string]any{
		"Exercises":            tiles,
		"User":                 user,
		"PoolRequestsLastHour": h.pool.RequestsLastHour(),
	}
	if err := h.tmpl.ExecuteTemplate(w, "admin", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// POST /admin/lock : verrouille un sujet, qu'il ait déjà un fichier YAML
// ou non (label utilisé uniquement dans ce second cas).
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

// POST /admin/unlock : déverrouille un sujet.
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
