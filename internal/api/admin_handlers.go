package api

import (
	"net/http"

	"github.com/tristan-reig/ft-moulinette/internal/visibility"
)

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
		"Exercises":  tiles,
		"User":       user,
		"Sections":   visibility.Sections,
		"Visibility": h.visibility.All(),
	}
	if err := h.tmpl.ExecuteTemplate(w, "admin", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// adminVisibility (POST /admin/visibility) active ou masque une section pour
// les membres non-admins.
func (h *handlers) adminVisibility(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulaire invalide", http.StatusBadRequest)
		return
	}
	section := r.FormValue("section")
	visible := r.FormValue("visible") == "true"
	if err := h.visibility.Set(section, visible); err != nil {
		http.Error(w, "réglage de visibilité échoué: "+err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/admin", http.StatusFound)
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
