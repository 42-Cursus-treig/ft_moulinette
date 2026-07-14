package fortytwo

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func newTestClient(base string) *Client {
	c := New(base)
	c.SetMinInterval(0)
	return c
}

// TestCachePartageLaReponse : deux cartes qui veulent la même ressource ne
// déclenchent qu'un seul appel réseau (le cache sert de singleflight).
func TestCachePartageLaReponse(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Write([]byte(`{"id": 7}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	for i := 0; i < 3; i++ {
		var out struct{ ID int }
		if err := c.get(context.Background(), "alice", "tok", "/v2/me", nil, time.Minute, &out); err != nil {
			t.Fatalf("appel %d : %v", i, err)
		}
		if out.ID != 7 {
			t.Fatalf("appel %d : id=%d, attendu 7", i, out.ID)
		}
	}
	if hits.Load() != 1 {
		t.Errorf("%d appels réseau, attendu 1 (cache)", hits.Load())
	}
}

// TestErreur403EstMiseEnCache : une réponse « interdit » est stable, on ne
// redemande pas à chaque affichage de la carte.
func TestErreur403EstMiseEnCache(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	for i := 0; i < 2; i++ {
		var out any
		err := c.get(context.Background(), "alice", "tok", "/v2/x", nil, time.Minute, &out)
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.Status != http.StatusForbidden {
			t.Fatalf("appel %d : erreur %v, attendu APIError 403", i, err)
		}
	}
	if hits.Load() != 1 {
		t.Errorf("%d appels réseau, attendu 1 (erreur en cache)", hits.Load())
	}
}

// TestRetrySur500PuisSucces : un 500 isolé est retenté une fois.
func TestRetrySur500PuisSucces(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) == 1 {
			w.Header().Set("Retry-After", "0.01")
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		w.Write([]byte(`{"id": 1}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	var out struct{ ID int }
	if err := c.get(context.Background(), "alice", "tok", "/v2/me", nil, time.Minute, &out); err != nil {
		t.Fatalf("erreur inattendue : %v", err)
	}
	if hits.Load() != 2 {
		t.Errorf("%d appels, attendu 2 (500 puis succès)", hits.Load())
	}
}

// TestDisjoncteur : après deux ressources en échec, la troisième reçoit
// ErrDown sans appel réseau.
func TestDisjoncteur(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Retry-After", "0.01")
		http.Error(w, "cloudflare 521", http.StatusBadGateway)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	var out any
	for _, p := range []string{"/v2/a", "/v2/b"} {
		if err := c.get(context.Background(), "alice", "tok", p, nil, time.Minute, &out); err == nil {
			t.Fatalf("succès inattendu sur %s", p)
		}
	}
	before := hits.Load() // 2 ressources × 2 tentatives
	err := c.get(context.Background(), "alice", "tok", "/v2/c", nil, time.Minute, &out)
	if !errors.Is(err, ErrDown) {
		t.Fatalf("erreur %v, attendu ErrDown (disjoncteur ouvert)", err)
	}
	if hits.Load() != before {
		t.Errorf("le disjoncteur ouvert a quand même émis %d appel(s)", hits.Load()-before)
	}
}

// TestScaleUserTolereInvisible : l'API renvoie parfois la chaîne "invisible"
// à la place de l'objet corrector/correcteds.
func TestScaleUserTolereInvisible(t *testing.T) {
	var st ScaleTeam
	blob := `{"corrector": "invisible", "correcteds": "invisible", "final_mark": 84}`
	if err := json.Unmarshal([]byte(blob), &st); err != nil {
		t.Fatalf("unmarshal : %v", err)
	}
	if st.Corrector.Login != "" || len(st.Correcteds) != 0 {
		t.Errorf("forme invisible : corrector=%q correcteds=%v, attendu vides", st.Corrector.Login, st.Correcteds)
	}

	blob = `{"corrector": {"login": "bob"}, "correcteds": [{"login": "carol"}, {"login": "dave"}]}`
	if err := json.Unmarshal([]byte(blob), &st); err != nil {
		t.Fatalf("unmarshal : %v", err)
	}
	if st.Corrector.Login != "bob" || len(st.Correcteds) != 2 || st.Correcteds[1].Login != "dave" {
		t.Errorf("forme objet : corrector=%q correcteds=%v", st.Corrector.Login, st.Correcteds)
	}
}
