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
)

// Config regroupe les identifiants de l'application déclarée sur l'intra 42.
type Config struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
	BaseURL      string
}

func (c Config) baseURL() string {
	if c.BaseURL != "" {
		return c.BaseURL
	}
	return "https://api.intra.42.fr"
}

// AuthorizeURL construit l'URL de démarrage du flux OAuth. state = protection
// CSRF, à revérifier au retour sur /auth/callback.
func (c Config) AuthorizeURL(state string) string {
	v := url.Values{}
	v.Set("client_id", c.ClientID)
	v.Set("redirect_uri", c.RedirectURL)
	v.Set("response_type", "code")
	v.Set("scope", "public")
	v.Set("state", state)
	return c.baseURL() + "/oauth/authorize?" + v.Encode()
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
}

// Exchange échange le code d'autorisation contre un access token.
func (c Config) Exchange(code string) (string, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", c.ClientID)
	form.Set("client_secret", c.ClientSecret)
	form.Set("code", code)
	form.Set("redirect_uri", c.RedirectURL)

	resp, err := http.PostForm(c.baseURL()+"/oauth/token", form)
	if err != nil {
		return "", fmt.Errorf("requête vers l'API 42 impossible: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("échange du code refusé par l'API 42 (%d): %s", resp.StatusCode, body)
	}

	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return "", fmt.Errorf("réponse de l'API 42 illisible: %w", err)
	}
	if tr.AccessToken == "" {
		return "", fmt.Errorf("l'API 42 n'a renvoyé aucun access_token")
	}
	return tr.AccessToken, nil
}

// User est le sous-ensemble du profil 42 (/v2/me) utilisé ici.
type User struct {
	ID    int    `json:"id"`
	Login string `json:"login"`
	Email string `json:"email"`
	Image struct {
		Link string `json:"link"`
	} `json:"image"`
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

	var u User
	if err := json.Unmarshal(body, &u); err != nil {
		return nil, fmt.Errorf("profil 42 illisible: %w", err)
	}
	return &u, nil
}
