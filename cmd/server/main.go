package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	// Embarque la base de fuseaux horaires dans le binaire pour que
	// time.LoadLocation("Europe/Paris") fonctionne même dans un conteneur
	// sans paquet tzdata (l'affichage des horaires d'exam en dépend).
	_ "time/tzdata"

	"github.com/tristan-reig/ft-moulinette/internal/api"
	"github.com/tristan-reig/ft-moulinette/internal/auth"
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

	// Fuseau par défaut du serveur : heure de France, pour que time.Now() et
	// tous les affichages (horaires d'exam, « MàJ » des classements, logs)
	// soient en heure locale française sans conversion explicite.
	if loc, err := time.LoadLocation("Europe/Paris"); err == nil {
		time.Local = loc
	} else {
		log.Printf("fuseau Europe/Paris indisponible, on garde %s : %v", time.Local, err)
	}

	addr := os.Getenv("FT_MOULINETTE_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	testsDir := os.Getenv("FT_MOULINETTE_TESTS_DIR")
	if testsDir == "" {
		testsDir = "tests"
	}
	historyDir := os.Getenv("FT_MOULINETTE_HISTORY_DIR")
	if historyDir == "" {
		historyDir = "data/history"
	}
	locksPath := os.Getenv("FT_MOULINETTE_LOCKS_FILE")
	if locksPath == "" {
		locksPath = "data/locked_projects.json"
	}

	adminLoginsEnv := os.Getenv("FT_MOULINETTE_ADMIN_LOGINS")
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

	maintenance := os.Getenv("FT_MOULINETTE_MAINTENANCE") == "true"
	if maintenance {
		log.Println("MODE MAINTENANCE actif : seuls les admins ont accès")
	}

	clientID := os.Getenv("FT_42_CLIENT_ID")
	clientSecret := os.Getenv("FT_42_CLIENT_SECRET")
	redirectURL := os.Getenv("FT_42_REDIRECT_URL")
	if redirectURL == "" {
		redirectURL = "http://localhost:9090/auth/callback"
	}
	if clientID == "" || clientSecret == "" {
		log.Fatal("FT_42_CLIENT_ID et FT_42_CLIENT_SECRET sont requis " +
			"(créez une application sur https://profile.intra.42.fr/oauth/applications/new)")
	}
	// Scope OAuth demandé à 42. Défaut « public » (profil + classement) ; le
	// scope demandé doit être coché sur l'app côté intra. Changer ce scope
	// oblige les utilisateurs à se reconnecter (le nouveau droit n'est porté
	// que par un jeton fraîchement émis).
	oauthScope := os.Getenv("FT_42_SCOPE")
	if oauthScope == "" {
		oauthScope = "public"
	}
	oauthConfig := auth.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		Scope:        oauthScope,
		// Vide en temps normal (l'API 42 officielle). Permet de pointer vers
		// un mock local pour développer/tester sans dépendre de l'intra.
		BaseURL: os.Getenv("FT_42_BASE_URL"),
	}

	sessions := auth.NewStore()
	// Cookie d'identité signé, partagé avec ft_intra sur .ft-moulinette.fr : un
	// user connecté sur la racine (ft_intra) est reconnu ici sans re-login.
	// Sans secret : mode mono-service (dev), login OAuth local classique.
	// Format du cookie : voir internal/auth/identity.go — contrat partagé avec
	// ft_intra, toute modification doit être appliquée des deux côtés.
	if secret := os.Getenv("FT_SSO_SECRET"); secret != "" {
		sessions.UseIdentity([]byte(secret), os.Getenv("FT_SSO_COOKIE_DOMAIN"))
	} else {
		log.Println("FT_SSO_SECRET absent : SSO inter-services désactivé (dev mono-service)")
	}
	// En prod, le login se fait sur la racine ft_intra : on y renvoie les
	// visiteurs non connectés (ex. https://ft-moulinette.fr/login).
	if loginURL := os.Getenv("FT_MOULINETTE_LOGIN_URL"); loginURL != "" {
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

	// Pool de workers : chaque worker prend un job dans la queue, le fait
	// tourner dans la sandbox Docker, et stocke le résultat.
	workerCount := 3
	q := queue.New(workerCount, sandbox.Run, hist)
	if err := q.LoadHistory(); err != nil {
		log.Fatal("chargement de l'historique: ", err)
	}
	q.Start()
	defer q.Stop()

	// Identifiant unique de CE démarrage du serveur : distingue, sur la page
	// principale, les jobs de la session serveur actuelle (visibles, survivent
	// à un F5) de ceux d'un précédent démarrage (visibles sur /history).
	serverBootID := fmt.Sprintf("%d", time.Now().UnixNano())

	campusName := os.Getenv("FT_MOULINETTE_CAMPUS_NAME")
	if campusName == "" {
		campusName = "Perpignan"
	}
	poolCachePath := os.Getenv("FT_MOULINETTE_POOL_CACHE")
	if poolCachePath == "" {
		poolCachePath = "data/pool_cache.json"
	}
	poolHistoryPath := os.Getenv("FT_MOULINETTE_POOL_HISTORY")
	if poolHistoryPath == "" {
		poolHistoryPath = "data/pool_history.json"
	}
	poolService := pool.NewService(clientID, clientSecret, campusName, poolCachePath, poolHistoryPath)

	// Session de piscine forcée : sans ces variables, la session affichée est
	// déduite de la date du serveur et bascule donc toute seule le 1er du mois.
	// Les poser (ex. july / 2026) fige la piscine servie — utile pour continuer
	// à consulter la session précédente une fois la suivante commencée. Vider
	// les deux variables et redémarrer restaure le comportement automatique.
	// À faire avant StartRefreshLoop : la boucle de fond lit la session.
	if monthEnv, yearEnv := os.Getenv("FT_MOULINETTE_POOL_MONTH"), os.Getenv("FT_MOULINETTE_POOL_YEAR"); monthEnv != "" || yearEnv != "" {
		poolYear, err := strconv.Atoi(strings.TrimSpace(yearEnv))
		if err != nil {
			log.Fatalf("FT_MOULINETTE_POOL_YEAR invalide (%q) : les deux variables "+
				"FT_MOULINETTE_POOL_MONTH et FT_MOULINETTE_POOL_YEAR doivent être "+
				"renseignées ensemble", yearEnv)
		}
		if err := poolService.ForceSession(monthEnv, poolYear); err != nil {
			log.Fatal("session de piscine forcée : ", err)
		}
		log.Printf("session de piscine forcée sur %s %d (FT_MOULINETTE_POOL_MONTH/_YEAR)", monthEnv, poolYear)
	}

	// Rafraîchit Score/Projets côté serveur en continu, sinon les classements
	// ne se mettent à jour que quand un navigateur a la page ouverte.
	poolService.StartRefreshLoop()

	router, err := api.NewRouter(q, testsDir, oauthConfig, sessions, serverBootID, locksStore, adminLogins, poolService, maintenance)
	if err != nil {
		log.Fatal(err)
	}

	srv := &http.Server{Addr: addr, Handler: router}

	// Arrêt propre sur Ctrl+C / SIGTERM : on flush l'historique en attente de
	// tous les utilisateurs avant de quitter.
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
