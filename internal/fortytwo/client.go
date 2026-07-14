// Package fortytwo est le client de l'API 42 côté utilisateur : chaque
// requête est signée avec le token OAuth de la session (et non le token
// applicatif du service pool), pour lire les données du compte connecté et
// répartir le quota d'appels par utilisateur.
//
// Le client encaisse les caprices de l'API 42 : requêtes sérialisées par
// utilisateur pour respecter la limite de 2 req/s par token, cache TTL des
// réponses, retry unique sur 429/5xx, et disjoncteur qui répond « en panne »
// immédiatement pendant un moment quand 42 est réellement tombée — plutôt
// que de faire patienter chaque carte du dashboard 20 secondes chacune.
package fortytwo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultMinInterval = 550 * time.Millisecond // limite API 42 : 2 req/s par token
	maxAttempts        = 2                      // 1 seul retry : une carte doit échouer vite
	breakerThreshold   = 2                      // échecs consécutifs avant d'ouvrir le disjoncteur
	breakerCooldown    = 90 * time.Second
	errorCacheTTL      = 30 * time.Second // mémoire courte d'un échec, pour ne pas marteler l'API
	maxBodySize        = 4 << 20          // /v2/me peut être volumineux, mais pas à ce point
)

// ErrDown est renvoyée sans appel réseau tant que le disjoncteur est ouvert.
var ErrDown = errors.New("API 42 indisponible (échecs répétés, nouvel essai différé)")

// APIError décrit une réponse non-2xx de l'API 42, pour que les cartes du
// dashboard distinguent « accès refusé » (scope, token) de « en panne ».
type APIError struct {
	Status int
	Path   string
	Body   string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("API 42 %d sur %s : %s", e.Status, e.Path, e.Body)
}

type cacheEntry struct {
	body      []byte
	err       error
	expiresAt time.Time
}

// userState sérialise les requêtes d'un même utilisateur : son mutex sert à
// la fois de limiteur de débit et de « singleflight » — quand deux cartes
// veulent /v2/me en même temps, la première remplit le cache, la seconde le
// lit au lieu de refaire l'appel.
type userState struct {
	mu            sync.Mutex
	lastRequestAt time.Time
	cache         map[string]cacheEntry
	failStreak    int
	downUntil     time.Time
}

type Client struct {
	base        string
	http        *http.Client
	minInterval time.Duration

	mu    sync.Mutex
	users map[string]*userState
}

func New(baseURL string) *Client {
	return &Client{
		base:        strings.TrimSuffix(baseURL, "/"),
		http:        &http.Client{Timeout: 10 * time.Second},
		minInterval: defaultMinInterval,
		users:       make(map[string]*userState),
	}
}

// SetMinInterval ajuste l'espacement minimal entre deux requêtes d'un même
// utilisateur. La valeur par défaut respecte la limite de l'API 42 ; les
// tests la mettent à zéro pour ne pas s'endormir entre chaque appel.
func (c *Client) SetMinInterval(d time.Duration) {
	c.minInterval = d
}

func (c *Client) user(login string) *userState {
	c.mu.Lock()
	defer c.mu.Unlock()
	us, ok := c.users[login]
	if !ok {
		us = &userState{cache: make(map[string]cacheEntry)}
		c.users[login] = us
	}
	return us
}

// get exécute un GET authentifié et garde la réponse brute en cache pendant
// ttl. Les échecs sont aussi mis en cache : brièvement pour une panne (elle
// peut se résorber), pour tout le ttl quand la réponse est stable (403 de
// scope, 404) — inutile de redemander à chaque visite.
func (c *Client) get(ctx context.Context, login, accessToken, pathname string, params url.Values, ttl time.Duration, out any) error {
	key := pathname
	if len(params) > 0 {
		key += "?" + params.Encode()
	}

	us := c.user(login)
	us.mu.Lock()
	defer us.mu.Unlock()

	if e, ok := us.cache[key]; ok && time.Now().Before(e.expiresAt) {
		if e.err != nil {
			return e.err
		}
		return json.Unmarshal(e.body, out)
	}

	body, err := c.fetchLocked(ctx, us, accessToken, pathname, params)
	if err != nil {
		cacheTTL := errorCacheTTL
		var apiErr *APIError
		if errors.As(err, &apiErr) && (apiErr.Status == http.StatusForbidden || apiErr.Status == http.StatusNotFound) {
			cacheTTL = ttl
		}
		us.cache[key] = cacheEntry{err: err, expiresAt: time.Now().Add(cacheTTL)}
		return err
	}
	us.cache[key] = cacheEntry{body: body, expiresAt: time.Now().Add(ttl)}
	us.gcLocked()
	return json.Unmarshal(body, out)
}

// fetchLocked fait l'aller-retour HTTP (throttle, retry, disjoncteur).
// Appelé sous us.mu : ne bloque que les requêtes de ce même utilisateur.
func (c *Client) fetchLocked(ctx context.Context, us *userState, accessToken, pathname string, params url.Values) ([]byte, error) {
	if time.Now().Before(us.downUntil) {
		return nil, ErrDown
	}

	u, err := url.Parse(c.base + pathname)
	if err != nil {
		return nil, err
	}
	u.RawQuery = params.Encode()

	for attempt := 1; ; attempt++ {
		if wait := c.minInterval - time.Since(us.lastRequestAt); wait > 0 {
			select {
			case <-time.After(wait):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		us.lastRequestAt = time.Now()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)

		resp, err := c.http.Do(req)
		if err != nil {
			if attempt < maxAttempts && ctx.Err() == nil {
				continue
			}
			return nil, us.failLocked(fmt.Errorf("API 42 injoignable sur %s : %w", u.Path, err))
		}

		// 429 et 5xx : un seul retry, en respectant Retry-After s'il est court.
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
			resp.Body.Close()
			if attempt < maxAttempts {
				retryAfter := 1200 * time.Millisecond
				if s := resp.Header.Get("Retry-After"); s != "" {
					if secs, err := strconv.ParseFloat(s, 64); err == nil && secs > 0 && secs < 10 {
						retryAfter = time.Duration(secs * float64(time.Second))
					}
				}
				select {
				case <-time.After(retryAfter):
				case <-ctx.Done():
					return nil, ctx.Err()
				}
				continue
			}
			return nil, us.failLocked(&APIError{Status: resp.StatusCode, Path: u.Path, Body: strings.TrimSpace(string(body))})
		}

		// Autre non-200 (401, 403, 404…) : réponse saine d'une API en vie,
		// on ne compte pas d'échec pour le disjoncteur.
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
			resp.Body.Close()
			us.failStreak = 0
			return nil, &APIError{Status: resp.StatusCode, Path: u.Path, Body: strings.TrimSpace(string(body))}
		}

		body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodySize))
		resp.Body.Close()
		if err != nil {
			return nil, us.failLocked(fmt.Errorf("lecture de la réponse 42 sur %s : %w", u.Path, err))
		}
		us.failStreak = 0
		return body, nil
	}
}

// failLocked compte les échecs consécutifs et ouvre le disjoncteur au-delà
// du seuil : pendant une vraie panne 42, les cartes suivantes reçoivent
// ErrDown immédiatement au lieu d'attendre leurs propres timeouts.
func (us *userState) failLocked(err error) error {
	us.failStreak++
	if us.failStreak >= breakerThreshold {
		us.downUntil = time.Now().Add(breakerCooldown)
		us.failStreak = 0
	}
	return err
}

// gcLocked purge les entrées expirées pour qu'une longue session ne fasse
// pas grossir le cache indéfiniment.
func (us *userState) gcLocked() {
	if len(us.cache) < 64 {
		return
	}
	now := time.Now()
	for k, e := range us.cache {
		if now.After(e.expiresAt) {
			delete(us.cache, k)
		}
	}
}
