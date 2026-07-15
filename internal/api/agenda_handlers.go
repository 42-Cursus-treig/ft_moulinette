package api

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tristan-reig/ft-moulinette/internal/auth"
	"github.com/tristan-reig/ft-moulinette/internal/fortytwo"
)

// --- Carte dashboard « Corrections à venir » ---

type dashDefenseRow struct {
	Direction string // « Je corrige » | « On me corrige »
	Give      bool   // true = je corrige
	Project   string
	Who       string // login de l'autre partie, "" tant que l'intra le cache
	WhoURL    string
	When      string
	Countdown string
	Soon      bool // < 20 min : c'est imminent
}

type dashDefensesView struct {
	Rows []dashDefenseRow
	Note string
}

// dashDefenses reconstruit le « Pending evaluations » de l'intra : les
// défenses à venir où l'utilisateur corrige (/v2/me/scale_teams) et où il est
// corrigé. L'identité de l'autre partie n'est révélée par l'API qu'environ
// 15 minutes avant la défense : la carte se rafraîchit toutes les 60 s et le
// login devient un lien vers le profil intra dès qu'il apparaît.
func (h *handlers) dashDefenses(ctx context.Context, user auth.User, tok string) (any, error) {
	me, err := h.ft.Me(ctx, user.Login, tok)
	if err != nil {
		return nil, err
	}
	giving, err := h.ft.MeScaleTeams(ctx, user.Login, tok)
	if err != nil {
		return nil, err
	}
	receiving, errR := h.ft.ScaleTeams(ctx, user.Login, tok, me.ID, "as_corrected")

	v := dashDefensesView{}
	if errR != nil {
		receiving = nil
		v.Note = "Les défenses où tu es corrigé n'ont pas pu être chargées."
	}

	now := time.Now()
	seen := map[int]bool{}
	type pending struct {
		st   fortytwo.ScaleTeam
		give bool
	}
	var items []pending
	for _, st := range giving {
		// Garde aussi la défense qui vient de commencer (elle est « en cours »).
		if st.BeginAt.Before(now.Add(-time.Hour)) || st.FilledAt != nil || seen[st.ID] {
			continue
		}
		seen[st.ID] = true
		items = append(items, pending{st, true})
	}
	for _, st := range receiving {
		if st.BeginAt.Before(now.Add(-time.Hour)) || st.FilledAt != nil || seen[st.ID] {
			continue
		}
		seen[st.ID] = true
		items = append(items, pending{st, false})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].st.BeginAt.Before(items[j].st.BeginAt) })

	for i, it := range items {
		if i == 6 {
			break
		}
		st := it.st
		row := dashDefenseRow{
			Give:      it.give,
			Direction: "On me corrige",
			Project:   defenseProject(ctx, h, user.Login, tok, st),
			When:      frDateTimeShort(st.BeginAt),
			Soon:      time.Until(st.BeginAt) < 20*time.Minute,
		}
		if it.give {
			row.Direction = "Je corrige"
			var names []string
			for _, cu := range st.Correcteds {
				if cu.Login != "" {
					names = append(names, cu.Login)
				}
			}
			row.Who = strings.Join(names, ", ")
		} else {
			row.Who = st.Corrector.Login
		}
		if row.Who != "" {
			row.WhoURL = "https://profile.intra.42.fr/users/" + url.PathEscape(strings.Fields(row.Who)[0])
		}
		if st.BeginAt.After(now) {
			row.Countdown = humanUntil(st.BeginAt)
		} else {
			row.Countdown = "en ce moment"
			row.Soon = true
		}
		v.Rows = append(v.Rows, row)
	}
	return v, nil
}

// defenseProject libelle la défense avec le vrai nom du projet quand l'API le
// permet (cache 24 h par projet), sinon retombe sur le nom d'équipe.
func defenseProject(ctx context.Context, h *handlers, login, tok string, st fortytwo.ScaleTeam) string {
	if st.Team.ProjectID != 0 {
		if name, err := h.ft.Project(ctx, login, tok, st.Team.ProjectID); err == nil && name != "" {
			return name
		}
	}
	if st.Team.Name != "" {
		return st.Team.Name
	}
	return "Défense"
}

// --- Page agenda : créneaux de correction ---

const (
	agDayStartMin = 6 * 60  // la grille démarre à 06:00…
	agDayEndMin   = 24 * 60 // …et finit à minuit
	agPxPer15     = 11      // hauteur d'une case de 15 min, alignée sur le CSS
)

// agendaPage (GET /agenda) rend la coquille ; le calendrier arrive en htmx.
func (h *handlers) agendaPage(w http.ResponseWriter, r *http.Request) {
	user, _ := userFromContext(r.Context())
	data := h.navFlags(user)
	data["User"] = user
	data["Page"] = "agenda"
	if err := h.tmpl.ExecuteTemplate(w, "agenda", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

type agBlock struct {
	Top, Height int    // géométrie en px dans la colonne
	Label       string // « 15:00 – 16:30 »
	IDs         string // ids des slots 42 regroupés, "12,13,14"
	Booked      bool
	Who         string // login du corrigé si révélé
	WhoURL      string
	Countdown   string
}

type agDay struct {
	Label  string // « lun. 13 »
	Date   string // 2026-07-13
	Today  bool
	Past   bool   // jour entièrement passé : rien de posable
	PastPx int    // hauteur grisée depuis le haut de la colonne (heures révolues)
	MinMin int    // première minute encore posable (borne le glisser-déposer)
	Blocks []agBlock
}

type agendaView struct {
	WeekLabel          string
	Week               string // lundi de la semaine affichée, YYYY-MM-DD
	PrevWeek, NextWeek string
	Days               []agDay
	Hours              []string
	GridHeight         int
	NowTop             int // px de la ligne « maintenant », -1 hors plage
	Err                string
}

// slotsCalendar (GET /ui/slots?week=YYYY-MM-DD) rend le calendrier.
func (h *handlers) slotsCalendar(w http.ResponseWriter, r *http.Request) {
	h.renderAgenda(w, r, parseWeek(r.FormValue("week")), "")
}

// slotsCreate (POST /ui/slots) crée une disponibilité posée au glisser-déposer.
func (h *handlers) slotsCreate(w http.ResponseWriter, r *http.Request) {
	user, _ := userFromContext(r.Context())
	week := parseWeek(r.FormValue("week"))

	day, errDay := time.ParseInLocation("2006-01-02", r.FormValue("day"), time.Local)
	startMin, _ := strconv.Atoi(r.FormValue("start"))
	endMin, _ := strconv.Atoi(r.FormValue("end"))
	if errDay != nil || startMin%15 != 0 || endMin%15 != 0 ||
		startMin < agDayStartMin || endMin > agDayEndMin || endMin-startMin < 15 {
		h.renderAgenda(w, r, week, "Créneau invalide.")
		return
	}
	begin := day.Add(time.Duration(startMin) * time.Minute)
	end := day.Add(time.Duration(endMin) * time.Minute)
	if !begin.After(time.Now()) {
		h.renderAgenda(w, r, week, "Impossible de poser un créneau dans le passé.")
		return
	}

	tok, err := h.sessions.FreshToken(r, h.oauth)
	if err != nil {
		h.renderAgenda(w, r, week, "Ta session a expiré — reconnecte-toi.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	if err := h.ft.CreateSlot(ctx, user.Login, tok.AccessToken, user.ID, begin, end); err != nil {
		h.renderAgenda(w, r, week, agendaErrMsg("poser ce créneau", err))
		return
	}
	h.renderAgenda(w, r, week, "")
}

// slotsDelete (POST /ui/slots/delete) retire un bloc de disponibilité : un
// bloc affiché regroupe plusieurs granules de 15 min, on les supprime toutes.
func (h *handlers) slotsDelete(w http.ResponseWriter, r *http.Request) {
	user, _ := userFromContext(r.Context())
	week := parseWeek(r.FormValue("week"))

	var ids []int
	for _, part := range strings.Split(r.FormValue("ids"), ",") {
		if id, err := strconv.Atoi(strings.TrimSpace(part)); err == nil && id > 0 {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 || len(ids) > 32 {
		h.renderAgenda(w, r, week, "Créneau invalide.")
		return
	}

	tok, err := h.sessions.FreshToken(r, h.oauth)
	if err != nil {
		h.renderAgenda(w, r, week, "Ta session a expiré — reconnecte-toi.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	for _, id := range ids {
		if err := h.ft.DeleteSlot(ctx, user.Login, tok.AccessToken, id); err != nil {
			h.renderAgenda(w, r, week, agendaErrMsg("retirer ce créneau", err))
			return
		}
	}
	h.renderAgenda(w, r, week, "")
}

// agendaErrMsg met en français l'échec d'une écriture de slot.
func agendaErrMsg(action string, err error) string {
	if scopeForbidden(err) {
		return "L'application 42 n'a pas le droit de gérer les slots (scope « public »). Passe par l'intra pour " + action + "."
	}
	var apiErr *fortytwo.APIError
	if errors.As(err, &apiErr) && apiErr.Status < 500 {
		return "42 a refusé de " + action + " (" + strconv.Itoa(apiErr.Status) + ")."
	}
	return "L'API 42 n'a pas répondu — impossible de " + action + " pour l'instant."
}

func (h *handlers) renderAgenda(w http.ResponseWriter, r *http.Request, week time.Time, errMsg string) {
	user, _ := userFromContext(r.Context())
	tok, err := h.sessions.FreshToken(r, h.oauth)
	if err != nil {
		h.renderAgendaError(w, week, "Ta session a expiré. Déconnecte-toi puis reconnecte-toi.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	slots, err := h.ft.Slots(ctx, user.Login, tok.AccessToken, week, week.AddDate(0, 0, 7))
	if err != nil {
		if scopeForbidden(err) {
			h.renderAgendaError(w, week, "Ton jeton 42 n'a pas le scope « projects », nécessaire aux créneaux. Pour l'activer : coche « projects » sur l'application (profile.intra.42.fr/oauth/applications), lance le serveur avec MOULINETTE_42_SCOPE=\"public projects\", puis déconnecte-toi et reconnecte-toi.")
			return
		}
		h.renderAgendaError(w, week, "L'API 42 n'a pas répondu. Réessaie dans un instant.")
		return
	}

	v := buildAgenda(slots, week, time.Now())
	v.Err = errMsg
	if err := h.tmpl.ExecuteTemplate(w, "agenda_calendar", v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// renderAgendaError réutilise le panneau d'erreur du dashboard, avec un
// Réessayer qui recharge le calendrier de la même semaine.
func (h *handlers) renderAgendaError(w http.ResponseWriter, week time.Time, msg string) {
	h.tmpl.ExecuteTemplate(w, "dash_error", map[string]any{
		"Kind":    "warn",
		"Message": msg,
		"Retry":   "/ui/slots?week=" + week.Format("2006-01-02"),
	})
}

// parseWeek recale une date (ou aujourd'hui si absente/invalide) sur son lundi.
func parseWeek(s string) time.Time {
	t, err := time.ParseInLocation("2006-01-02", s, time.Local)
	if err != nil {
		t = time.Now()
	}
	weekday := (int(t.Weekday()) + 6) % 7
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.Local).AddDate(0, 0, -weekday)
}

func buildAgenda(slots []fortytwo.Slot, week, now time.Time) agendaView {
	v := agendaView{
		Week:       week.Format("2006-01-02"),
		PrevWeek:   week.AddDate(0, 0, -7).Format("2006-01-02"),
		NextWeek:   week.AddDate(0, 0, 7).Format("2006-01-02"),
		GridHeight: (agDayEndMin - agDayStartMin) / 15 * agPxPer15,
		NowTop:     -1,
	}
	end := week.AddDate(0, 0, 6)
	if week.Month() == end.Month() {
		v.WeekLabel = fmt.Sprintf("%d – %d %s %d", week.Day(), end.Day(), frMonthsShort[end.Month()-1], end.Year())
	} else {
		v.WeekLabel = fmt.Sprintf("%d %s – %d %s %d", week.Day(), frMonthsShort[week.Month()-1], end.Day(), frMonthsShort[end.Month()-1], end.Year())
	}
	for m := agDayStartMin; m < agDayEndMin; m += 60 {
		v.Hours = append(v.Hours, fmt.Sprintf("%02d:00", m/60))
	}

	perDay := make([][]fortytwo.Slot, 7)
	for _, s := range slots {
		idx := daysApart(week, s.BeginAt.In(time.Local))
		if idx >= 0 && idx < 7 {
			perDay[idx] = append(perDay[idx], s)
		}
	}
	for i := 0; i < 7; i++ {
		day := week.AddDate(0, 0, i)
		rel := daysApart(now, day) // <0 passé, 0 aujourd'hui, >0 futur
		d := agDay{
			Label:  fmt.Sprintf("%s %d", frDaysShort[day.Weekday()], day.Day()),
			Date:   day.Format("2006-01-02"),
			Today:  rel == 0,
			Past:   rel < 0,
			MinMin: agDayStartMin, // jour futur : tout est posable
		}
		d.Blocks = groupSlots(perDay[i])
		switch {
		case rel < 0:
			// Jour révolu : toute la colonne est grisée et rien n'y est posable.
			d.PastPx = v.GridHeight
			d.MinMin = agDayEndMin
		case rel == 0:
			mins := now.Hour()*60 + now.Minute()
			switch {
			case mins <= agDayStartMin:
				// Avant l'ouverture de la grille : journée entièrement posable.
			case mins >= agDayEndMin:
				d.PastPx = v.GridHeight
				d.MinMin = agDayEndMin
			default:
				// On ne peut poser qu'à partir du prochain quart d'heure ; on
				// grise jusque-là, et la ligne « maintenant » marque l'instant.
				d.MinMin = (mins/15)*15 + 15
				if d.MinMin > agDayEndMin {
					d.MinMin = agDayEndMin
				}
				d.PastPx = (d.MinMin - agDayStartMin) * agPxPer15 / 15
				v.NowTop = (mins - agDayStartMin) * agPxPer15 / 15
			}
		}
		v.Days = append(v.Days, d)
	}
	return v
}

// slotRun est une suite de granules de 15 min contigus de même nature, en
// cours de fusion en un bloc affichable.
type slotRun struct {
	begin, end time.Time
	ids        []string
	booked     bool
	bk         fortytwo.SlotBooking
}

// groupSlots fusionne les granules de 15 min contigus de même nature en un
// seul bloc affichable (l'intra les montre un par un — un bloc unique avec un
// seul ✕ est plus lisible).
func groupSlots(slots []fortytwo.Slot) []agBlock {
	sort.Slice(slots, func(i, j int) bool { return slots[i].BeginAt.Before(slots[j].BeginAt) })

	var blocks []agBlock
	var cur *slotRun
	flush := func() {
		if cur == nil {
			return
		}
		if b, ok := blockFrom(*cur); ok {
			blocks = append(blocks, b)
		}
		cur = nil
	}
	for _, s := range slots {
		booked := s.ScaleTeam.Present
		sameBooking := !booked || (cur != nil && cur.bk.ID == s.ScaleTeam.ID)
		if cur != nil && cur.booked == booked && sameBooking && s.BeginAt.Equal(cur.end) {
			cur.end = s.EndAt
			cur.ids = append(cur.ids, strconv.Itoa(s.ID))
			continue
		}
		flush()
		cur = &slotRun{begin: s.BeginAt, end: s.EndAt, ids: []string{strconv.Itoa(s.ID)}, booked: booked, bk: s.ScaleTeam}
	}
	flush()
	return blocks
}

func blockFrom(a slotRun) (agBlock, bool) {
	begin, end := a.begin.In(time.Local), a.end.In(time.Local)
	startMin := begin.Hour()*60 + begin.Minute()
	endMin := end.Hour()*60 + end.Minute()
	if endMin == 0 {
		endMin = 24 * 60 // un slot qui finit à minuit appartient au jour courant
	}
	if startMin < agDayStartMin {
		startMin = agDayStartMin
	}
	if endMin > agDayEndMin {
		endMin = agDayEndMin
	}
	if endMin <= startMin {
		return agBlock{}, false
	}

	b := agBlock{
		Top:    (startMin - agDayStartMin) * agPxPer15 / 15,
		Height: (endMin-startMin)*agPxPer15/15 - 2,
		Label:  fmt.Sprintf("%02d:%02d – %02d:%02d", begin.Hour(), begin.Minute(), endMin/60%24, endMin%60),
		IDs:    strings.Join(a.ids, ","),
		Booked: a.booked,
	}
	if a.booked {
		for _, cu := range a.bk.Correcteds {
			if cu.Login != "" {
				b.Who = cu.Login
				b.WhoURL = "https://profile.intra.42.fr/users/" + url.PathEscape(cu.Login)
				break
			}
		}
		b.Countdown = humanUntil(a.begin)
	}
	return b, true
}

// daysApart compte les jours calendaires entre a et b (dates locales) : on
// compare les midis des deux jours et on arrondit, ce qui absorbe l'heure
// d'été (±1 h) dans les deux sens — une troncature ferait dérailler les
// écarts négatifs.
func daysApart(a, b time.Time) int {
	a = time.Date(a.Year(), a.Month(), a.Day(), 12, 0, 0, 0, time.Local)
	b = time.Date(b.Year(), b.Month(), b.Day(), 12, 0, 0, 0, time.Local)
	return int(math.Round(b.Sub(a).Hours() / 24))
}

// frDateTimeShort : « mar. 14 juil. · 15h00 ».
func frDateTimeShort(t time.Time) string {
	t = t.Local()
	return fmt.Sprintf("%s · %dh%02d", frDateShort(t), t.Hour(), t.Minute())
}
