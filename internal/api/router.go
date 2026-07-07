package api

import (
	"fmt"
	"net/http"

	"github.com/tristan-reig/ft-moulinette/internal/auth"
	"github.com/tristan-reig/ft-moulinette/internal/locks"
	"github.com/tristan-reig/ft-moulinette/internal/pool"
	"github.com/tristan-reig/ft-moulinette/internal/queue"
)

// NewRouter câble les routes de l'interface web et de l'API JSON sur
// un même mux. Toutes les routes hors /auth exigent une session 42.
func NewRouter(q *queue.Queue, testsDir string, oauth auth.Config, sessions *auth.Store, serverBootID string, locksStore *locks.Store, adminLogins map[string]bool, poolSvc *pool.Service) (http.Handler, error) {
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
	}

	// Connexion : jamais protégées, sinon impossible de se connecter.
	mux.HandleFunc("GET /auth/login", h.authLogin)
	mux.HandleFunc("GET /auth/callback", h.authCallback)
	mux.HandleFunc("POST /auth/logout", h.authLogout)

	// Interface web (htmx).
	mux.HandleFunc("GET /{$}", h.requireAuth(h.index))
	mux.HandleFunc("GET /history", h.requireAuth(h.history))
	mux.HandleFunc("GET /classement", h.requireAuth(h.rankingPage))
	mux.HandleFunc("GET /ui/classement", h.requireAuth(h.rankingFragment))
	mux.Handle("GET /static/", http.StripPrefix("/static/", static))
	mux.HandleFunc("POST /ui/jobs", h.requireAuth(h.submitJobUI))
	mux.HandleFunc("GET /ui/jobs/{id}", h.requireAuth(h.jobStatusUI))

	// Administration.
	mux.HandleFunc("GET /admin", h.requireAdmin(h.adminPage))
	mux.HandleFunc("POST /admin/lock", h.requireAdmin(h.adminLock))
	mux.HandleFunc("POST /admin/unlock", h.requireAdmin(h.adminUnlock))

	// API JSON.
	mux.HandleFunc("POST /jobs", h.requireAuthAPI(h.submitJob))
	mux.HandleFunc("GET /jobs/{id}", h.requireAuthAPI(h.getJob))
	mux.HandleFunc("GET /healthz", h.health)

	return mux, nil
}
