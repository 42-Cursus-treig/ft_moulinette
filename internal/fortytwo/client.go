// Package fortytwo est un client de l'API v2 de 42 câblé sur le jeton OAuth de
// l'utilisateur courant (et non un jeton d'application). Le dashboard s'en sert
// pour lire le profil, les projets, les corrections et les défenses de la
// personne connectée, et pour écrire en son nom (inscription à un projet,
// feedback à un correcteur).
//
// À la différence de internal/pool (client_credentials, jeton partagé, données
// campus), chaque appel ici reçoit l'access token de la session en argument :
// aucun jeton n'est stocké dans le client. Le rafraîchissement du jeton est la
// responsabilité de la couche session (auth.Store.FreshToken).
package fortytwo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

// minRequestInterval borne le débit à ~2 req/s par jeton, la limite de l'API 42.
// Le throttle est PAR jeton : deux utilisateurs différents ne se bloquent pas
// l'un l'autre (contrairement au client campus partagé de internal/pool).
const (
	minRequestInterval = 550 * time.Millisecond
	maxRetries         = 3
	tokenPruneAfter    = 2 * time.Minute // oublie un jeton inactif pour borner la map
)

// ErrUnauthorized signale un 401 de l'API 42 : le jeton (déjà rafraîchi côté
// session) est refusé. Le handler doit inviter l'utilisateur à se reconnecter
// plutôt que d'afficher une erreur brute.
var ErrUnauthorized = errors.New("jeton 42 refusé : reconnexion nécessaire")

// Client parle à l'API v2 de 42 pour le compte d'un utilisateur.
type Client struct {
	base string
	http *http.Client

	mu       sync.Mutex
	lastSeen map[string]time.Time // dernier créneau réservé par jeton (throttle)
}

// NewClient construit un client visant baseURL (ex. https://api.intra.42.fr,
// ou un mock local pour le dev). Une base vide vise l'API 42 officielle.
func NewClient(baseURL string) *Client {
	if baseURL == "" {
		baseURL = "https://api.intra.42.fr"
	}
	return &Client{
		base:     baseURL,
		http:     &http.Client{Timeout: 20 * time.Second},
		lastSeen: make(map[string]time.Time),
	}
}

// reserve réserve le prochain créneau d'appel pour token et renvoie le temps à
// attendre avant de partir. La réservation se fait sous verrou (rapide) ; le
// sommeil, lui, a lieu hors verrou pour ne pas sérialiser les autres jetons.
func (c *Client) reserve(token string) time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	// Purge opportuniste des jetons inactifs pour éviter une croissance sans fin.
	for t, seen := range c.lastSeen {
		if now.Sub(seen) > tokenPruneAfter {
			delete(c.lastSeen, t)
		}
	}

	next := now
	if last, ok := c.lastSeen[token]; ok && last.After(now) {
		next = last
	}
	wait := next.Sub(now)
	c.lastSeen[token] = next.Add(minRequestInterval)
	return wait
}

// do exécute une requête throttlée avec retry (429/5xx, et 401 renvoyé tel quel
// via ErrUnauthorized) et décode la réponse JSON dans out (out peut être nil).
func (c *Client) do(ctx context.Context, token, method, pathname string, params url.Values, body io.Reader, out any) error {
	u, err := url.Parse(c.base + pathname)
	if err != nil {
		return err
	}
	if params != nil {
		u.RawQuery = params.Encode()
	}

	// bytes.Reader est rejouable ; on capture le corps pour pouvoir réémettre
	// sur retry (un io.Reader simple serait déjà consommé au 2e tour).
	var rawBody []byte
	if body != nil {
		if rawBody, err = io.ReadAll(body); err != nil {
			return err
		}
	}

	for attempt := 0; ; attempt++ {
		if wait := c.reserve(token); wait > 0 {
			select {
			case <-time.After(wait):
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		var reqBody io.Reader
		if rawBody != nil {
			reqBody = bytes.NewReader(rawBody)
		}
		req, err := http.NewRequestWithContext(ctx, method, u.String(), reqBody)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		if rawBody != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := c.http.Do(req)
		if err != nil {
			if attempt < maxRetries {
				continue
			}
			return fmt.Errorf("API 42 sur %s : %w", u.Path, err)
		}

		if (resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500) && attempt < maxRetries {
			retryAfter := 1500 * time.Millisecond
			if s := resp.Header.Get("Retry-After"); s != "" {
				if secs, err := strconv.ParseFloat(s, 64); err == nil && secs > 0 {
					retryAfter = time.Duration(secs * float64(time.Second))
				}
			}
			resp.Body.Close()
			select {
			case <-time.After(retryAfter):
			case <-ctx.Done():
				return ctx.Err()
			}
			continue
		}

		if resp.StatusCode == http.StatusUnauthorized {
			resp.Body.Close()
			return ErrUnauthorized
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
			resp.Body.Close()
			return fmt.Errorf("API 42 %d sur %s : %s", resp.StatusCode, u.Path, b)
		}

		if out == nil {
			resp.Body.Close()
			return nil
		}
		err = json.NewDecoder(resp.Body).Decode(out)
		resp.Body.Close()
		if err != nil {
			return fmt.Errorf("réponse API 42 illisible sur %s : %w", u.Path, err)
		}
		return nil
	}
}

// get exécute un GET throttlé et décode le JSON dans out.
func (c *Client) get(ctx context.Context, token, pathname string, params url.Values, out any) error {
	return c.do(ctx, token, http.MethodGet, pathname, params, nil, out)
}

// postJSON exécute un POST throttlé avec un corps JSON et décode la réponse
// dans out (out peut être nil si la réponse n'est pas exploitée).
func (c *Client) postJSON(ctx context.Context, token, pathname string, payload, out any) error {
	buf, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return c.do(ctx, token, http.MethodPost, pathname, nil, bytes.NewReader(buf), out)
}

// getAll agrège toutes les pages d'une collection (page[size]=100).
func getAll[T any](ctx context.Context, c *Client, token, pathname string, params url.Values) ([]T, error) {
	var all []T
	for page := 1; ; page++ {
		p := url.Values{}
		for k, v := range params {
			p[k] = v
		}
		p.Set("page[size]", "100")
		p.Set("page[number]", strconv.Itoa(page))

		var chunk []T
		if err := c.get(ctx, token, pathname, p, &chunk); err != nil {
			return nil, err
		}
		all = append(all, chunk...)
		if len(chunk) < 100 {
			return all, nil
		}
	}
}
