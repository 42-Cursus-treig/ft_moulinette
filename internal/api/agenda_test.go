package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tristan-reig/ft-moulinette/internal/fortytwo"
	"github.com/tristan-reig/ft-moulinette/internal/pool"
)

func postJSON(mux *http.ServeMux, path, body string, ck *http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if ck != nil {
		req.AddCookie(ck)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestAgendaPage(t *testing.T) {
	m := newMock42(t)
	defer m.srv.Close()
	h, mux := newDashHandlers(t, m.srv.URL)

	if rec := getWithCookie(mux, "/agenda", nil); rec.Code != http.StatusFound {
		t.Fatalf("anonyme : code=%d, attendu 302", rec.Code)
	}

	ck := loginAs(t, h, validToken())
	rec := getWithCookie(mux, "/agenda", ck)
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, `id="agenda-root"`) || !strings.Contains(body, `hx-get="/ui/slots"`) {
		t.Fatalf("page agenda : code=%d, coquille attendue", rec.Code)
	}
	if !strings.Contains(body, "/static/agenda.js") {
		t.Errorf("page agenda : script du glisser-déposer absent")
	}
}

func TestAgendaCalendar(t *testing.T) {
	m := newMock42(t)
	defer m.srv.Close()
	h, mux := newDashHandlers(t, m.srv.URL)
	ck := loginAs(t, h, validToken())

	// Semaine du 4 janvier 2027 : les créneaux du mock (mercredi 6) y tombent
	// et sont dans le futur (jamais masqués par le filtre des créneaux passés).
	rec := getWithCookie(mux, "/ui/slots?week=2027-01-04", ck)
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("calendrier : code=%d", rec.Code)
	}
	// Les deux granules libres contiguës sont fusionnées en un bloc (ids 201,202).
	if !strings.Contains(body, "15:00 – 15:30") || !strings.Contains(body, `data-ids="201,202"`) {
		t.Errorf("bloc libre fusionné 15:00–15:30 (ids 201,202) absent : %s", body)
	}
	// Réservation pas encore révélée, puis réservation révélée avec lien profil.
	if !strings.Contains(body, "révélé ~15 min avant") {
		t.Errorf("bloc réservé invisible absent")
	}
	if !strings.Contains(body, "profile.intra.42.fr/users/bob") {
		t.Errorf("lien vers le profil du corrigé absent")
	}
	if !strings.Contains(body, "ag-block booked") {
		t.Errorf("classe des blocs réservés absente")
	}
	if !strings.Contains(body, `data-week="2027-01-04"`) {
		t.Errorf("data-week=2027-01-04 absent")
	}
	// Bornes du glisser-déposer exposées au JS (fin de grille + min par colonne).
	if !strings.Contains(body, `data-end="1440"`) || !strings.Contains(body, `data-min=`) {
		t.Errorf("bornes data-end/data-min absentes du calendrier")
	}

	// Sans argument, la semaine par défaut est celle d'aujourd'hui, calée au lundi.
	def := getWithCookie(mux, "/ui/slots", ck).Body.String()
	if !strings.Contains(def, `data-week="`+parseWeek("").Format("2006-01-02")+`"`) {
		t.Errorf("semaine par défaut absente")
	}
}

// TestAgendaSync : le lot local (suppressions + créations) est appliqué chez
// 42 — suppressions d'abord, créations ensuite — et résumé en JSON.
func TestAgendaSync(t *testing.T) {
	m := newMock42(t)
	defer m.srv.Close()
	h, mux := newDashHandlers(t, m.srv.URL)
	ck := loginAs(t, h, validToken())

	tomorrow := time.Now().AddDate(0, 0, 1)
	day := tomorrow.Format("2006-01-02")
	body := fmt.Sprintf(`{"week":"","delete":[201,202],"create":[
	  {"day":%q,"start":600,"end":660},
	  {"day":%q,"start":720,"end":750}
	]}`, day, day)
	rec := postJSON(mux, "/ui/slots/sync", body, ck)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"saved":2`) {
		t.Fatalf("sync : code=%d corps=%s", rec.Code, rec.Body.String())
	}
	if m.slotDeletes.Load() != 2 || m.slotCreates.Load() != 2 {
		t.Errorf("%d DELETE / %d POST, attendu 2/2", m.slotDeletes.Load(), m.slotCreates.Load())
	}
	// L'heure locale est convertie en UTC (dernier POST : 12:00).
	wantBegin := time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 12, 0, 0, 0, time.Local).UTC()
	got, err := time.Parse(time.RFC3339, m.lastSlotBegin.Load().(string))
	if err != nil || !got.Equal(wantBegin) {
		t.Errorf("begin_at envoyé = %v (%v), attendu %v", m.lastSlotBegin.Load(), err, wantBegin)
	}

	// Créneau d'un jour révolu : signalé en échec, aucun POST supplémentaire.
	past := fmt.Sprintf(`{"delete":[],"create":[{"day":%q,"start":600,"end":660}]}`,
		time.Now().AddDate(0, 0, -1).Format("2006-01-02"))
	rec = postJSON(mux, "/ui/slots/sync", past, ck)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "est déjà passé") || m.slotCreates.Load() != 2 {
		t.Errorf("jour passé : échec signalé attendu sans POST (code=%d corps=%s)", rec.Code, rec.Body.String())
	}

	// Lot vide ou bornes non alignées : 400 / échec signalé.
	if rec := postJSON(mux, "/ui/slots/sync", `{"delete":[],"create":[]}`, ck); rec.Code != http.StatusBadRequest {
		t.Errorf("lot vide : 400 attendu (code=%d)", rec.Code)
	}
	bad := fmt.Sprintf(`{"create":[{"day":%q,"start":601,"end":660}]}`, day)
	if rec := postJSON(mux, "/ui/slots/sync", bad, ck); !strings.Contains(rec.Body.String(), "invalide") || m.slotCreates.Load() != 2 {
		t.Errorf("bornes non alignées : créneau ignoré attendu")
	}
}

// TestAgendaSyncDecale : 42 refuse la place demandée (déjà prise côté
// serveur) — le créneau est décalé de 15 min et le décalage est signalé.
func TestAgendaSyncDecale(t *testing.T) {
	var creates atomic.Int32
	mux42 := http.NewServeMux()
	mux42.HandleFunc("POST /v2/slots", func(w http.ResponseWriter, r *http.Request) {
		if creates.Add(1) == 1 {
			http.Error(w, `{"error":"overlapping slot"}`, http.StatusUnprocessableEntity)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `[{"id": 401}]`)
	})
	srv := httptest.NewServer(mux42)
	defer srv.Close()

	h, mux := newDashHandlers(t, srv.URL)
	ck := loginAs(t, h, validToken())

	day := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	body := fmt.Sprintf(`{"create":[{"day":%q,"start":600,"end":660}]}`, day)
	rec := postJSON(mux, "/ui/slots/sync", body, ck)
	out := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(out, `"saved":1`) {
		t.Fatalf("sync décalé : code=%d corps=%s", rec.Code, out)
	}
	if !strings.Contains(out, "décalé à 10:15") {
		t.Errorf("décalage non signalé : %s", out)
	}
	if creates.Load() != 2 {
		t.Errorf("%d POST, attendu 2 (refus puis décalage)", creates.Load())
	}
}

// TestAgendaSyncSuppressionRefusee : une granule verrouillée par l'intra
// (403) n'arrête pas le lot — elle est signalée et le reste s'applique.
func TestAgendaSyncSuppressionRefusee(t *testing.T) {
	mux42 := http.NewServeMux()
	mux42.HandleFunc("DELETE /v2/slots/{id}", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"Access Denied"}`, http.StatusForbidden)
	})
	mux42.HandleFunc("POST /v2/slots", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `[{"id": 401}]`)
	})
	srv := httptest.NewServer(mux42)
	defer srv.Close()

	h, mux := newDashHandlers(t, srv.URL)
	ck := loginAs(t, h, validToken())

	day := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	body := fmt.Sprintf(`{"delete":[136150988],"create":[{"day":%q,"start":600,"end":660}]}`, day)
	rec := postJSON(mux, "/ui/slots/sync", body, ck)
	out := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(out, `"saved":1`) {
		t.Fatalf("sync : code=%d corps=%s", rec.Code, out)
	}
	if !strings.Contains(out, "suppression(s) refusée(s)") {
		t.Errorf("le refus de suppression doit être signalé : %s", out)
	}
}

// TestAgendaSyncScope : 403 sur la création (scope « projects » manquant) —
// tout le lot échoue avec un message qui pointe le vrai remède.
func TestAgendaSyncScope(t *testing.T) {
	mux42 := http.NewServeMux()
	mux42.HandleFunc("POST /v2/slots", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"Forbidden"}`, http.StatusForbidden)
	})
	srv := httptest.NewServer(mux42)
	defer srv.Close()

	h, mux := newDashHandlers(t, srv.URL)
	ck := loginAs(t, h, validToken())

	day := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	body := fmt.Sprintf(`{"create":[{"day":%q,"start":600,"end":660}]}`, day)
	rec := postJSON(mux, "/ui/slots/sync", body, ck)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "projects") {
		t.Errorf("scope manquant : 403 + message scope attendus (code=%d corps=%s)", rec.Code, rec.Body.String())
	}
}

// TestAgendaSyncIdsPerimes : des granules qui n'existent plus côté 42 (ids
// périmés, suppression en cascade) ne bloquent pas le lot — le DELETE 404 est
// traité comme « déjà supprimé » et les créations s'appliquent.
func TestAgendaSyncIdsPerimes(t *testing.T) {
	mux42 := http.NewServeMux()
	mux42.HandleFunc("DELETE /v2/slots/{id}", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"Not Found"}`, http.StatusNotFound)
	})
	mux42.HandleFunc("POST /v2/slots", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `[{"id": 401}]`)
	})
	srv := httptest.NewServer(mux42)
	defer srv.Close()

	h, mux := newDashHandlers(t, srv.URL)
	ck := loginAs(t, h, validToken())

	day := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	body := fmt.Sprintf(`{"delete":[999,998],"create":[{"day":%q,"start":630,"end":690}]}`, day)
	rec := postJSON(mux, "/ui/slots/sync", body, ck)
	out := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(out, `"saved":1`) {
		t.Fatalf("ids périmés : code=%d corps=%s, attendu 200 + saved 1", rec.Code, out)
	}
	if strings.Contains(out, "refusée") {
		t.Errorf("un 404 (déjà supprimé) ne doit pas être signalé comme un refus : %s", out)
	}
}

// TestUserCard : la fiche publique d'un pisciner, avec validation du login.
func TestUserCard(t *testing.T) {
	m := newMock42(t)
	defer m.srv.Close()
	h, mux := newDashHandlers(t, m.srv.URL)
	ck := loginAs(t, h, validToken())

	rec := getWithCookie(mux, "/ui/user?login=carol", ck)
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, "Carol Danvers") {
		t.Fatalf("fiche carol : code=%d corps=%s", rec.Code, body)
	}
	for _, want := range []string{"5.21", "profile.intra.42.fr/users/carol", "Projets validés"} {
		if !strings.Contains(body, want) {
			t.Errorf("fiche carol : %q absent", want)
		}
	}

	if rec := getWithCookie(mux, "/ui/user?login=Carol%21", ck); !strings.Contains(rec.Body.String(), "Login invalide") {
		t.Errorf("login invalide : refus attendu")
	}
	if rec := getWithCookie(mux, "/ui/user?login=zzinconnu", ck); !strings.Contains(rec.Body.String(), "Aucun compte") {
		t.Errorf("login inconnu : message « aucun compte » attendu, obtenu %s", rec.Body.String())
	}
}

// TestAgendaICS : le calendrier .ics est servi depuis l'instantané local
// (zéro appel API), protégé par la signature HMAC de l'URL.
func TestAgendaICS(t *testing.T) {
	m := newMock42(t)
	defer m.srv.Close()
	h, mux := newDashHandlers(t, m.srv.URL)
	ck := loginAs(t, h, validToken())

	// Un rendu du calendrier et de la carte défenses remplit l'instantané.
	getWithCookie(mux, "/ui/slots?week=2027-01-04", ck)
	getWithCookie(mux, "/ui/dashboard/defenses", ck)

	rec := getWithCookie(mux, h.icsURL("alice"), nil) // pas de session : la signature suffit
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, "BEGIN:VCALENDAR") {
		t.Fatalf("ics : code=%d corps=%s", rec.Code, body)
	}
	for _, want := range []string{"Dispo correction", "Correction réservée", "Je corrige carol", "C Piscine Rush 00"} {
		if !strings.Contains(body, want) {
			t.Errorf("ics : %q absent :\n%s", want, body)
		}
	}
	// Les deux granules libres contiguës donnent UN seul VEVENT.
	if strings.Count(body, "SUMMARY:Dispo correction") != 1 {
		t.Errorf("ics : granules libres non fusionnées")
	}

	if rec := getWithCookie(mux, "/agenda.ics?login=alice&k=mauvaise", nil); rec.Code != http.StatusNotFound {
		t.Errorf("mauvaise signature : 404 attendu (code=%d)", rec.Code)
	}
}

// TestSlotsCopyWeek : la recopie renvoie les dispos libres de la semaine
// précédente projetées sur la semaine cible, réservations exclues.
func TestSlotsCopyWeek(t *testing.T) {
	m := newMock42(t)
	defer m.srv.Close()
	h, mux := newDashHandlers(t, m.srv.URL)
	ck := loginAs(t, h, validToken())

	// Les slots du mock sont le mercredi 6 janvier 2027 : on demande la
	// semaine suivante (lundi 11), la projection tombe le mercredi 13.
	rec := getWithCookie(mux, "/ui/slots/copy?week=2027-01-11", ck)
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("copie : code=%d corps=%s", rec.Code, body)
	}
	if !strings.Contains(body, `"day":"2027-01-13"`) || !strings.Contains(body, `"start":900`) || !strings.Contains(body, `"end":930`) {
		t.Errorf("plage libre 15:00–15:30 projetée attendue : %s", body)
	}
	if strings.Count(body, `"day"`) != 1 {
		t.Errorf("les créneaux réservés ne doivent pas être recopiés : %s", body)
	}
}

func TestBuildExamBanner(t *testing.T) {
	now := time.Now()
	mk := func(beginOff, endOff time.Duration) []pool.ExamWindowInfo {
		return []pool.ExamWindowInfo{{Key: "00", Begin: now.Add(beginOff), End: now.Add(endOff)}}
	}
	if b := buildExamBanner(mk(-time.Hour, time.Hour), now); b == nil || !b.Active || !strings.Contains(b.Countdown, "se termine") {
		t.Errorf("exam en cours : bannière active attendue, obtenu %+v", b)
	}
	if b := buildExamBanner(mk(30*time.Minute, 4*time.Hour), now); b == nil || b.Active || !strings.Contains(b.Countdown, "commence") {
		t.Errorf("exam dans 30 min : bannière « commence » attendue, obtenu %+v", b)
	}
	if b := buildExamBanner(mk(3*time.Hour, 7*time.Hour), now); b != nil {
		t.Errorf("exam dans 3 h : pas de bannière attendue, obtenu %+v", b)
	}
	if b := buildExamBanner(mk(-5*time.Hour, -time.Hour), now); b != nil {
		t.Errorf("exam fini : pas de bannière attendue, obtenu %+v", b)
	}
	if b := buildExamBanner(nil, now); b != nil {
		t.Errorf("aucune fenêtre : pas de bannière attendue")
	}
}

func TestParseWeek(t *testing.T) {
	// Le 15 juillet 2026 est un mercredi : la semaine démarre le lundi 13.
	if got := parseWeek("2026-07-15").Format("2006-01-02"); got != "2026-07-13" {
		t.Errorf("parseWeek(2026-07-15) = %s, attendu 2026-07-13", got)
	}
	monday := parseWeek("")
	if monday.Weekday() != time.Monday || monday.After(time.Now()) {
		t.Errorf("parseWeek(\"\") = %v : lundi passé attendu", monday)
	}
}

func TestBuildAgendaGreys(t *testing.T) {
	week := time.Date(2026, 7, 13, 0, 0, 0, 0, time.Local) // lundi
	now := time.Date(2026, 7, 15, 11, 44, 0, 0, time.Local) // mercredi 11:44
	v := buildAgenda(nil, week, now)

	if len(v.Days) != 7 {
		t.Fatalf("%d jours, attendu 7", len(v.Days))
	}
	// Lundi et mardi sont révolus : colonne entièrement grisée, rien de posable.
	for _, i := range []int{0, 1} {
		d := v.Days[i]
		if !d.Past || d.PastPx != v.GridHeight || d.MinMin != agDayEndMin {
			t.Errorf("jour %d (passé) : Past=%v PastPx=%d MinMin=%d", i, d.Past, d.PastPx, d.MinMin)
		}
	}
	// Mercredi = aujourd'hui : posable à partir du prochain quart (11:45 = 705),
	// grisé jusque-là, ligne « maintenant » positionnée.
	today := v.Days[2]
	if !today.Today || today.Past || today.MinMin != 705 || today.PastPx <= 0 {
		t.Errorf("aujourd'hui : Today=%v Past=%v MinMin=%d PastPx=%d", today.Today, today.Past, today.MinMin, today.PastPx)
	}
	if v.NowTop <= 0 {
		t.Errorf("ligne maintenant : NowTop=%d, attendu > 0", v.NowTop)
	}
	// Jeudi et après : tout est posable, rien de grisé.
	for _, i := range []int{3, 4, 5, 6} {
		d := v.Days[i]
		if d.Past || d.PastPx != 0 || d.MinMin != agDayStartMin {
			t.Errorf("jour %d (futur) : Past=%v PastPx=%d MinMin=%d", i, d.Past, d.PastPx, d.MinMin)
		}
	}
}

func TestGroupSlots(t *testing.T) {
	day := time.Date(2026, 7, 15, 0, 0, 0, 0, time.Local)
	granule := func(id, h, m int, booked bool, bookingID int) fortytwo.Slot {
		s := fortytwo.Slot{
			ID:      id,
			BeginAt: day.Add(time.Duration(h*60+m) * time.Minute),
			EndAt:   day.Add(time.Duration(h*60+m+15) * time.Minute),
		}
		if booked {
			s.ScaleTeam = fortytwo.SlotBooking{Present: true, ID: bookingID}
		}
		return s
	}

	blocks := groupSlots([]fortytwo.Slot{
		granule(1, 15, 0, false, 0),
		granule(2, 15, 15, false, 0),
		granule(3, 15, 30, false, 0),
		granule(4, 16, 0, true, 9), // trou de 15 min : nouveau bloc
		granule(5, 16, 15, true, 9),
		granule(6, 16, 30, true, 10), // autre défense : bloc séparé
	})
	if len(blocks) != 3 {
		t.Fatalf("%d blocs, attendu 3", len(blocks))
	}
	free := blocks[0]
	if free.IDs != "1,2,3" || free.Label != "15:00 – 15:45" || free.Booked {
		t.Errorf("bloc libre : %+v", free)
	}
	// Géométrie : 15:00 → (900-360)/15×11 = 396 px, 45 min → 33-2 px.
	if free.Top != 396 || free.Height != 31 {
		t.Errorf("géométrie du bloc libre : top=%d height=%d, attendu 396/31", free.Top, free.Height)
	}
	if !blocks[1].Booked || blocks[1].IDs != "4,5" || !blocks[2].Booked || blocks[2].IDs != "6" {
		t.Errorf("blocs réservés mal fusionnés : %+v / %+v", blocks[1], blocks[2])
	}
}

func TestDaysApart(t *testing.T) {
	base := time.Date(2026, 7, 15, 3, 0, 0, 0, time.Local)
	cases := []struct {
		b    time.Time
		want int
	}{
		{base, 0},
		{base.Add(10 * time.Hour), 0},                              // même jour calendaire
		{time.Date(2026, 7, 16, 1, 0, 0, 0, time.Local), 1},        // lendemain, < 24 h d'écart
		{base.AddDate(0, 0, -1), -1},
		{base.AddDate(0, 0, 6), 6},
	}
	for _, c := range cases {
		if got := daysApart(base, c.b); got != c.want {
			t.Errorf("daysApart(%v, %v) = %d, attendu %d", base.Format("02/01 15h"), c.b.Format("02/01 15h"), got, c.want)
		}
	}
	if got := daysApart(base.AddDate(0, 0, 3), base); got != -3 {
		t.Errorf("daysApart inversé = %d, attendu -3", got)
	}
}
