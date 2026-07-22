// Package auth gère la connexion via l'API OAuth2 de 42 : redirection,
// échange du code contre un token, récupération du profil (/v2/me), puis
// session cookie côté serveur.
package auth

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Config regroupe les identifiants de l'application déclarée sur l'intra 42.
type Config struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
	BaseURL      string
	// Scope demandé au flux OAuth (liste séparée par des espaces). Le jeton ne
	// portera QUE ces droits : « public » suffit pour le profil et le
	// classement, mais gérer les créneaux de correction (/v2/slots) exige aussi
	// « projects ». Le scope demandé doit être coché sur l'app côté intra,
	// sinon 42 refuse l'autorisation.
	Scope string
}

func (c Config) scope() string {
	if c.Scope != "" {
		return c.Scope
	}
	return "public"
}

func (c Config) baseURL() string {
	if c.BaseURL != "" {
		return c.BaseURL
	}
	return "https://api.intra.42.fr"
}

// APIBaseURL expose la base de l'API 42 aux autres packages (le client du
// dashboard doit viser le même serveur que le flux OAuth, mock compris).
func (c Config) APIBaseURL() string {
	return c.baseURL()
}

// AuthorizeURL construit l'URL de démarrage du flux OAuth. state = protection
// CSRF, à revérifier au retour sur /auth/callback.
func (c Config) AuthorizeURL(state string) string {
	v := url.Values{}
	v.Set("client_id", c.ClientID)
	v.Set("redirect_uri", c.RedirectURL)
	v.Set("response_type", "code")
	v.Set("scope", c.scope())
	v.Set("state", state)
	return c.baseURL() + "/oauth/authorize?" + v.Encode()
}

// Token est le jeu de jetons OAuth d'un utilisateur : l'access token expire
// vite (2 h chez 42), le refresh token permet d'en obtenir un neuf sans
// refaire tout le parcours de connexion.
type Token struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
}

// Usable dit si l'access token peut encore servir, avec une marge : un token
// qui expire dans quelques secondes serait refusé le temps d'arriver chez 42.
func (t Token) Usable() bool {
	return t.AccessToken != "" && time.Now().Before(t.ExpiresAt.Add(-30*time.Second))
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

// requestToken exécute un POST /oauth/token et normalise la réponse.
func (c Config) requestToken(form url.Values) (*Token, error) {
	resp, err := http.PostForm(c.baseURL()+"/oauth/token", form)
	if err != nil {
		return nil, fmt.Errorf("requête vers l'API 42 impossible: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token refusé par l'API 42 (%d): %s", resp.StatusCode, body)
	}

	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return nil, fmt.Errorf("réponse de l'API 42 illisible: %w", err)
	}
	if tr.AccessToken == "" {
		return nil, fmt.Errorf("l'API 42 n'a renvoyé aucun access_token")
	}
	if tr.ExpiresIn <= 0 {
		tr.ExpiresIn = 7200 // durée de vie par défaut des tokens 42
	}
	return &Token{
		AccessToken:  tr.AccessToken,
		RefreshToken: tr.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second),
	}, nil
}

// Exchange échange le code d'autorisation contre un jeu de tokens.
func (c Config) Exchange(code string) (*Token, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", c.ClientID)
	form.Set("client_secret", c.ClientSecret)
	form.Set("code", code)
	form.Set("redirect_uri", c.RedirectURL)
	return c.requestToken(form)
}

// Refresh obtient un nouvel access token à partir du refresh token : une
// session moulinette (7 jours) vit bien plus longtemps qu'un token 42 (2 h).
func (c Config) Refresh(refreshToken string) (*Token, error) {
	if refreshToken == "" {
		return nil, fmt.Errorf("aucun refresh token : reconnexion nécessaire")
	}
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	form.Set("client_id", c.ClientID)
	form.Set("client_secret", c.ClientSecret)
	return c.requestToken(form)
}

// User est le sous-ensemble du profil 42 (/v2/me) utilisé ici.
type User struct {
	ID    int    `json:"id"`
	Login string `json:"login"`
	Email string `json:"email"`
	Image struct {
		Link string `json:"link"`
	} `json:"image"`
	// PiscineOnly marque un compte qui n'a QUE des cursus de piscine (pas encore
	// cadet/student) : ces comptes sont bloqués sur tout le site. Calculé à la
	// connexion depuis cursus_users, puis propagé aux autres services du split
	// via le cookie d'identité signé (jamais renvoyé par l'API 42, d'où json:"-").
	PiscineOnly bool `json:"-"`
}

// FetchUser récupère le profil du titulaire de l'access token.
func (c Config) FetchUser(accessToken string) (*User, error) {
	req, err := http.NewRequest(http.MethodGet, c.baseURL()+"/v2/me", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("requête /v2/me impossible: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("/v2/me a renvoyé %d: %s", resp.StatusCode, body)
	}

	// On lit l'identité de base ET les cursus, pour décider si le compte n'est
	// qu'un piscineux (à bloquer) ou déjà un cadet/student.
	var raw struct {
		User
		CursusUsers []struct {
			Cursus struct {
				Slug string `json:"slug"`
			} `json:"cursus"`
		} `json:"cursus_users"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("profil 42 illisible: %w", err)
	}
	u := raw.User
	slugs := make([]string, 0, len(raw.CursusUsers))
	for _, cu := range raw.CursusUsers {
		slugs = append(slugs, cu.Cursus.Slug)
	}
	u.PiscineOnly = isPiscineOnly(slugs)
	return &u, nil
}

// isPiscineOnly indique qu'un profil n'a QUE des cursus de piscine (aucun cursus
// principal 42) : le compte est un piscineux, pas encore cadet/student. Un
// cursus est « piscine » si son slug contient "piscine" (c-piscine, 42-piscine,
// piscine-…) ; le cursus principal cadet/student porte le slug "42cursus", qui
// ne contient pas "piscine". Un profil sans aucun cursus n'est pas bloqué.
func isPiscineOnly(slugs []string) bool {
	if len(slugs) == 0 {
		return false
	}
	for _, s := range slugs {
		if !strings.Contains(strings.ToLower(s), "piscine") {
			return false
		}
	}
	return true
}
