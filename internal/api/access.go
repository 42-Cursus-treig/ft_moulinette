package api

import (
	"net/http"

	"github.com/tristan-reig/ft-moulinette/internal/auth"
)

// piscineBlocked dit si l'accès doit être refusé à ce compte : un piscineux
// (aucun cursus hors piscine) est bloqué sur tout le site ; un admin passe
// toujours.
func (h *handlers) piscineBlocked(user auth.User) bool {
	return user.PiscineOnly && !h.isAdmin(user)
}

// renderPiscineBlocked rend la page de blocage plein écran (403) présentée aux
// comptes piscine.
func (h *handlers) renderPiscineBlocked(w http.ResponseWriter, user auth.User) {
	w.WriteHeader(http.StatusForbidden)
	if err := h.tmpl.ExecuteTemplate(w, "blocked", map[string]any{"User": user}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

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
	data["Maintenance"] = h.maintenance
	if h.maintenance {
		w.WriteHeader(http.StatusServiceUnavailable)
	} else {
		w.WriteHeader(http.StatusForbidden)
	}
	if err := h.tmpl.ExecuteTemplate(w, "disabled", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// requireNotMaintenancePage protège une PAGE qui n'est pas gérée par section
// (dashboard, agenda) : admin passe toujours, membre voit la page "disabled".
func (h *handlers) requireNotMaintenancePage(next http.HandlerFunc) http.HandlerFunc {
	return h.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		user, _ := userFromContext(r.Context())
		if !h.maintenance || h.isAdmin(user) {
			next(w, r)
			return
		}
		h.renderSectionDisabled(w, user)
	})
}

// requireNotMaintenanceFragment : fragment htmx bloqué => 503.
func (h *handlers) requireNotMaintenanceFragment(next http.HandlerFunc) http.HandlerFunc {
	return h.requireAuthFragment(func(w http.ResponseWriter, r *http.Request) {
		user, _ := userFromContext(r.Context())
		if !h.maintenance || h.isAdmin(user) {
			next(w, r)
			return
		}
		http.Error(w, "maintenance en cours", http.StatusServiceUnavailable)
	})
}

// requireNotMaintenanceAPI : API JSON bloquée => 503.
func (h *handlers) requireNotMaintenanceAPI(next http.HandlerFunc) http.HandlerFunc {
	return h.requireAuthAPI(func(w http.ResponseWriter, r *http.Request) {
		user, _ := userFromContext(r.Context())
		if !h.maintenance || h.isAdmin(user) {
			next(w, r)
			return
		}
		http.Error(w, `{"error":"maintenance en cours"}`, http.StatusServiceUnavailable)
	})
}
