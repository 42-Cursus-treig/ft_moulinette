package auth

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"sync"
	"time"
)

const (
	sessionCookie = "ft_moulinette_session"
	stateCookie   = "ft_moulinette_oauth_state"
	sessionTTL    = 7 * 24 * time.Hour
)

// RandomToken génère un jeton cryptographiquement sûr, utilisé pour les IDs de
// session et pour le state CSRF du flux OAuth.
func RandomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

type session struct {
	user      User
	expiresAt time.Time
	token     *tokenBox
}

// tokenBox isole le token 42 de la session derrière son propre verrou : le
// dashboard charge ses cartes en parallèle, et un refresh ne doit partir
// qu'une seule fois (42 invalide l'ancien refresh token après usage).
type tokenBox struct {
	mu  sync.Mutex
	tok Token
}

// Store est un annuaire de sessions en mémoire.
type Store struct {
	mu       sync.RWMutex
	sessions map[string]session
}

func NewStore() *Store {
	return &Store{sessions: make(map[string]session)}
}

// Create ouvre une session, y attache le token 42 de l'utilisateur et pose
// le cookie associé.
func (s *Store) Create(w http.ResponseWriter, user User, tok Token) error {
	token, err := RandomToken()
	if err != nil {
		return err
	}

	s.mu.Lock()
	s.sessions[token] = session{
		user:      user,
		expiresAt: time.Now().Add(sessionTTL),
		token:     &tokenBox{tok: tok},
	}
	s.mu.Unlock()

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(sessionTTL),
	})
	return nil
}

// FromRequest renvoie l'utilisateur du cookie de session s'il est valide et non expiré.
func (s *Store) FromRequest(r *http.Request) (User, bool) {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil {
		return User{}, false
	}

	s.mu.RLock()
	sess, ok := s.sessions[cookie.Value]
	s.mu.RUnlock()

	if !ok || time.Now().After(sess.expiresAt) {
		return User{}, false
	}
	return sess.user, true
}

// FreshToken renvoie un access token 42 utilisable pour la session de r, en
// le rafraîchissant d'abord auprès de 42 si nécessaire. Les appels concurrents
// d'une même session attendent le même refresh au lieu d'en déclencher
// plusieurs - l'appel réseau se fait sous le verrou du token, qui ne bloque
// que cette session.
func (s *Store) FreshToken(r *http.Request, cfg Config) (Token, error) {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil {
		return Token{}, fmt.Errorf("session absente")
	}

	s.mu.RLock()
	sess, ok := s.sessions[cookie.Value]
	s.mu.RUnlock()

	if !ok || time.Now().After(sess.expiresAt) || sess.token == nil {
		return Token{}, fmt.Errorf("session expirée")
	}

	box := sess.token
	box.mu.Lock()
	defer box.mu.Unlock()

	if box.tok.Usable() {
		return box.tok, nil
	}
	fresh, err := cfg.Refresh(box.tok.RefreshToken)
	if err != nil {
		return Token{}, fmt.Errorf("refresh du token 42: %w", err)
	}
	box.tok = *fresh
	return *fresh, nil
}

func (s *Store) Destroy(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		s.mu.Lock()
		delete(s.sessions, cookie.Value)
		s.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
}

// SetStateCookie pose le state CSRF, le temps de l'aller-retour vers 42.
func SetStateCookie(w http.ResponseWriter, state string) {
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookie,
		Value:    state,
		Path:     "/auth",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   600,
	})
}

// VerifyStateCookie compare le state reçu à celui posé avant redirection (anti-CSRF).
func VerifyStateCookie(r *http.Request, state string) bool {
	cookie, err := r.Cookie(stateCookie)
	return err == nil && state != "" && cookie.Value == state
}
