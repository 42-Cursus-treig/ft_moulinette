package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	// Embarque la base de fuseaux horaires dans le binaire pour que
	// time.LoadLocation("Europe/Paris") fonctionne même dans un conteneur
	// sans paquet tzdata (l'affichage des horaires d'exam en dépend).
	_ "time/tzdata"

	"github.com/tristan-reig/ft-moulinette/internal/api"
	"github.com/tristan-reig/ft-moulinette/internal/auth"
	"github.com/tristan-reig/ft-moulinette/internal/fortytwo"
	"github.com/tristan-reig/ft-moulinette/internal/history"
	"github.com/tristan-reig/ft-moulinette/internal/locks"
	"github.com/tristan-reig/ft-moulinette/internal/pool"
	"github.com/tristan-reig/ft-moulinette/internal/queue"
	"github.com/tristan-reig/ft-moulinette/internal/sandbox"
)

func main() {
	if err := loadDotEnv(".env"); err != nil {
		log.Fatal("lecture du .env: ", err)
	}

	// Fuseau par défaut du serveur : heure de France. Ainsi time.Now() et tous
	// les affichages (horaires d'exam, « MàJ » des classements, logs) sont en
	// heure locale française sans conversion explicite. La base tzdata est
	// embarquée dans le binaire (import _ "time/tzdata") pour fonctionner même
	// dans un conteneur sans paquet tzdata.
	if loc, err := time.LoadLocation("Europe/Paris"); err == nil {
		time.Local = loc
	} else {
		log.Printf("fuseau Europe/Paris indisponible, on garde %s : %v", time.Local, err)
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

	maintenance := os.Getenv("MOULINETTE_MAINTENANCE") == "true"
	if maintenance {
		log.Println("MODE MAINTENANCE actif : seuls les admins ont accès")
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
	// Scope OAuth demandé à 42. L'intra dashboard lit projets/corrections et
	// écrit (inscription à un projet, feedback correcteur) : le défaut est donc
	// « public projects ». Le scope doit être coché sur l'app côté intra, sinon
	// 42 refuse l'autorisation. Le changer oblige les utilisateurs à se
	// reconnecter (le nouveau droit n'est porté que par un jeton fraîchement
	// émis). Repasser à « public » désactive de fait les briques 42 du dashboard.
	oauthScope := os.Getenv("MOULINETTE_42_SCOPE")
	if oauthScope == "" {
		oauthScope = "public projects"
	}
	oauthConfig := auth.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		Scope:        oauthScope,
		// Vide en temps normal (l'API 42 officielle). Permet de pointer vers
		// un mock local pour développer/tester sans dépendre de l'intra.
		BaseURL: os.Getenv("MOULINETTE_42_BASE_URL"),
	}
	sessions := auth.NewStore()
	// Cookie d'identité signé, partagé avec ft_intra sur .ft-moulinette.fr : un
	// user connecté sur la racine (ft_intra) est reconnu ici sans re-login.
	// Sans secret : mode mono-service (dev), login OAuth local classique.
	if secret := os.Getenv("MOULINETTE_SESSION_SECRET"); secret != "" {
		sessions.UseIdentity([]byte(secret), os.Getenv("MOULINETTE_COOKIE_DOMAIN"))
	} else {
		log.Println("MOULINETTE_SESSION_SECRET absent : SSO inter-services désactivé (dev mono-service)")
	}
	// En prod, le login se fait sur la racine ft_intra : on y renvoie les
	// visiteurs non connectés (ex. https://ft-moulinette.fr/login).
	if loginURL := os.Getenv("MOULINETTE_LOGIN_URL"); loginURL != "" {
		api.SetLoginURL(loginURL)
	}

	hist, err := history.New(historyDir)
	if err != nil {
		log.Fatal("initialisation de l'historique: ", err)
	}

	locksStore, err := locks.New(locksPath)
	if err != nil {
		log.Fatal("initialisation des verrous: ", err)
	}

	// Pool de workers : chaque worker prend un job dans la queue,
	// le fait tourner dans la sandbox Docker, et stocke le résultat.
	workerCount := 3
	q := queue.New(workerCount, sandbox.Run, hist)
	if err := q.LoadHistory(); err != nil {
		log.Fatal("chargement de l'historique: ", err)
	}
	q.Start()
	defer q.Stop()

	// Identifiant unique de CE démarrage du serveur : sert à distinguer, sur
	// la page principale, "jobs de la session serveur actuelle" (visibles,
	// survivent à un F5) de "jobs d'un précédent démarrage" (uniquement
	// visibles sur /history désormais).
	serverBootID := fmt.Sprintf("%d", time.Now().UnixNano())

	campusName := os.Getenv("MOULINETTE_CAMPUS_NAME")
	if campusName == "" {
		campusName = "Perpignan" // Valeur par défaut
	}
	poolCachePath := os.Getenv("MOULINETTE_POOL_CACHE")
	if poolCachePath == "" {
		poolCachePath = "data/pool_cache.json"
	}
	poolHistoryPath := os.Getenv("MOULINETTE_POOL_HISTORY")
	if poolHistoryPath == "" {
		poolHistoryPath = "data/pool_history.json"
	}
	poolService := pool.NewService(clientID, clientSecret, campusName, poolCachePath, poolHistoryPath)
	// Rafraîchit Score/Projets côté serveur en continu, sinon les classements
	// ne se mettent à jour que quand un navigateur a la page ouverte.
	poolService.StartRefreshLoop()

	// Client 42 user-scoped du dashboard : vise la même base que le flux OAuth
	// (API réelle, ou mock via MOULINETTE_42_BASE_URL pour le dev).
	ftService := fortytwo.NewService(oauthConfig.APIBaseURL())

	router, err := api.NewRouter(q, testsDir, oauthConfig, sessions, serverBootID, locksStore, adminLogins, poolService, ftService, maintenance)
	if err != nil {
		log.Fatal(err)
	}

	srv := &http.Server{Addr: addr, Handler: router}

	// Arrêt propre sur Ctrl+C / SIGTERM : on flush l'historique en attente
	// de tous les utilisateurs avant de quitter (limite le risque de perte
	// par rapport à une coupure brutale, où seul le beacon de fermeture
	// d'onglet ou la déconnexion aurait pu déclencher le flush).
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("ft_moulinette écoute sur %s", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()

	<-ctx.Done()
	stop()
	log.Println("arrêt en cours : flush de l'historique en attente…")

	if err := q.FlushAllSessions(); err != nil {
		log.Printf("échec du flush final de l'historique: %v", err)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("arrêt du serveur HTTP: %v", err)
	}
}
