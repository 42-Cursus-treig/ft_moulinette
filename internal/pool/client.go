package pool

import (
	"context"
	"encoding/json"
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
	apiBase            = "https://api.intra.42.fr"
	minRequestInterval = 550 * time.Millisecond // limite API 42 : 2 req/s
	maxRetries         = 3
)

// client est un client API 42 en client_credentials : les requêtes sont
// sérialisées (mutex) pour respecter la rate limit globale, comme api42.js.
type client struct {
	id, secret string
	http       *http.Client

	mu             sync.Mutex
	token          string
	tokenExpiresAt time.Time
	lastRequestAt  time.Time
}

func newClient(id, secret string) *client {
	return &client{
		id:     id,
		secret: secret,
		http:   &http.Client{Timeout: 20 * time.Second},
	}
}

func (c *client) fetchToken(ctx context.Context) error {
	form := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {c.id},
		"client_secret": {c.secret},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiBase+"/oauth/token", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("OAuth 42: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("échec OAuth 42 (%d) : %s", resp.StatusCode, body)
	}

	var data struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return fmt.Errorf("réponse OAuth 42 invalide: %w", err)
	}
	c.token = data.AccessToken
	c.tokenExpiresAt = time.Now().Add(time.Duration(data.ExpiresIn-60) * time.Second)
	return nil
}

func (c *client) getTokenLocked(ctx context.Context) (string, error) {
	if c.token == "" || time.Now().After(c.tokenExpiresAt) {
		if err := c.fetchToken(ctx); err != nil {
			return "", err
		}
	}
	return c.token, nil
}

// get exécute un GET throttlé et décode la réponse JSON dans out.
func (c *client) get(ctx context.Context, pathname string, params url.Values, out any) error {
	u, err := url.Parse(apiBase + pathname)
	if err != nil {
		return err
	}
	u.RawQuery = params.Encode()

	c.mu.Lock()
	defer c.mu.Unlock()

	for attempt := 0; ; attempt++ {
		if wait := minRequestInterval - time.Since(c.lastRequestAt); wait > 0 {
			select {
			case <-time.After(wait):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		c.lastRequestAt = time.Now()

		token, err := c.getTokenLocked(ctx)
		if err != nil {
			return err
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)

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

		if resp.StatusCode == http.StatusUnauthorized && attempt < maxRetries {
			resp.Body.Close()
			c.token = "" // token révoqué : on en reprend un au tour suivant
			continue
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
			resp.Body.Close()
			return fmt.Errorf("API 42 %d sur %s : %s", resp.StatusCode, u.Path, body)
		}

		err = json.NewDecoder(resp.Body).Decode(out)
		resp.Body.Close()
		if err != nil {
			return fmt.Errorf("réponse API 42 invalide sur %s : %w", u.Path, err)
		}
		return nil
	}
}

// getAll agrège toutes les pages (page[size]=100).
func getAll[T any](ctx context.Context, c *client, pathname string, params url.Values) ([]T, error) {
	var all []T
	for page := 1; ; page++ {
		p := url.Values{}
		for k, v := range params {
			p[k] = v
		}
		p.Set("page[size]", "100")
		p.Set("page[number]", strconv.Itoa(page))

		var chunk []T
		if err := c.get(ctx, pathname, p, &chunk); err != nil {
			return nil, err
		}
		all = append(all, chunk...)
		if len(chunk) < 100 {
			return all, nil
		}
	}
}
