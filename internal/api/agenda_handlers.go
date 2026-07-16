package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
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
	ID        int    // id du scale_team (clé stable pour les notifications JS)
	Direction string // « Je corrige » | « On me corrige »
	Give      bool   // true = je corrige
	Project   string
	Who       string // login de l'autre partie, "" tant que l'intra le cache
	WhoURL    string
	When      string
	BeginRFC  string // instant RFC3339, pour le décompte côté client
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
			ID:        st.ID,
			Give:      it.give,
			Direction: "On me corrige",
			Project:   defenseProject(ctx, h, user.Login, tok, st),
			When:      frDateTimeShort(st.BeginAt),
			BeginRFC:  st.BeginAt.UTC().Format(time.RFC3339),
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

	// Alimente aussi l'export .ics (zéro appel API supplémentaire).
	defs := make([]icsDefense, 0, len(v.Rows))
	for i, it := range items {
		if i == len(v.Rows) {
			break
		}
		defs = append(defs, icsDefense{
			ID:      it.st.ID,
			Give:    it.give,
			Project: v.Rows[i].Project,
			Who:     v.Rows[i].Who,
			Begin:   it.st.BeginAt,
		})
	}
	h.icsSnap.setDefenses(user.Login, defs)
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
	Top, Height      int    // géométrie en px dans la colonne
	StartMin, EndMin int    // bornes en minutes depuis minuit (pour le JS)
	Label            string // « 15:00 – 16:30 »
	IDs              string // ids des slots 42 regroupés, "12,13,14"
	Booked           bool
	Who              string // login du corrigé si révélé
	WhoURL           string
	Countdown        string
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
	NowTop             int    // px de la ligne « maintenant », -1 hors plage
	ICSURL             string // abonnement calendrier (.ics)
	Err                string
}

// slotsCalendar (GET /ui/slots?week=YYYY-MM-DD) rend le calendrier.
func (h *handlers) slotsCalendar(w http.ResponseWriter, r *http.Request) {
	h.renderAgenda(w, r, parseWeek(r.FormValue("week")), "")
}

// --- Enregistrement différé ---
//
// L'agenda s'édite entièrement en local : poser, déplacer, étirer, retirer ne
// touchent pas l'API 42. Le bouton « Enregistrer » envoie le lot ici, qui
// l'applique chez 42 : suppressions d'abord (elles libèrent la place), puis
// créations. Quand 42 refuse une création (place déjà prise côté serveur),
// elle est décalée de 15 min en 15 min jusqu'à trouver un trou (2 h maxi) ;
// chaque décalage est signalé dans la réponse pour rester transparent.

type slotSyncRange struct {
	Day   string `json:"day"`
	Start int    `json:"start"`
	End   int    `json:"end"`
}

type slotSyncRequest struct {
	Delete []int           `json:"delete"`
	Create []slotSyncRange `json:"create"`
}

type slotSyncResult struct {
	Saved   int      `json:"saved"`
	Shifted []string `json:"shifted"`
	Failed  []string `json:"failed"`
}

const slotShiftMax = 8 * 15 // décalage maximal en cas de conflit : 2 h

// slotsSync (POST /ui/slots/sync) applique le lot de modifications locales.
func (h *handlers) slotsSync(w http.ResponseWriter, r *http.Request) {
	user, _ := userFromContext(r.Context())

	var req slotSyncRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		slotError(w, http.StatusBadRequest, "Requête illisible.")
		return
	}
	if len(req.Delete) > 128 || len(req.Create) > 64 || (len(req.Delete) == 0 && len(req.Create) == 0) {
		slotError(w, http.StatusBadRequest, "Lot de modifications invalide.")
		return
	}

	tok, err := h.sessions.FreshToken(r, h.oauth)
	if err != nil {
		slotError(w, http.StatusUnauthorized, "Ta session a expiré — reconnecte-toi.")
		return
	}
	// Chaque appel 42 est espacé de ~550 ms : un gros lot prend son temps.
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()

	res := slotSyncResult{Shifted: []string{}, Failed: []string{}}

	refused := 0
	for _, id := range req.Delete {
		if id <= 0 {
			continue
		}
		if err := h.ft.DeleteSlot(ctx, user.Login, tok.AccessToken, id); err != nil {
			var apiErr *fortytwo.APIError
			if errors.As(err, &apiErr) && apiErr.Status < 500 {
				// Granule refusée (réservée ou verrouillée par l'intra) : on la
				// laisse en place et on continue le reste du lot.
				log.Printf("[agenda] suppression refusée (slot %d) : %v", id, err)
				refused++
				continue
			}
			slotFail(w, "enregistrer tes créneaux", err) // panne : inutile d'insister
			return
		}
	}
	if refused > 0 {
		res.Failed = append(res.Failed, fmt.Sprintf("%d suppression(s) refusée(s) — créneau(x) sans doute réservé(s)", refused))
	}

	now := time.Now()
	for _, c := range req.Create {
		day, errDay := time.ParseInLocation("2006-01-02", c.Day, time.Local)
		if errDay != nil || c.Start%15 != 0 || c.End%15 != 0 ||
			c.Start < agDayStartMin || c.End > agDayEndMin || c.End-c.Start < 15 {
			res.Failed = append(res.Failed, "un créneau invalide a été ignoré")
			continue
		}
		want := fmt.Sprintf("%s %02d:%02d", frDateShort(day), c.Start/60, c.Start%60)
		if daysApart(now, day) < 0 {
			res.Failed = append(res.Failed, want+" est déjà passé")
			continue
		}

		saved := false
		for shift := 0; shift <= slotShiftMax && c.End+shift <= agDayEndMin; shift += 15 {
			begin := day.Add(time.Duration(c.Start+shift) * time.Minute)
			if !begin.After(now) {
				continue // ce début est déjà passé : on essaie plus tard dans la journée
			}
			end := day.Add(time.Duration(c.End+shift) * time.Minute)
			_, err := h.ft.CreateSlot(ctx, user.Login, tok.AccessToken, user.ID, begin, end)
			if err == nil {
				res.Saved++
				if shift > 0 {
					res.Shifted = append(res.Shifted, fmt.Sprintf("%s décalé à %02d:%02d", want, (c.Start+shift)/60, (c.Start+shift)%60))
				}
				saved = true
				break
			}
			// 403 sur POST /v2/slots = scope manquant, 401 = session morte :
			// tout le lot est condamné, on s'arrête là.
			if scopeForbidden(err) || isUnauthorized(err) {
				slotFail(w, "enregistrer tes créneaux", err)
				return
			}
			var apiErr *fortytwo.APIError
			if errors.As(err, &apiErr) && apiErr.Status < 500 {
				continue // refus applicatif (chevauchement côté 42…) : 15 min plus tard
			}
			slotFail(w, "enregistrer tes créneaux", err)
			return
		}
		if !saved {
			res.Failed = append(res.Failed, want+" n'a pas trouvé de place (décalages jusqu'à 2 h essayés)")
		}
	}

	writeJSON(w, http.StatusOK, res)
}

func isUnauthorized(err error) bool {
	var apiErr *fortytwo.APIError
	return errors.As(err, &apiErr) && apiErr.Status == http.StatusUnauthorized
}

// slotFail journalise l'échec (l'agenda échouait en silence dans les logs
// serveur) puis répond au client avec le code et le message adaptés.
func slotFail(w http.ResponseWriter, action string, err error) {
	log.Printf("[agenda] échec de %s : %v", action, err)
	slotError(w, slotStatus(err), agendaErrMsg(action, err))
}

func slotError(w http.ResponseWriter, status int, msg string) {
	http.Error(w, msg, status)
}

// slotStatus mappe une erreur 42 sur un code que le client distingue : 403
// (scope), 502 (panne/5xx) ou 409 (refus applicatif).
func slotStatus(err error) int {
	if scopeForbidden(err) {
		return http.StatusForbidden
	}
	var apiErr *fortytwo.APIError
	if errors.As(err, &apiErr) {
		if apiErr.Status >= 500 {
			return http.StatusBadGateway
		}
		return http.StatusConflict
	}
	return http.StatusBadGateway
}


// agendaErrMsg met en français l'échec d'une écriture de slot.
func agendaErrMsg(action string, err error) string {
	var apiErr *fortytwo.APIError
	if errors.As(err, &apiErr) {
		switch {
		// 403 sur une granule précise (/v2/slots/<id>) : ce n'est pas un
		// problème de scope — l'intra refuse de toucher CE créneau, très
		// probablement parce qu'il vient d'être réservé pour une défense (ou
		// qu'il est verrouillé à l'approche de son heure).
		case apiErr.Status == http.StatusForbidden && strings.HasPrefix(apiErr.Path, "/v2/slots/"):
			return "42 refuse de toucher ce créneau : il est sans doute déjà réservé pour une défense (ou verrouillé par l'intra). Le calendrier se recharge."
		case apiErr.Status == http.StatusForbidden:
			return "L'application 42 n'a pas le droit de gérer les slots — il manque le scope « projects » (à cocher sur l'app intra + MOULINETTE_42_SCOPE, puis reconnexion)."
		case apiErr.Status < 500:
			return "42 a refusé de " + action + " (" + strconv.Itoa(apiErr.Status) + ")."
		}
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

	// Alimente l'export .ics au passage : le calendrier abonné reflète ce que
	// l'utilisateur a vu ici, sans coûter le moindre appel API supplémentaire.
	h.icsSnap.setSlots(user.Login, week.Format("2006-01-02"), slots)

	v := buildAgenda(slots, week, time.Now())
	v.Err = errMsg
	v.ICSURL = h.icsURL(user.Login)
	if err := h.tmpl.ExecuteTemplate(w, "agenda_calendar", v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// slotsCopyWeek (GET /ui/slots/copy?week=YYYY-MM-DD) renvoie, en JSON, les
// dispos libres de la semaine PRÉCÉDENTE projetées sur la semaine demandée :
// le client en fait des blocs locaux (brouillon), rien n'est poussé chez 42.
// Coût : un seul appel API (mis en cache).
func (h *handlers) slotsCopyWeek(w http.ResponseWriter, r *http.Request) {
	user, _ := userFromContext(r.Context())
	week := parseWeek(r.FormValue("week"))

	tok, err := h.sessions.FreshToken(r, h.oauth)
	if err != nil {
		slotError(w, http.StatusUnauthorized, "Ta session a expiré — reconnecte-toi.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	prev := week.AddDate(0, 0, -7)
	slots, err := h.ft.Slots(ctx, user.Login, tok.AccessToken, prev, week)
	if err != nil {
		slotFail(w, "recopier la semaine précédente", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"create": shiftFreeRanges(slots, prev, 7)})
}

// shiftFreeRanges fusionne les granules LIBRES d'une semaine en plages et les
// projette shiftDays plus tard, bornées à la grille de l'agenda.
func shiftFreeRanges(slots []fortytwo.Slot, weekFrom time.Time, shiftDays int) []slotSyncRange {
	var free []fortytwo.Slot
	for _, s := range slots {
		if !s.ScaleTeam.Present {
			free = append(free, s)
		}
	}
	out := []slotSyncRange{}
	for _, blk := range mergeSlots(free) {
		begin := blk.begin.In(time.Local)
		end := blk.end.In(time.Local)
		dayIdx := daysApart(weekFrom, begin)
		if dayIdx < 0 || dayIdx > 6 {
			continue
		}
		startMin := begin.Hour()*60 + begin.Minute()
		endMin := end.Hour()*60 + end.Minute()
		if endMin == 0 {
			endMin = 24 * 60
		}
		if startMin < agDayStartMin {
			startMin = agDayStartMin
		}
		if endMin > agDayEndMin {
			endMin = agDayEndMin
		}
		if endMin-startMin < 15 {
			continue
		}
		day := weekFrom.AddDate(0, 0, dayIdx+shiftDays)
		out = append(out, slotSyncRange{Day: day.Format("2006-01-02"), Start: startMin, End: endMin})
	}
	return out
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
		// Les créneaux révolus (déjà passés) encombrent inutilement la grille :
		// on ne les affiche pas. Un créneau en cours (début passé, fin future)
		// reste visible.
		if !s.EndAt.After(now) {
			continue
		}
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
		Top:      (startMin - agDayStartMin) * agPxPer15 / 15,
		Height:   (endMin-startMin)*agPxPer15/15 - 2,
		StartMin: startMin,
		EndMin:   endMin,
		Label:    fmt.Sprintf("%02d:%02d – %02d:%02d", begin.Hour(), begin.Minute(), endMin/60%24, endMin%60),
		IDs:      strings.Join(a.ids, ","),
		Booked:   a.booked,
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
