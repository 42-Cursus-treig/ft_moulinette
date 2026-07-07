package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/tristan-reig/ft-moulinette/internal/api"
	"github.com/tristan-reig/ft-moulinette/internal/auth"
	"github.com/tristan-reig/ft-moulinette/internal/history"
	"github.com/tristan-reig/ft-moulinette/internal/locks"
	"github.com/tristan-reig/ft-moulinette/internal/queue"
	"github.com/tristan-reig/ft-moulinette/internal/sandbox"
)

func main() {
	if err := loadDotEnv(".env"); err != nil {
		log.Fatal("lecture du .env: ", err)
	}

	addr := os.Getenv("MOULINETTE_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	testsDir := os.Getenv("MOULINETTE_TESTS_DIR")
	if testsDir == "" {
		testsDir = "tests"
	}
	historyDir := os.Getenv("MOULINETTE_HISTORY_DIR")
	if historyDir == "" {
		historyDir = "data/history"
	}
	locksPath := os.Getenv("MOULINETTE_LOCKS_FILE")
	if locksPath == "" {
		locksPath = "data/locked_projects.json"
	}

	adminLoginsEnv := os.Getenv("MOULINETTE_ADMIN_LOGINS")
	if adminLoginsEnv == "" {
		adminLoginsEnv = "treig"
	}
	adminLogins := make(map[string]bool)
	for _, login := range strings.Split(adminLoginsEnv, ",") {
		login = strings.TrimSpace(login)
		if login != "" {
			adminLogins[login] = true
		}
	}

	clientID := os.Getenv("MOULINETTE_42_CLIENT_ID")
	clientSecret := os.Getenv("MOULINETTE_42_CLIENT_SECRET")
	redirectURL := os.Getenv("MOULINETTE_42_REDIRECT_URL")
	if redirectURL == "" {
		redirectURL = "http://localhost:8080/auth/callback"
	}
	if clientID == "" || clientSecret == "" {
		log.Fatal("MOULINETTE_42_CLIENT_ID et MOULINETTE_42_CLIENT_SECRET sont requis " +
			"(créez une application sur https://profile.intra.42.fr/oauth/applications/new)")
	}
	oauthConfig := auth.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
	}
	sessions := auth.NewStore()

	hist, err := history.New(historyDir)
	if err != nil {
		log.Fatal("initialisation de l'historique: ", err)
	}

	locksStore, err := locks.New(locksPath)
	if err != nil {
		log.Fatal("initialisation des verrous: ", err)
	}

	workerCount := 3
	q := queue.New(workerCount, sandbox.Run, hist)
	if err := q.LoadHistory(); err != nil {
		log.Fatal("chargement de l'historique: ", err)
	}
	q.Start()
	defer q.Stop()

	// serverBootID distingue les jobs du démarrage courant (affichés sur la
	// page principale, survivent à un F5) de ceux des démarrages précédents
	// (visibles seulement sur /history).
	serverBootID := fmt.Sprintf("%d", time.Now().UnixNano())

	router, err := api.NewRouter(q, testsDir, oauthConfig, sessions, serverBootID, locksStore, adminLogins)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("ft_moulinette écoute sur %s", addr)
	if err := http.ListenAndServe(addr, router); err != nil {
		log.Fatal(err)
	}
}
