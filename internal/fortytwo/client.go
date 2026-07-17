// Package fortytwo est le client de l'API 42 côté utilisateur : chaque
// requête est signée avec le token OAuth de la session (et non le token
// applicatif du service pool), pour lire les données du compte connecté et
// répartir le quota d'appels par utilisateur.
//
// Le client encaisse les caprices de l'API 42 et rend le dashboard instantané
// au rechargement :
//   - cache TTL par utilisateur, servi « stale-while-revalidate » : une
//     donnée expirée est renvoyée immédiatement pendant qu'un rafraîchissement
//     part en arrière-plan — un F5 n'attend jamais l'API ;
//   - dédoublonnage des appels identiques en vol (singleflight) ;
//   - débit limité à ~2 req/s par utilisateur via des créneaux réservés :
//     les appels distincts partent espacés mais s'exécutent en parallèle ;
//   - retry unique sur 429/5xx et disjoncteur pendant les vraies pannes.
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
	defaultMinInterval = 650 * time.Millisecond // limite API 42 : 2 req/s par token
	maxAttempts        = 2                      // 1 seul retry : une carte doit échouer vite
	breakerThreshold   = 3                      // échecs consécutifs avant d'ouvrir le disjoncteur
	breakerCooldown    = 45 * time.Second
	errorCacheTTL      = 30 * time.Second // mémoire courte d'un échec, pour ne pas marteler l'API
	staleGrace         = 15 * time.Minute // au-delà de l'expiration + cette marge, on ne sert plus le périmé
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

// call est un appel réseau en vol, partagé entre les demandeurs de la même clé.
type call struct {
	done chan struct{}
	body []byte
	err  error
}

// userState porte le cache, les appels en vol et le budget de débit d'un
// utilisateur. Le mutex ne protège que l'état : il n'est JAMAIS tenu pendant
// un aller-retour réseau — une carte en cache n'attend donc jamais une carte
// qui télécharge.
type userState struct {
	mu          sync.Mutex
	nextAllowed time.Time // prochain créneau d'appel disponible
	cache       map[string]cacheEntry
	inflight    map[string]*call
	failStreak  int
	downUntil   time.Time
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
		us = &userState{cache: make(map[string]cacheEntry), inflight: make(map[string]*call)}
		c.users[login] = us
	}
	return us
}

// get sert une lecture depuis le cache quand c'est possible : frais → tel
// quel ; périmé depuis peu → tel quel AVEC rafraîchissement en arrière-plan ;
// absent → téléchargement bloquant, partagé si le même est déjà en vol.
func (c *Client) get(ctx context.Context, login, accessToken, pathname string, params url.Values, ttl time.Duration, out any) error {
	key := pathname
	if len(params) > 0 {
		key += "?" + params.Encode()
	}

	us := c.user(login)
	us.mu.Lock()

	if e, ok := us.cache[key]; ok {
		if time.Now().Before(e.expiresAt) {
			us.mu.Unlock()
			if e.err != nil {
				return e.err
			}
			return json.Unmarshal(e.body, out)
		}
		if e.err == nil && time.Since(e.expiresAt) < staleGrace {
			c.refreshLocked(us, accessToken, key, pathname, params, ttl)
			us.mu.Unlock()
			return json.Unmarshal(e.body, out)
		}
	}

	// Rien d'utilisable : si le même appel est déjà en vol, on attend SON
	// résultat au lieu d'en lancer un deuxième.
	if cl, ok := us.inflight[key]; ok {
		us.mu.Unlock()
		select {
		case <-cl.done:
		case <-ctx.Done():
			return ctx.Err()
		}
		if cl.err != nil {
			return cl.err
		}
		return json.Unmarshal(cl.body, out)
	}

	cl, slot, err := c.beginFetchLocked(us, key)
	us.mu.Unlock()
	if err != nil {
		return err // disjoncteur ouvert
	}
	c.finishFetch(ctx, us, cl, slot, accessToken, key, pathname, params, ttl)
	if cl.err != nil {
		return cl.err
	}
	return json.Unmarshal(cl.body, out)
}

// refreshLocked (appelé sous us.mu) lance un rafraîchissement en arrière-plan
// s'il n'y en a pas déjà un en vol pour cette clé — et jamais pendant une
// panne : on continuera de servir le périmé sans insister.
func (c *Client) refreshLocked(us *userState, accessToken, key, pathname string, params url.Values, ttl time.Duration) {
	if _, ok := us.inflight[key]; ok {
		return
	}
	if time.Now().Before(us.downUntil) {
		return
	}
	cl, slot, err := c.beginFetchLocked(us, key)
	if err != nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		c.finishFetch(ctx, us, cl, slot, accessToken, key, pathname, params, ttl)
	}()
}

// reserveSlot réserve le prochain créneau de débit (variante autonome, pour
// les retries qui doivent repasser par la file au lieu de percuter les
// appels en vol — c'est ce qui déclenchait le « Spam Rate Limit » de 42).
func (c *Client) reserveSlot(us *userState) time.Time {
	us.mu.Lock()
	defer us.mu.Unlock()
	slot := time.Now()
	if us.nextAllowed.After(slot) {
		slot = us.nextAllowed
	}
	us.nextAllowed = slot.Add(c.minInterval)
	return slot
}

// beginFetchLocked réserve le prochain créneau de débit et enregistre l'appel
// en vol. Appelé sous us.mu.
func (c *Client) beginFetchLocked(us *userState, key string) (*call, time.Time, error) {
	if time.Now().Before(us.downUntil) {
		return nil, time.Time{}, ErrDown
	}
	slot := time.Now()
	if us.nextAllowed.After(slot) {
		slot = us.nextAllowed
	}
	us.nextAllowed = slot.Add(c.minInterval)
	cl := &call{done: make(chan struct{})}
	us.inflight[key] = cl
	return cl, slot, nil
}

// finishFetch exécute l'aller-retour réseau (hors verrou), puis enregistre le
// résultat : cache, disjoncteur, et réveil des demandeurs en attente.
func (c *Client) finishFetch(ctx context.Context, us *userState, cl *call, slot time.Time, accessToken, key, pathname string, params url.Values, ttl time.Duration) {
	body, err := c.doHTTP(ctx, us, slot, accessToken, pathname, params)

	us.mu.Lock()
	var apiErr *APIError
	switch {
	case err == nil:
		us.okLocked()
		us.cache[key] = cacheEntry{body: body, expiresAt: time.Now().Add(ttl)}
		us.gcLocked()
	case errors.As(err, &apiErr) && apiErr.Status < 500 && apiErr.Status != http.StatusTooManyRequests:
		// Refus applicatif : l'API vit. Les refus stables (scope, inexistant)
		// se mémorisent pour tout le TTL, le reste brièvement.
		us.okLocked()
		cacheTTL := errorCacheTTL
		if apiErr.Status == http.StatusForbidden || apiErr.Status == http.StatusNotFound {
			cacheTTL = ttl
		}
		us.cache[key] = cacheEntry{err: err, expiresAt: time.Now().Add(cacheTTL)}
	default:
		// Panne (réseau, 5xx, 429 épuisé) : on compte l'échec, mais on GARDE
		// l'éventuelle donnée périmée — mieux vaut la resservir qu'un panneau
		// d'erreur, elle re-tentera un rafraîchissement dans errorCacheTTL.
		us.failLocked(err)
		if old, ok := us.cache[key]; ok && old.err == nil {
			old.expiresAt = time.Now().Add(errorCacheTTL)
			us.cache[key] = old
		} else {
			us.cache[key] = cacheEntry{err: err, expiresAt: time.Now().Add(errorCacheTTL)}
		}
	}
	delete(us.inflight, key)
	cl.body, cl.err = body, err
	close(cl.done)
	us.mu.Unlock()
}

// doHTTP attend son créneau de débit puis fait l'aller-retour, avec un retry
// sur 429/5xx. Aucun verrou tenu ici : les appels distincts se recouvrent.
func (c *Client) doHTTP(ctx context.Context, us *userState, slot time.Time, accessToken, pathname string, params url.Values) ([]byte, error) {
	if wait := time.Until(slot); wait > 0 {
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	u, err := url.Parse(c.base + pathname)
	if err != nil {
		return nil, err
	}
	u.RawQuery = params.Encode()

	for attempt := 1; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)

		resp, err := c.http.Do(req)
		if err != nil {
			if attempt < maxAttempts && ctx.Err() == nil {
				if wait := time.Until(c.reserveSlot(us)); wait > 0 {
					select {
					case <-time.After(wait):
					case <-ctx.Done():
						return nil, ctx.Err()
					}
				}
				continue
			}
			return nil, fmt.Errorf("API 42 injoignable sur %s : %w", u.Path, err)
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
				// Le retry repasse par la file de débit : sans ça il percute
				// un autre appel en vol et re-déclenche le « spam limit ».
				deadline := time.Now().Add(retryAfter)
				if slot := c.reserveSlot(us); slot.After(deadline) {
					deadline = slot
				}
				select {
				case <-time.After(time.Until(deadline)):
				case <-ctx.Done():
					return nil, ctx.Err()
				}
				continue
			}
			return nil, &APIError{Status: resp.StatusCode, Path: u.Path, Body: strings.TrimSpace(string(body))}
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
			resp.Body.Close()
			return nil, &APIError{Status: resp.StatusCode, Path: u.Path, Body: strings.TrimSpace(string(body))}
		}

		body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodySize))
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("lecture de la réponse 42 sur %s : %w", u.Path, err)
		}
		return body, nil
	}
}

// mutate exécute une écriture (POST/DELETE en formulaire) : jamais de cache
// ni de retry (un POST n'est pas idempotent). Une mutation est toujours une
// action utilisateur explicite : elle ignore le disjoncteur et ne tient pas
// le verrou pendant le réseau. En cas de succès, les entrées de cache dont la
// clé commence par un des préfixes donnés sont invalidées.
func (c *Client) mutate(ctx context.Context, login, accessToken, method, pathname string, form url.Values, invalidatePrefixes ...string) ([]byte, error) {
	us := c.user(login)

	us.mu.Lock()
	slot := time.Now()
	if us.nextAllowed.After(slot) {
		slot = us.nextAllowed
	}
	us.nextAllowed = slot.Add(c.minInterval)
	us.mu.Unlock()

	if wait := time.Until(slot); wait > 0 {
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+pathname, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		us.mu.Lock()
		defer us.mu.Unlock()
		return nil, us.failLocked(fmt.Errorf("API 42 injoignable sur %s : %w", pathname, err))
	}
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxBodySize))
	resp.Body.Close()

	us.mu.Lock()
	defer us.mu.Unlock()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		apiErr := &APIError{Status: resp.StatusCode, Path: pathname, Body: strings.TrimSpace(string(respBody[:min(len(respBody), 300)]))}
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			return nil, us.failLocked(apiErr)
		}
		us.okLocked()
		return nil, apiErr
	}

	us.okLocked()
	for _, prefix := range invalidatePrefixes {
		for k := range us.cache {
			if strings.HasPrefix(k, prefix) {
				delete(us.cache, k)
			}
		}
	}
	return respBody, nil
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

// okLocked : une requête a abouti (ou a reçu une réponse saine type 4xx) —
// l'API est vivante, on referme le disjoncteur.
func (us *userState) okLocked() {
	us.failStreak = 0
	us.downUntil = time.Time{}
}

// gcLocked purge les entrées expirées depuis trop longtemps pour qu'une
// longue session ne fasse pas grossir le cache indéfiniment.
func (us *userState) gcLocked() {
	if len(us.cache) < 64 {
		return
	}
	now := time.Now()
	for k, e := range us.cache {
		if now.Sub(e.expiresAt) > staleGrace {
			delete(us.cache, k)
		}
	}
}
