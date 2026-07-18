package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/tristan-reig/ft-moulinette/internal/fortytwo"
)

// Export iCalendar de l'agenda : créneaux de correction + défenses, à
// s'abonner depuis Apple/Google Calendar. Les applications calendrier ne
// savent pas porter un cookie de session, et interroger l'API 42 à chaque
// poll grillerait le quota : l'endpoint sert donc un instantané alimenté par
// les rendus normaux du site (agenda, carte défenses) — zéro appel API, et
// une URL « capacité » signée HMAC à la place de la session.

type icsDefense struct {
	ID      int
	Give    bool
	Project string
	Who     string
	Begin   time.Time
}

type icsData struct {
	Weeks    map[string][]fortytwo.Slot // clé : lundi YYYY-MM-DD
	Defenses []icsDefense
	At       time.Time
}

type icsStore struct {
	mu sync.Mutex
	m  map[string]*icsData
}

func newICSStore() *icsStore {
	return &icsStore{m: make(map[string]*icsData)}
}

func (s *icsStore) entry(login string) *icsData {
	d, ok := s.m[login]
	if !ok {
		d = &icsData{Weeks: make(map[string][]fortytwo.Slot)}
		s.m[login] = d
	}
	return d
}

func (s *icsStore) setSlots(login, week string, slots []fortytwo.Slot) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.entry(login)
	d.Weeks[week] = slots
	d.At = time.Now()
	// Les semaines révolues ne servent plus à rien dans un calendrier.
	for k := range d.Weeks {
		if t, err := time.ParseInLocation("2006-01-02", k, time.Local); err == nil && t.AddDate(0, 0, 7).Before(time.Now().AddDate(0, 0, -7)) {
			delete(d.Weeks, k)
		}
	}
}

func (s *icsStore) setDefenses(login string, defs []icsDefense) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.entry(login)
	d.Defenses = defs
	d.At = time.Now()
}

func (s *icsStore) get(login string) (icsData, bool) {
	if s == nil {
		return icsData{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.m[login]
	if !ok {
		return icsData{}, false
	}
	out := icsData{Weeks: make(map[string][]fortytwo.Slot, len(d.Weeks)), Defenses: append([]icsDefense(nil), d.Defenses...), At: d.At}
	for k, v := range d.Weeks {
		out.Weeks[k] = v
	}
	return out, true
}

// icsKey signe le login avec le secret OAuth de l'app : une URL de calendrier
// stable par utilisateur, sans stockage ni session.
func icsKey(secret, login string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("ics:" + login))
	return hex.EncodeToString(mac.Sum(nil))[:32]
}

var icsLoginRe = regexp.MustCompile(`^[a-z0-9_-]{2,20}$`)

// agendaICS (GET /agenda.ics?login=X&k=SIG) sert le calendrier iCalendar.
// Public (pas de session) : la signature HMAC fait office d'authentification.
func (h *handlers) agendaICS(w http.ResponseWriter, r *http.Request) {
	if h.maintenance {
		http.Error(w, "maintenance en cours", http.StatusServiceUnavailable)
		return
	}

	login := r.FormValue("login")
	key := r.FormValue("k")
	if !icsLoginRe.MatchString(login) || !hmac.Equal([]byte(key), []byte(icsKey(h.oauth.ClientSecret, login))) {
		http.NotFound(w, r)
		return
	}

	data, ok := h.icsSnap.get(login)
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="ft_moulinette.ics"`)

	var b strings.Builder
	b.WriteString("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//ft_moulinette//agenda//FR\r\n")
	b.WriteString("X-WR-CALNAME:Corrections 42 (" + login + ")\r\n")
	if ok {
		writeICSEvents(&b, login, data)
	}
	b.WriteString("END:VCALENDAR\r\n")
	fmt.Fprint(w, b.String())
}

func icsEscape(s string) string {
	s = strings.NewReplacer("\\", "\\\\", ";", "\\;", ",", "\\,", "\n", "\\n").Replace(s)
	return s
}

func icsTime(t time.Time) string {
	return t.UTC().Format("20060102T150405Z")
}

func writeICSEvents(b *strings.Builder, login string, data icsData) {
	event := func(uid, summary string, begin, end time.Time) {
		b.WriteString("BEGIN:VEVENT\r\n")
		b.WriteString("UID:" + uid + "@ft-moulinette\r\n")
		b.WriteString("DTSTAMP:" + icsTime(data.At) + "\r\n")
		b.WriteString("DTSTART:" + icsTime(begin) + "\r\n")
		b.WriteString("DTEND:" + icsTime(end) + "\r\n")
		b.WriteString("SUMMARY:" + icsEscape(summary) + "\r\n")
		b.WriteString("END:VEVENT\r\n")
	}

	// Les granules de 15 min sont fusionnées en plages, comme dans l'agenda.
	weeks := make([]string, 0, len(data.Weeks))
	for k := range data.Weeks {
		weeks = append(weeks, k)
	}
	sort.Strings(weeks)
	for _, wk := range weeks {
		for _, blk := range mergeSlots(data.Weeks[wk]) {
			if blk.booked {
				who := blk.who
				if who == "" {
					who = "corrigé pas encore révélé"
				}
				event(fmt.Sprintf("slot-%s", blk.ids), "Correction réservée — "+who, blk.begin, blk.end)
			} else {
				event(fmt.Sprintf("slot-%s", blk.ids), "Dispo correction", blk.begin, blk.end)
			}
		}
	}
	for _, d := range data.Defenses {
		summary := "On me corrige — " + d.Project
		if d.Give {
			summary = "Je corrige"
			if d.Who != "" {
				summary += " " + d.Who
			}
			summary += " — " + d.Project
		}
		// L'API ne donne pas la durée de la défense : 30 min par convention.
		event(fmt.Sprintf("def-%d", d.ID), summary, d.Begin, d.Begin.Add(30*time.Minute))
	}
}

type icsBlock struct {
	begin, end time.Time
	booked     bool
	who        string
	ids        string
}

// mergeSlots fusionne les granules contiguës de même nature (équivalent
// temporel de groupSlots, sans la géométrie en pixels).
func mergeSlots(slots []fortytwo.Slot) []icsBlock {
	sorted := append([]fortytwo.Slot(nil), slots...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].BeginAt.Before(sorted[j].BeginAt) })

	var out []icsBlock
	for _, s := range sorted {
		who := ""
		for _, cu := range s.ScaleTeam.Correcteds {
			if cu.Login != "" {
				who = cu.Login
				break
			}
		}
		if n := len(out); n > 0 && out[n-1].booked == s.ScaleTeam.Present && out[n-1].end.Equal(s.BeginAt) && (!s.ScaleTeam.Present || out[n-1].who == who) {
			out[n-1].end = s.EndAt
			out[n-1].ids += fmt.Sprintf("-%d", s.ID)
			continue
		}
		out = append(out, icsBlock{begin: s.BeginAt, end: s.EndAt, booked: s.ScaleTeam.Present, who: who, ids: fmt.Sprintf("%d", s.ID)})
	}
	return out
}

// icsURL construit l'URL d'abonnement pour l'utilisateur courant.
func (h *handlers) icsURL(login string) string {
	v := url.Values{}
	v.Set("login", login)
	v.Set("k", icsKey(h.oauth.ClientSecret, login))
	return "/agenda.ics?" + v.Encode()
}
