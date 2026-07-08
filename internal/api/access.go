package api

import (
	"net/http"

	"github.com/tristan-reig/ft-moulinette/internal/auth"
)

// requireSectionPage protège une PAGE membre : un admin passe toujours ; un
// membre passe si la section lui est visible, sinon reçoit la page
// « désactivée » (en-tête conservé pour continuer à naviguer).
func (h *handlers) requireSectionPage(section string, next http.HandlerFunc) http.HandlerFunc {
	return h.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		user, _ := userFromContext(r.Context())
		if h.sectionVisible(user, section) {
			next(w, r)
			return
		}
		h.renderSectionDisabled(w, user)
	})
}

// requireSectionFragment protège un fragment/endpoint htmx : bloqué => 403.
func (h *handlers) requireSectionFragment(section string, next http.HandlerFunc) http.HandlerFunc {
	return h.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		user, _ := userFromContext(r.Context())
		if h.sectionVisible(user, section) {
			next(w, r)
			return
		}
		http.Error(w, "section désactivée", http.StatusForbidden)
	})
}

// requireSectionAPI protège l'API JSON : bloqué => 403 (auth via requireAuthAPI).
func (h *handlers) requireSectionAPI(section string, next http.HandlerFunc) http.HandlerFunc {
	return h.requireAuthAPI(func(w http.ResponseWriter, r *http.Request) {
		user, _ := userFromContext(r.Context())
		if h.sectionVisible(user, section) {
			next(w, r)
			return
		}
		http.Error(w, `{"error":"section désactivée"}`, http.StatusForbidden)
	})
}

// renderSectionDisabled rend la page « section désactivée » avec l'en-tête de
// navigation habituel (limité aux sections encore ouvertes à ce membre).
func (h *handlers) renderSectionDisabled(w http.ResponseWriter, user auth.User) {
	data := h.navFlags(user)
	data["User"] = user
	data["Page"] = ""
	w.WriteHeader(http.StatusForbidden)
	if err := h.tmpl.ExecuteTemplate(w, "disabled", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
