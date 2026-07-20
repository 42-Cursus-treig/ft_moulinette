package api

import (
	"context"
	"log"
	"net/http"

	"github.com/tristan-reig/ft-moulinette/internal/auth"
)

type userContextKey struct{}

func userFromContext(ctx context.Context) (auth.User, bool) {
	u, ok := ctx.Value(userContextKey{}).(auth.User)
	return u, ok
}

// loginPage (GET /login) affiche l'écran d'accueil public avec le bouton de
// connexion 42. Un utilisateur déjà connecté est renvoyé directement au service.
func (h *handlers) loginPage(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.sessions.FromRequest(r); ok {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	if err := h.tmpl.ExecuteTemplate(w, "login", nil); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// authLogin (GET /auth/login) démarre le flux OAuth en redirigeant vers 42.
func (h *handlers) authLogin(w http.ResponseWriter, r *http.Request) {
	state, err := auth.RandomToken()
	if err != nil {
		http.Error(w, "erreur interne", http.StatusInternalServerError)
		return
	}
	h.sessions.SetStateCookie(w, state)
	http.Redirect(w, r, h.oauth.AuthorizeURL(state), http.StatusFound)
}

// authCallback (GET /auth/callback) traite le retour d'autorisation 42 :
// vérifie le state CSRF, échange le code, récupère le profil, ouvre la session.
func (h *handlers) authCallback(w http.ResponseWriter, r *http.Request) {
	if msg := r.URL.Query().Get("error"); msg != "" {
		http.Error(w, "connexion refusée par 42 : "+msg, http.StatusForbidden)
		return
	}

	state := r.URL.Query().Get("state")
	if !h.sessions.VerifyStateCookie(r, state) {
		http.Error(w, "état OAuth invalide, réessayez de vous connecter", http.StatusBadRequest)
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "code d'autorisation manquant", http.StatusBadRequest)
		return
	}

	token, err := h.oauth.Exchange(code)
	if err != nil {
		http.Error(w, "échange OAuth échoué: "+err.Error(), http.StatusBadGateway)
		return
	}

	user, err := h.oauth.FetchUser(token.AccessToken)
	if err != nil {
		http.Error(w, "récupération du profil 42 échouée: "+err.Error(), http.StatusBadGateway)
		return
	}

	if err := h.sessions.Create(w, *user, *token); err != nil {
		http.Error(w, "création de session échouée", http.StatusInternalServerError)
		return
	}
	// L'atterrissage post-login reste l'outil moulinette ; le dashboard se
	// rejoint depuis le badge de login dans l'en-tête.
	http.Redirect(w, r, "/", http.StatusFound)
}

// authLogout (POST /auth/logout) flush l'historique en attente de
// l'utilisateur avant de détruire sa session - sinon les corrections de
// cette visite resteraient bufferisées en mémoire jusqu'à la fermeture de
// l'onglet ou l'arrêt du serveur.
func (h *handlers) authLogout(w http.ResponseWriter, r *http.Request) {
	if user, ok := h.sessions.FromRequest(r); ok {
		if err := h.queue.FlushSession(user.Login); err != nil {
			log.Printf("flush de session à la déconnexion pour %s: %v", user.Login, err)
		}
	}
	h.sessions.Destroy(w, r)
	http.Redirect(w, r, "/", http.StatusFound)
}

// loginURL est la cible de redirection d'un visiteur non connecté. En prod
// (split), on la pointe vers la racine ft_intra qui porte le flux OAuth 42
// (MOULINETTE_LOGIN_URL) ; en dev mono-service, elle reste "/login" (local).
var loginURL = "/login"

// SetLoginURL configure la cible de redirection de login.
func SetLoginURL(u string) {
	if u != "" {
		loginURL = u
	}
}

// requireAuth protège les pages web : redirige vers l'écran d'accueil /login
// si non connecté, où l'utilisateur choisit lui-même de lancer le flux OAuth.
func (h *handlers) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := h.sessions.FromRequest(r)
		if !ok {
			http.Redirect(w, r, loginURL, http.StatusFound)
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), userContextKey{}, user)))
	}
}

// requireAuthFragment protège un fragment htmx : sans session valide on
// demande à htmx une redirection plein écran (HX-Redirect) vers /login. Une
// redirection 302 classique serait suivie silencieusement par htmx, qui
// injecterait la page de connexion à l'intérieur de la carte.
func (h *handlers) requireAuthFragment(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := h.sessions.FromRequest(r)
		if !ok {
			w.Header().Set("HX-Redirect", loginURL)
			w.WriteHeader(http.StatusOK)
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), userContextKey{}, user)))
	}
}

// requireAuthAPI protège l'API JSON : renvoie 401 au lieu de rediriger, pour
// rester exploitable par script/curl.
func (h *handlers) requireAuthAPI(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := h.sessions.FromRequest(r)
		if !ok {
			http.Error(w, `{"error":"authentification requise : connectez-vous sur `+"/"+` depuis un navigateur d'abord"}`, http.StatusUnauthorized)
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), userContextKey{}, user)))
	}
}
