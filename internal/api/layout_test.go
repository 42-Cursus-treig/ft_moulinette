package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tristan-reig/ft-moulinette/internal/auth"
)

// TestWidgetsRegistrySync : le registre des widgets et la table des cartes
// doivent rester alignés — un widget sans carte donnerait un squelette
// éternel, une carte sans widget serait introuvable dans l'éditeur.
func TestWidgetsRegistrySync(t *testing.T) {
	if len(dashWidgets) != len(dashCards) {
		t.Errorf("%d widgets pour %d cartes", len(dashWidgets), len(dashCards))
	}
	for _, w := range dashWidgets {
		if _, ok := dashCards[w.ID]; !ok {
			t.Errorf("widget %q sans carte correspondante", w.ID)
		}
		if !dashSpanAllowed[w.Span] {
			t.Errorf("widget %q : largeur par défaut %d non autorisée", w.ID, w.Span)
		}
	}
}

func TestSanitizeLayout(t *testing.T) {
	in := []widgetPos{
		{ID: "hero", Span: 6},
		{ID: "inconnu", Span: 6},  // id inconnu : jeté
		{ID: "hero", Span: 12},    // doublon : jeté
		{ID: "points", Span: 999}, // largeur invalide : celle par défaut
	}
	out := sanitizeLayout(in)
	if len(out) != 2 {
		t.Fatalf("%d widgets après nettoyage, attendu 2 : %+v", len(out), out)
	}
	if out[0].ID != "hero" || out[0].Span != 6 {
		t.Errorf("hero : %+v", out[0])
	}
	if out[1].ID != "points" || out[1].Span != 6 { // défaut du registre
		t.Errorf("points : %+v (largeur par défaut attendue)", out[1])
	}
}

// TestLayoutStorePersistance : la disposition survit à un redémarrage.
func TestLayoutStorePersistance(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "layouts.json")
	s, err := newLayoutStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Get("alice"); ok {
		t.Fatal("store vide : Get doit répondre absent")
	}
	want := []widgetPos{{ID: "points", Span: 12}, {ID: "hero", Span: 6}}
	if err := s.Set("alice", want); err != nil {
		t.Fatal(err)
	}

	reloaded, err := newLayoutStore(path)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := reloaded.Get("alice")
	if !ok || len(got) != 2 || got[0].ID != "points" || got[0].Span != 12 {
		t.Errorf("après rechargement : %+v", got)
	}

	if err := reloaded.Reset("alice"); err != nil {
		t.Fatal(err)
	}
	if _, ok := reloaded.Get("alice"); ok {
		t.Errorf("après reset : la disposition devrait avoir disparu")
	}
}

// TestSaveLayoutEtRendu : l'endpoint d'enregistrement + le rendu de la page
// selon la disposition (ordre, widgets masqués en réserve).
func TestSaveLayoutEtRendu(t *testing.T) {
	m := newMock42(t)
	defer m.srv.Close()
	h, mux := newDashHandlers(t, m.srv.URL)
	mux.HandleFunc("POST /ui/dashboard/layout", h.requireAuthAPI(h.saveLayout))
	ck := loginAs(t, h, validToken())

	// Disposition réduite : deux widgets seulement, points en premier.
	rec := postJSON(mux, "/ui/dashboard/layout", `{"widgets":[{"id":"points","span":12},{"id":"hero","span":6}]}`, ck)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("enregistrement : code=%d corps=%s", rec.Code, rec.Body.String())
	}

	page := getWithCookie(mux, "/dashboard", ck).Body.String()
	if got := strings.Count(page, `hx-get="/ui/dashboard/`); got != 2 {
		t.Errorf("%d cartes rendues, attendu 2", got)
	}
	if !strings.Contains(page, `dash-span-12" hx-get="/ui/dashboard/points"`) {
		t.Errorf("points en pleine largeur attendu en tête : %s", page[:400])
	}
	if strings.Index(page, "/ui/dashboard/points") > strings.Index(page, "/ui/dashboard/hero") {
		t.Errorf("l'ordre enregistré n'est pas respecté")
	}
	// Les widgets masqués sont dans l'inventaire de l'éditeur.
	if !strings.Contains(page, `data-id="logtime"`) || !strings.Contains(page, "Inventaire") {
		t.Errorf("l'inventaire de l'éditeur doit lister les widgets masqués")
	}

	// Lot invalide : uniquement des ids inconnus → 400.
	if rec := postJSON(mux, "/ui/dashboard/layout", `{"widgets":[{"id":"zzz","span":6}]}`, ck); rec.Code != http.StatusBadRequest {
		t.Errorf("ids inconnus : 400 attendu (code=%d)", rec.Code)
	}

	// Reset : retour à la disposition par défaut (toutes les cartes).
	if rec := postJSON(mux, "/ui/dashboard/layout", `{"reset":true}`, ck); rec.Code != http.StatusNoContent {
		t.Fatalf("reset : code=%d", rec.Code)
	}
	page = getWithCookie(mux, "/dashboard", ck).Body.String()
	if got := strings.Count(page, `hx-get="/ui/dashboard/`); got != len(dashWidgets) {
		t.Errorf("après reset : %d cartes, attendu %d", got, len(dashWidgets))
	}
}

// TestRouterLayoutRoute : la route d'enregistrement est bien câblée dans le
// vrai routeur (un 401 sans session prouve qu'elle existe — une route absente
// donnerait 404).
func TestRouterLayoutRoute(t *testing.T) {
	router, err := NewRouter(nil, "tests", auth.Config{ClientID: "cid", ClientSecret: "sec"}, auth.NewStore(), "boot", nil, map[string]bool{}, nil,
		filepath.Join(t.TempDir(), "layouts.json"))
	if err != nil {
		t.Fatalf("NewRouter : %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/ui/dashboard/layout", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("code=%d, attendu 401 (route câblée, session absente)", rec.Code)
	}
}

// TestDashExamNonInscrit : sans inscription à un exam futur, la carte le dit.
func TestDashExamNonInscrit(t *testing.T) {
	mux42 := http.NewServeMux()
	mux42.HandleFunc("GET /v2/users/1/events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Un meetup à venir mais aucun événement de type exam : pas inscrit.
		fmt.Fprintf(w, `[{"name": "Meetup", "kind": "meet_up", "location": "Amphi", "begin_at": %q, "end_at": %q}]`,
			time.Now().Add(24*time.Hour).UTC().Format(time.RFC3339),
			time.Now().Add(26*time.Hour).UTC().Format(time.RFC3339))
	})
	srv := httptest.NewServer(mux42)
	defer srv.Close()

	h, mux := newDashHandlers(t, srv.URL)
	ck := loginAs(t, h, validToken())
	rec := getWithCookie(mux, "/ui/dashboard/exam", ck)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "inscrit à aucun exam") {
		t.Errorf("non inscrit : message attendu, obtenu %s", rec.Body.String())
	}
}
