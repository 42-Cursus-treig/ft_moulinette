package api

import (
	"fmt"
	"net/http"

	"github.com/tristan-reig/ft-moulinette/internal/auth"
	"github.com/tristan-reig/ft-moulinette/internal/locks"
	"github.com/tristan-reig/ft-moulinette/internal/pool"
	"github.com/tristan-reig/ft-moulinette/internal/queue"
	"github.com/tristan-reig/ft-moulinette/internal/visibility"
)

// NewRouter câble les routes de l'interface web et de l'API JSON sur
// un même mux. Toutes les routes hors /auth exigent une session 42.
func NewRouter(q *queue.Queue, testsDir string, oauth auth.Config, sessions *auth.Store, serverBootID string, locksStore *locks.Store, adminLogins map[string]bool, poolSvc *pool.Service, visibilityStore *visibility.Store) (http.Handler, error) {
	tmpl, err := loadTemplates()
	if err != nil {
		return nil, fmt.Errorf("chargement des templates: %w", err)
	}

	static, err := staticHandler()
	if err != nil {
		return nil, fmt.Errorf("chargement des assets statiques: %w", err)
	}

	mux := http.NewServeMux()
	h := &handlers{
		queue:        q,
		tmpl:         tmpl,
		testsDir:     testsDir,
		oauth:        oauth,
		sessions:     sessions,
		serverBootID: serverBootID,
		locks:        locksStore,
		adminLogins:  adminLogins,
		pool:         poolSvc,
		visibility:   visibilityStore,
	}

	// Connexion : jamais protégées, sinon impossible de se connecter.
	mux.HandleFunc("GET /auth/login", h.authLogin)
	mux.HandleFunc("GET /auth/callback", h.authCallback)
	mux.HandleFunc("POST /auth/logout", h.authLogout)

	// Interface web (htmx). L'accès des membres non-admins à chaque section
	// est conditionné par la visibilité réglée dans le panel admin.
	mux.HandleFunc("GET /{$}", h.requireSectionPage(visibility.SectionMoulinette, h.index))
	mux.HandleFunc("GET /history", h.requireSectionPage(visibility.SectionHistory, h.history))
	mux.HandleFunc("GET /classement", h.requireSectionPage(visibility.SectionClassement, h.rankingPage))
	mux.HandleFunc("GET /ui/classement", h.requireSectionFragment(visibility.SectionClassement, h.rankingFragment))
	mux.Handle("GET /static/", http.StripPrefix("/static/", static))
	mux.HandleFunc("POST /ui/jobs", h.requireSectionFragment(visibility.SectionMoulinette, h.submitJobUI))
	mux.HandleFunc("GET /ui/jobs/{id}", h.requireSectionFragment(visibility.SectionMoulinette, h.jobStatusUI))
	// Pas de gating par section : flush de l'historique, action de
	// "ménage" indépendante de la visibilité d'une section particulière.
	mux.HandleFunc("POST /ui/session/close", h.requireAuthAPI(h.closeSession))

	// Administration.
	mux.HandleFunc("GET /admin", h.requireAdmin(h.adminPage))
	mux.HandleFunc("POST /admin/lock", h.requireAdmin(h.adminLock))
	mux.HandleFunc("POST /admin/unlock", h.requireAdmin(h.adminUnlock))
	mux.HandleFunc("POST /admin/delete", h.requireAdmin(h.adminDelete))
	mux.HandleFunc("POST /admin/reorder", h.requireAdmin(h.adminReorder))
	mux.HandleFunc("POST /admin/visibility", h.requireAdmin(h.adminVisibility))

	// API JSON.
	mux.HandleFunc("POST /jobs", h.requireSectionAPI(visibility.SectionMoulinette, h.submitJob))
	mux.HandleFunc("GET /jobs/{id}", h.requireSectionAPI(visibility.SectionMoulinette, h.getJob))
	mux.HandleFunc("GET /healthz", h.health)

	return mux, nil
}
