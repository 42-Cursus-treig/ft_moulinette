package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tristan-reig/ft-moulinette/internal/auth"
	"github.com/tristan-reig/ft-moulinette/internal/fortytwo"
)

// --- Mock de l'API 42 ---

// mock42 sert des réponses réalistes de l'API 42 pour tester le dashboard
// sans réseau (l'API réelle est de toute façon régulièrement en panne).
type mock42 struct {
	srv       *httptest.Server
	tokenHits atomic.Int32
	apiHits   atomic.Int32
}

func newMock42(t *testing.T) *mock42 {
	t.Helper()
	m := &mock42{}
	mux := http.NewServeMux()

	mux.HandleFunc("POST /oauth/token", func(w http.ResponseWriter, r *http.Request) {
		m.tokenHits.Add(1)
		if err := r.ParseForm(); err != nil || r.Form.Get("grant_type") != "refresh_token" {
			http.Error(w, "grant_type inattendu", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"access_token": "fresh-token", "refresh_token": "r2", "expires_in": 7200}`)
	})

	serve := func(pattern, body string) {
		mux.HandleFunc("GET "+pattern, func(w http.ResponseWriter, r *http.Request) {
			m.apiHits.Add(1)
			if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
				http.Error(w, "token manquant", http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, body)
		})
	}

	serve("/v2/me", mockMeJSON)
	serve("/v2/users/1/locations_stats", mockLocationsJSON())
	serve("/v2/users/1/coalitions", `[{"id": 377, "name": "Les Gordons", "color": "#00babc", "score": 4242, "image_url": ""}]`)
	serve("/v2/users/1/coalitions_users", `[{"id": 9001, "coalition_id": 377, "score": 512, "rank": 3}]`)
	serve("/v2/users/1/correction_point_historics", `[
	  {"sum": -1, "reason": "Defense plannified", "created_at": "2026-07-12T09:00:00.000Z"},
	  {"sum": 3, "reason": "Earning after defense", "created_at": "2026-07-10T15:00:00.000Z"},
	  {"sum": 2, "reason": "Correction point adjustment", "created_at": "2026-07-07T08:00:00.000Z"}
	]`)
	serve("/v2/users/1/scale_teams/as_corrected", `[
	  {"final_mark": 84, "comment": "Bon boulot, code propre.", "begin_at": "2026-07-10T10:00:00.000Z",
	   "filled_at": "2026-07-10T10:30:00.000Z", "corrector": {"login": "bob"},
	   "flag": {"name": "Ok", "positive": true}, "team": {"name": "alice's team"}},
	  {"final_mark": null, "comment": null, "begin_at": "2030-01-01T10:00:00.000Z", "filled_at": null,
	   "corrector": "invisible", "flag": {"name": "", "positive": false}, "team": {"name": "alice's team"}}
	]`)
	serve("/v2/users/1/scale_teams/as_corrector", `[
	  {"final_mark": 50, "comment": "Norme au poil, gestion d'erreurs à revoir.", "begin_at": "2026-07-09T14:00:00.000Z",
	   "filled_at": "2026-07-09T14:30:00.000Z", "corrector": {"login": "alice"},
	   "correcteds": [{"login": "carol"}], "flag": {"name": "Outstanding project", "positive": true},
	   "team": {"name": "carol's team"}}
	]`)
	serve("/v2/users/1/events", mockEventsJSON())

	m.srv = httptest.NewServer(mux)
	return m
}

const mockMeJSON = `{
  "id": 1, "login": "alice", "displayname": "Alice Liddell", "email": "alice@student.42.fr",
  "correction_point": 5, "wallet": 60, "pool_month": "july", "pool_year": "2026",
  "location": "e1r2p3", "created_at": "2026-07-01T08:00:00.000Z",
  "image": {"link": "https://cdn.intra.42.fr/users/alice.jpg", "versions": {"medium": "https://cdn.intra.42.fr/users/medium_alice.jpg"}},
  "campus": [{"id": 62, "name": "Perpignan", "country": "France"}],
  "campus_users": [{"campus_id": 62, "is_primary": true}],
  "cursus_users": [{
    "grade": null, "level": 4.42, "begin_at": "2026-07-06T07:00:00.000Z",
    "cursus": {"id": 9, "name": "C Piscine", "slug": "c-piscine"},
    "skills": [
      {"name": "Unix", "level": 3.14},
      {"name": "Rigor", "level": 4.5},
      {"name": "Algorithms & AI", "level": 2.21}
    ]
  }],
  "projects_users": [
    {"id": 11, "final_mark": 100, "status": "finished", "validated?": true, "cursus_ids": [9],
     "marked_at": "2026-07-08T18:00:00.000Z", "created_at": "2026-07-07T08:00:00.000Z",
     "project": {"name": "C 00", "slug": "c-piscine-c-00"}},
    {"id": 12, "final_mark": 84, "status": "finished", "validated?": true, "cursus_ids": [9],
     "marked_at": "2026-07-10T18:00:00.000Z", "created_at": "2026-07-08T08:00:00.000Z",
     "project": {"name": "C 01", "slug": "c-piscine-c-01"}},
    {"id": 13, "final_mark": 35, "status": "finished", "validated?": false, "cursus_ids": [9],
     "marked_at": "2026-07-11T18:00:00.000Z", "created_at": "2026-07-09T08:00:00.000Z",
     "project": {"name": "C 02", "slug": "c-piscine-c-02"}},
    {"id": 14, "final_mark": null, "status": "in_progress", "validated?": null, "cursus_ids": [9],
     "marked_at": null, "created_at": "2026-07-12T08:00:00.000Z",
     "project": {"name": "C 03", "slug": "c-piscine-c-03"}},
    {"id": 15, "final_mark": null, "status": "waiting_for_correction", "validated?": null, "cursus_ids": [9],
     "marked_at": null, "created_at": "2026-07-13T08:00:00.000Z",
     "project": {"name": "Exam 01", "slug": "c-piscine-exam-01"}}
  ],
  "achievements": [
    {"name": "Code Explorer", "description": "Valider son premier projet.", "tier": "easy", "kind": "project"},
    {"name": "Rigorous Basterd", "description": "Cinq validations d'affilée.", "tier": "medium", "kind": "project"},
    {"name": "All Star", "description": "Tout valider.", "tier": "challenge", "kind": "project"}
  ],
  "titles": [{"id": 11, "name": "%login the Pisciner"}],
  "titles_users": [{"title_id": 11, "selected": true}]
}`

// mockLocationsJSON génère un logtime relatif à aujourd'hui : hier 5h30,
// J-3 12h15 (dans les 7 jours), et J-20 2h00 (dans les 30 jours seulement).
func mockLocationsJSON() string {
	now := time.Now()
	day := func(offset int) string { return now.AddDate(0, 0, offset).Format("2006-01-02") }
	return fmt.Sprintf(`{"%s": "05:30:00.000000", "%s": "12:15:00.000000", "%s": "02:00:00.000000"}`,
		day(-1), day(-3), day(-20))
}

func mockEventsJSON() string {
	now := time.Now()
	return fmt.Sprintf(`[
	  {"name": "Meetup Piscine", "kind": "meet_up", "location": "Amphi", "begin_at": "%s", "end_at": "%s"},
	  {"name": "Vieille conf", "kind": "conference", "location": "Zoom", "begin_at": "%s", "end_at": "%s"}
	]`,
		now.Add(48*time.Hour).UTC().Format(time.RFC3339),
		now.Add(50*time.Hour).UTC().Format(time.RFC3339),
		now.Add(-72*time.Hour).UTC().Format(time.RFC3339),
		now.Add(-70*time.Hour).UTC().Format(time.RFC3339))
}

// --- Harnais ---

func newDashHandlers(t *testing.T, apiBase string) (*handlers, *http.ServeMux) {
	t.Helper()
	tmpl, err := loadTemplates()
	if err != nil {
		t.Fatalf("chargement des templates : %v", err)
	}
	ft := fortytwo.New(apiBase)
	ft.SetMinInterval(0)
	h := &handlers{
		tmpl:        tmpl,
		sessions:    auth.NewStore(),
		oauth:       auth.Config{ClientID: "cid", ClientSecret: "sec", RedirectURL: "http://localhost/auth/callback", BaseURL: apiBase},
		adminLogins: map[string]bool{},
		ft:          ft,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /dashboard", h.requireAuth(h.dashboardPage))
	mux.HandleFunc("GET /ui/dashboard/{card}", h.requireAuthFragment(h.dashboardCard))
	return h, mux
}

func loginAs(t *testing.T, h *handlers, tok auth.Token) *http.Cookie {
	t.Helper()
	rec := httptest.NewRecorder()
	if err := h.sessions.Create(rec, auth.User{ID: 1, Login: "alice"}, tok); err != nil {
		t.Fatalf("création de session : %v", err)
	}
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == "ft_moulinette_session" {
			return ck
		}
	}
	t.Fatal("cookie de session absent")
	return nil
}

func validToken() auth.Token {
	return auth.Token{AccessToken: "tok", RefreshToken: "r1", ExpiresAt: time.Now().Add(2 * time.Hour)}
}

func getWithCookie(mux *http.ServeMux, path string, ck *http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if ck != nil {
		req.AddCookie(ck)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// --- Tests des handlers ---

func TestDashboardPage(t *testing.T) {
	m := newMock42(t)
	defer m.srv.Close()
	h, mux := newDashHandlers(t, m.srv.URL)

	// Sans session : redirection vers l'écran d'accueil.
	rec := getWithCookie(mux, "/dashboard", nil)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/login" {
		t.Fatalf("anonyme : code=%d location=%q, attendu 302 /login", rec.Code, rec.Header().Get("Location"))
	}

	// Avec session : la coquille embarque les 9 cartes en chargement htmx.
	ck := loginAs(t, h, validToken())
	rec = getWithCookie(mux, "/dashboard", ck)
	if rec.Code != http.StatusOK {
		t.Fatalf("connecté : code=%d, attendu 200", rec.Code)
	}
	body := rec.Body.String()
	for card := range dashCards {
		if !strings.Contains(body, `hx-get="/ui/dashboard/`+card+`"`) {
			t.Errorf("coquille : carte %q absente", card)
		}
	}
}

func TestDashboardCards(t *testing.T) {
	m := newMock42(t)
	defer m.srv.Close()
	h, mux := newDashHandlers(t, m.srv.URL)
	ck := loginAs(t, h, validToken())

	cases := []struct {
		card string
		want []string
	}{
		{"hero", []string{"Alice Liddell", "4.42", "alice the Pisciner", "Piscine juillet 2026", "e1r2p3", "Perpignan · France"}},
		{"logtime", []string{"hm-cell l4", "17h45", "19h45"}}, // 7 j = 5h30+12h15 ; 30 j = +2h00
		{"projects", []string{"C 03", "C Piscine", "2 validés", "1 échoué", "donut"}},
		{"skills", []string{"Unix", "3.14", "Rigor"}},
		{"coalition", []string{"Les Gordons", "#3", "512", "4 242"}},
		{"evals", []string{"bob", "84", "anonyme", "planifiée", "carol", "Bon boulot"}},
		{"points", []string{"5 pts", "polyline", "Défense planifiée", "-1"}},
		{"achievements", []string{"All Star", "Challenge", "3 débloqués"}},
		{"events", []string{"Meetup Piscine", "Meetup", "Amphi", "2 inscriptions"}},
		// Sans service pool (tests), les cartes locales dégradent proprement.
		{"promo", []string{"Service de classement inactif"}},
		{"exam", []string{"Horaire des exams indisponible"}},
	}
	for _, tc := range cases {
		rec := getWithCookie(mux, "/ui/dashboard/"+tc.card, ck)
		if rec.Code != http.StatusOK {
			t.Errorf("carte %s : code=%d, attendu 200", tc.card, rec.Code)
			continue
		}
		body := rec.Body.String()
		if strings.Contains(body, "Réessayer") {
			t.Errorf("carte %s : rendue en erreur : %s", tc.card, body)
			continue
		}
		for _, want := range tc.want {
			if !strings.Contains(body, want) {
				t.Errorf("carte %s : %q absent du rendu", tc.card, want)
			}
		}
	}

	// La vieille conf passée ne doit pas apparaître dans les événements à venir.
	rec := getWithCookie(mux, "/ui/dashboard/events", ck)
	if strings.Contains(rec.Body.String(), "Vieille conf") {
		t.Errorf("events : un événement passé est affiché")
	}
}

func TestDashboardCardInconnue(t *testing.T) {
	m := newMock42(t)
	defer m.srv.Close()
	h, mux := newDashHandlers(t, m.srv.URL)
	ck := loginAs(t, h, validToken())

	if rec := getWithCookie(mux, "/ui/dashboard/nimporte", ck); rec.Code != http.StatusNotFound {
		t.Errorf("carte inconnue : code=%d, attendu 404", rec.Code)
	}
}

func TestDashboardFragmentSansSession(t *testing.T) {
	m := newMock42(t)
	defer m.srv.Close()
	_, mux := newDashHandlers(t, m.srv.URL)

	rec := getWithCookie(mux, "/ui/dashboard/hero", nil)
	if rec.Code != http.StatusOK || rec.Header().Get("HX-Redirect") != "/login" {
		t.Errorf("fragment anonyme : code=%d HX-Redirect=%q, attendu 200 + /login", rec.Code, rec.Header().Get("HX-Redirect"))
	}
	if rec.Body.Len() != 0 {
		t.Errorf("fragment anonyme : corps non vide (%d octets)", rec.Body.Len())
	}
}

// TestDashboardRefreshToken : un access token expiré est rafraîchi une seule
// fois, même quand plusieurs cartes s'enchaînent.
func TestDashboardRefreshToken(t *testing.T) {
	m := newMock42(t)
	defer m.srv.Close()
	h, mux := newDashHandlers(t, m.srv.URL)
	ck := loginAs(t, h, auth.Token{AccessToken: "vieux", RefreshToken: "r1", ExpiresAt: time.Now().Add(-time.Hour)})

	rec := getWithCookie(mux, "/ui/dashboard/hero", ck)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Alice Liddell") {
		t.Fatalf("hero après refresh : code=%d corps=%s", rec.Code, rec.Body.String())
	}
	if got := m.tokenHits.Load(); got != 1 {
		t.Fatalf("%d appels /oauth/token, attendu 1", got)
	}

	// Deuxième carte : le token rafraîchi est réutilisé tel quel.
	getWithCookie(mux, "/ui/dashboard/skills", ck)
	if got := m.tokenHits.Load(); got != 1 {
		t.Errorf("%d appels /oauth/token après une 2e carte, attendu toujours 1", got)
	}
}

// TestDashboardPanne42 : quand l'API 42 est en panne, chaque carte rend un
// panneau « Réessayer » en 200, et l'échec de /v2/me est partagé via le
// cache au lieu de re-marteler l'API.
func TestDashboardPanne42(t *testing.T) {
	var hits atomic.Int32
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Retry-After", "0.01")
		http.Error(w, "error code: 521", http.StatusBadGateway)
	}))
	defer down.Close()

	h, mux := newDashHandlers(t, down.URL)
	ck := loginAs(t, h, validToken())

	rec := getWithCookie(mux, "/ui/dashboard/hero", ck)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Réessayer") {
		t.Fatalf("carte en panne : code=%d, panneau d'erreur attendu", rec.Code)
	}
	after := hits.Load() // 2 tentatives sur /v2/me

	rec = getWithCookie(mux, "/ui/dashboard/points", ck)
	if !strings.Contains(rec.Body.String(), "Réessayer") {
		t.Fatalf("2e carte en panne : panneau d'erreur attendu")
	}
	if hits.Load() != after {
		t.Errorf("l'échec de /v2/me n'a pas été servi depuis le cache (%d appels de plus)", hits.Load()-after)
	}
}

// newScope403Mock sert un /v2/me valide et refuse tout le reste en 403,
// comme le fait l'API 42 quand le scope de l'app ne couvre pas un endpoint.
func newScope403Mock(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v2/me", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, mockMeJSON)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	})
	return httptest.NewServer(mux)
}

// TestDashboardScopeInterdit : un 403 de scope est définitif — le panneau
// prend la forme « cadenas », sans bouton Réessayer qui ne servirait à rien.
func TestDashboardScopeInterdit(t *testing.T) {
	srv := newScope403Mock(t)
	defer srv.Close()
	h, mux := newDashHandlers(t, srv.URL)
	ck := loginAs(t, h, validToken())

	rec := getWithCookie(mux, "/ui/dashboard/evals", ck)
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, `dash-error scope`) {
		t.Fatalf("evals en 403 : code=%d, panneau scope attendu : %s", rec.Code, body)
	}
	if strings.Contains(body, "Réessayer") {
		t.Errorf("evals en 403 : le bouton Réessayer ne doit pas apparaître")
	}
}

// TestDashboardPointsSansHistorique : l'historique en 403 ne condamne pas la
// carte — le solde (issu de /v2/me) reste affiché avec une note.
func TestDashboardPointsSansHistorique(t *testing.T) {
	srv := newScope403Mock(t)
	defer srv.Close()
	h, mux := newDashHandlers(t, srv.URL)
	ck := loginAs(t, h, validToken())

	rec := getWithCookie(mux, "/ui/dashboard/points", ck)
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, "5 pts") {
		t.Fatalf("points sans historique : code=%d, solde attendu : %s", rec.Code, body)
	}
	if !strings.Contains(body, "le solde, lui, est à jour") {
		t.Errorf("points sans historique : note de dégradation absente")
	}
	if strings.Contains(body, "Réessayer") {
		t.Errorf("points sans historique : la carte ne doit pas être en erreur")
	}
}

// --- Tests unitaires des helpers ---

func TestLogStreaks(t *testing.T) {
	today := time.Date(2026, 7, 14, 15, 0, 0, 0, time.Local)
	start := today.AddDate(0, 0, -10)
	day := func(offset int) string { return today.AddDate(0, 0, offset).Format("2006-01-02") }

	// Série de 3 (J-6..J-4), trou, série de 2 (J-2, J-1), aujourd'hui vide :
	// la série en cours tient (la journée n'est pas finie), la meilleure est 3.
	stats := map[string]string{
		day(-6): "02:00:00.0", day(-5): "03:00:00.0", day(-4): "01:00:00.0",
		day(-2): "05:00:00.0", day(-1): "04:00:00.0",
	}
	current, best := logStreaks(stats, start, today)
	if current != 2 || best != 3 {
		t.Errorf("current=%d best=%d, attendu 2 et 3", current, best)
	}

	// Aujourd'hui pointé : la série continue.
	stats[day(0)] = "01:00:00.0"
	current, best = logStreaks(stats, start, today)
	if current != 3 || best != 3 {
		t.Errorf("avec aujourd'hui : current=%d best=%d, attendu 3 et 3", current, best)
	}

	// Aucune activité.
	current, best = logStreaks(map[string]string{}, start, today)
	if current != 0 || best != 0 {
		t.Errorf("sans activité : current=%d best=%d, attendu 0 et 0", current, best)
	}
}

func TestHumanUntil(t *testing.T) {
	now := time.Now()
	cases := []struct {
		at   time.Time
		want string
	}{
		{now.Add(50*time.Hour + 30*time.Minute), "dans 2 j 02 h"},
		{now.Add(95 * time.Minute), "dans 1 h 35 min"},
		{now.Add(5*time.Minute + 10*time.Second), "dans 5 min"},
		{now.Add(-time.Second), "imminent"},
	}
	for _, c := range cases {
		if got := humanUntil(c.at); got != c.want {
			t.Errorf("humanUntil(+%v) = %q, attendu %q", time.Until(c.at).Round(time.Minute), got, c.want)
		}
	}
}

func TestParseLogHours(t *testing.T) {
	cases := []struct {
		in   string
		want float64
	}{
		{"05:30:00.000000", 5.5},
		{"12:15:00.0", 12.25},
		{"", 0},
		{"n'importe quoi", 0},
	}
	for _, c := range cases {
		if got := parseLogHours(c.in); got < c.want-0.001 || got > c.want+0.001 {
			t.Errorf("parseLogHours(%q) = %v, attendu %v", c.in, got, c.want)
		}
	}
}

func TestFmtHours(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{0, "0h"},
		{5.5, "5h30"},
		{17.75, "17h45"},
		{26.999, "27h00"},
	}
	for _, c := range cases {
		if got := fmtHours(c.in); got != c.want {
			t.Errorf("fmtHours(%v) = %q, attendu %q", c.in, got, c.want)
		}
	}
}

func TestFmtInt(t *testing.T) {
	cases := []struct {
		in   int
		want string
	}{
		{512, "512"},
		{4242, "4 242"},
		{1234567, "1 234 567"},
		{-4242, "-4 242"},
	}
	for _, c := range cases {
		if got := fmtInt(c.in); got != c.want {
			t.Errorf("fmtInt(%d) = %q, attendu %q", c.in, got, c.want)
		}
	}
}

func TestDonutSegments(t *testing.T) {
	segs := donutSegments([]int{3, 1, 2}, []string{"ok", "ko", "wip"})
	if len(segs) != 3 {
		t.Fatalf("%d segments, attendu 3", len(segs))
	}
	if segs[0].Dash != "50.00 50.00" || segs[0].Offset != "0.00" {
		t.Errorf("segment 1 : dash=%q offset=%q", segs[0].Dash, segs[0].Offset)
	}
	if segs[1].Offset != "-50.00" {
		t.Errorf("segment 2 : offset=%q, attendu -50.00", segs[1].Offset)
	}

	if segs := donutSegments([]int{2, 0, 0}, []string{"ok", "ko", "wip"}); len(segs) != 1 {
		t.Errorf("les effectifs nuls doivent être omis (%d segments)", len(segs))
	}
	if segs := donutSegments([]int{0, 0, 0}, []string{"ok", "ko", "wip"}); segs != nil {
		t.Errorf("aucun projet : attendu nil, obtenu %v", segs)
	}
}

func TestSparkline(t *testing.T) {
	if s := sparkline([]int{5}); s != "" {
		t.Errorf("série trop courte : attendu vide, obtenu %q", s)
	}
	s := sparkline([]int{3, 6, 5})
	if len(strings.Fields(s)) != 3 {
		t.Errorf("3 valeurs doivent donner 3 points : %q", s)
	}
	// Série plate : ne doit pas diviser par zéro.
	if s := sparkline([]int{4, 4, 4}); s == "" {
		t.Errorf("série plate : polyline attendue")
	}
}

func TestBuildHeatmap(t *testing.T) {
	// Mardi 14 juillet 2026 : la période démarre le lundi 27 avril.
	today := time.Date(2026, 7, 14, 15, 0, 0, 0, time.Local)
	start := time.Date(2026, 4, 27, 0, 0, 0, 0, time.Local)
	stats := map[string]string{
		"2026-07-13": "08:00:00.000000", // lundi de la dernière colonne
		"2026-05-04": "01:30:00.000000",
	}

	weeks, months := buildHeatmap(stats, start, today)
	if len(weeks) != 12 || len(months) != 12 {
		t.Fatalf("%d colonnes / %d libellés, attendu 12/12", len(weeks), len(months))
	}
	for i, col := range weeks {
		if len(col) != 7 {
			t.Fatalf("colonne %d : %d cases, attendu 7", i, len(col))
		}
	}
	last := weeks[11]
	if last[0].Class != "l4" {
		t.Errorf("lundi 13 juil (8h) : classe %s, attendu l4", last[0].Class)
	}
	if last[1].Class == "lx" || last[2].Class != "lx" {
		t.Errorf("mardi doit être visible, mercredi (futur) masqué : %s / %s", last[1].Class, last[2].Class)
	}
	if weeks[1][0].Class != "l1" {
		t.Errorf("4 mai (1h30) : classe %s, attendu l1", weeks[1][0].Class)
	}
	if months[0] == "" {
		t.Errorf("la première colonne doit porter son libellé de mois")
	}
}

func TestFrDateShort(t *testing.T) {
	d := time.Date(2026, 7, 14, 10, 0, 0, 0, time.Local)
	if got := frDateShort(d); got != "mar. 14 juil." {
		t.Errorf("frDateShort = %q, attendu %q", got, "mar. 14 juil.")
	}
}
