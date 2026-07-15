package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/tristan-reig/ft-moulinette/internal/fortytwo"
)

func postWithCookie(mux *http.ServeMux, path string, form url.Values, ck *http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
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

	rec := getWithCookie(mux, "/ui/slots", ck)
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("calendrier : code=%d", rec.Code)
	}
	// Les deux granules libres contiguës sont fusionnées en un bloc, un seul ✕.
	if !strings.Contains(body, "15:00 – 15:30") || !strings.Contains(body, `"201,202"`) {
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
	// La semaine affichée par défaut est celle d'aujourd'hui, calée au lundi.
	monday := parseWeek("").Format("2006-01-02")
	if !strings.Contains(body, `data-week="`+monday+`"`) {
		t.Errorf("data-week=%s absent", monday)
	}
	// Bornes du glisser-déposer exposées au JS (fin de grille + min par colonne).
	if !strings.Contains(body, `data-end="1440"`) || !strings.Contains(body, `data-min=`) {
		t.Errorf("bornes data-end/data-min absentes du calendrier")
	}
}

func TestAgendaCreate(t *testing.T) {
	m := newMock42(t)
	defer m.srv.Close()
	h, mux := newDashHandlers(t, m.srv.URL)
	ck := loginAs(t, h, validToken())

	// Un premier rendu remplit le cache des slots : la création doit
	// l'invalider, sinon le calendrier re-rendu ne montrerait pas le créneau.
	getWithCookie(mux, "/ui/slots", ck)
	if m.slotHits.Load() != 1 {
		t.Fatalf("%d GET initiaux, attendu 1", m.slotHits.Load())
	}

	tomorrow := time.Now().AddDate(0, 0, 1)
	form := url.Values{
		"day":   {tomorrow.Format("2006-01-02")},
		"start": {"600"}, // 10:00
		"end":   {"660"}, // 11:00
		"week":  {""},
	}
	rec := postWithCookie(mux, "/ui/slots", form, ck)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "ag-error") {
		t.Fatalf("création : code=%d corps=%s", rec.Code, rec.Body.String())
	}
	if m.slotCreates.Load() != 1 {
		t.Fatalf("%d POST /v2/slots, attendu 1", m.slotCreates.Load())
	}
	// L'heure locale 10:00 est convertie en UTC pour l'API 42.
	wantBegin := time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 10, 0, 0, 0, time.Local).UTC()
	got, err := time.Parse(time.RFC3339, m.lastSlotBegin.Load().(string))
	if err != nil || !got.Equal(wantBegin) {
		t.Errorf("begin_at envoyé = %v (%v), attendu %v", m.lastSlotBegin.Load(), err, wantBegin)
	}
	// Le cache des slots a été invalidé : le re-rendu refait un GET.
	if m.slotHits.Load() != 2 {
		t.Errorf("le calendrier n'a pas été relu après la création (%d GET, attendu 2)", m.slotHits.Load())
	}

	// Un créneau dans le passé est refusé sans appel à l'API.
	past := url.Values{"day": {time.Now().AddDate(0, 0, -1).Format("2006-01-02")}, "start": {"600"}, "end": {"660"}}
	rec = postWithCookie(mux, "/ui/slots", past, ck)
	if !strings.Contains(rec.Body.String(), "dans le passé") || m.slotCreates.Load() != 1 {
		t.Errorf("créneau passé : refus attendu sans POST supplémentaire")
	}

	// Bornes hors grille ou pas alignées sur 15 min : refus.
	bad := url.Values{"day": {tomorrow.Format("2006-01-02")}, "start": {"601"}, "end": {"660"}}
	rec = postWithCookie(mux, "/ui/slots", bad, ck)
	if !strings.Contains(rec.Body.String(), "Créneau invalide") || m.slotCreates.Load() != 1 {
		t.Errorf("créneau non aligné : refus attendu")
	}
}

func TestAgendaDelete(t *testing.T) {
	m := newMock42(t)
	defer m.srv.Close()
	h, mux := newDashHandlers(t, m.srv.URL)
	ck := loginAs(t, h, validToken())

	rec := postWithCookie(mux, "/ui/slots/delete", url.Values{"ids": {"201,202"}, "week": {""}}, ck)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "ag-error") {
		t.Fatalf("suppression : code=%d corps=%s", rec.Code, rec.Body.String())
	}
	if m.slotDeletes.Load() != 2 {
		t.Errorf("%d DELETE, attendu 2 (une granule par id)", m.slotDeletes.Load())
	}

	rec = postWithCookie(mux, "/ui/slots/delete", url.Values{"ids": {"zut"}}, ck)
	if !strings.Contains(rec.Body.String(), "Créneau invalide") || m.slotDeletes.Load() != 2 {
		t.Errorf("ids invalides : refus attendu sans DELETE supplémentaire")
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
	week := time.Date(2026, 7, 13, 0, 0, 0, 0, time.Local)  // lundi
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
		{base.Add(10 * time.Hour), 0}, // même jour calendaire
		{time.Date(2026, 7, 16, 1, 0, 0, 0, time.Local), 1}, // lendemain, < 24 h d'écart
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
