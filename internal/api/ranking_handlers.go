package api

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/tristan-reig/ft-moulinette/internal/pool"
)

// examTab est un exam proposé dans le sous-onglet Exam.
type examTab struct {
	Value string
	Label string
}

// examScheduleView décrit l'horaire d'un exam pour l'affichage sous les
// boutons (date, plage horaire, durée), en heure locale du serveur.
type examScheduleView struct {
	Date     string // ex. 11/07/2026
	Start    string // ex. 08:00
	End      string // ex. 12:00
	Duration string // ex. 4h00
	Active   bool   // l'exam est en cours en ce moment
}

// buildExamSchedule met en forme la fenêtre [begin, end] d'un exam.
func buildExamSchedule(begin, end time.Time) examScheduleView {
	begin, end = begin.Local(), end.Local()
	d := end.Sub(begin)
	now := time.Now()
	return examScheduleView{
		Date:     begin.Format("02/01/2006"),
		Start:    begin.Format("15:04"),
		End:      end.Format("15:04"),
		Duration: fmt.Sprintf("%dh%02d", int(d.Hours()), int(d.Minutes())%60),
		Active:   !now.Before(begin) && !now.After(end),
	}
}

var examTabs = []examTab{
	{"00", "Exam 00"},
	{"01", "Exam 01"},
	{"02", "Exam 02"},
	{"final", "Exam Final"},
}

var poolMonthLabels = map[string]string{
	"july":      "Juillet",
	"august":    "Août",
	"september": "Septembre",
}

// coalitionPill est un filtre de coalition affiché au-dessus du classement score.
type coalitionPill struct {
	Name   string
	Color  string
	URL    string
	Active bool
}

// rankingPage (GET /classement) rend la page Classement avec ses sous-onglets.
func (h *handlers) rankingPage(w http.ResponseWriter, r *http.Request) {
	user, _ := userFromContext(r.Context())

	data := h.navFlags(user)
	data["User"] = user
	data["Exams"] = examTabs
	data["Page"] = "classement"
	if err := h.tmpl.ExecuteTemplate(w, "ranking", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// rankingFragment (GET /ui/classement) renvoie le fragment htmx d'un
// classement de la piscine en cours :
// ?tab=score|projects|exam|progress[&exam=00][&coalition=Nom].
// La session est déduite de la date serveur ; hors saison, le classement
// est indisponible.
func (h *handlers) rankingFragment(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	tab := q.Get("tab")
	if tab != "score" && tab != "projects" && tab != "exam" && tab != "progress" {
		http.Error(w, "onglet invalide", http.StatusBadRequest)
		return
	}

	exam := q.Get("exam")
	if exam == "" {
		exam = "00"
	}
	if _, ok := pool.ExamSlugs[exam]; !ok {
		http.Error(w, "exam invalide", http.StatusBadRequest)
		return
	}

	self := url.Values{"tab": {tab}}
	if tab == "exam" {
		self.Set("exam", exam)
	}

	data := map[string]any{
		"Tab":           tab,
		"Exam":          exam,
		"ExamLabel":     pool.ExamLabels[exam],
		"MonthLabel":    "",
		"Year":          0,
		"Err":           "",
		"UpdatedAt":     time.Time{},
		"RefreshSec":    600,
		"ScoreRows":     []pool.ScoreRow(nil),
		"ProjectRows":   []pool.ProjectRow(nil),
		"ExamRows":      []pool.ExamRow(nil),
		"Coalitions":    []coalitionPill(nil),
		"Charts":        []progressChart(nil),
		"SnapshotCount": 0,
	}

	month, year, inSeason := pool.CurrentSession(time.Now())
	if !inSeason {
		data["State"] = string(pool.StateUnavailable)
		data["SelfURL"] = "/ui/classement?" + self.Encode()
	} else {
		yearStr := strconv.Itoa(year)
		var status pool.Status
		switch tab {
		case "score":
			rows, st := h.pool.Score(month, yearStr)
			status = st
			selected := q.Get("coalition")
			data["Coalitions"], data["ScoreRows"] = filterCoalition(rows, selected)
			if selected != "" {
				self.Set("coalition", selected)
			}
		case "projects":
			data["ProjectRows"], status = h.pool.Projects(month, yearStr)
		case "exam":
			data["ExamRows"], status = h.pool.Exam(month, yearStr, exam)
			data["RefreshSec"] = int(h.pool.ExamRefresh(exam).Seconds())
			if begin, end, ok := h.pool.ExamWindow(exam); ok {
				data["ExamSchedule"] = buildExamSchedule(begin, end)
			}
		case "progress":
			// Prime le cache score (c'est lui qui alimente les relevés) et
			// sert l'historique existant sans écran de chargement si possible.
			_, status = h.pool.Score(month, yearStr)
			user, _ := userFromContext(r.Context())
			snaps := h.pool.History(month, yearStr)
			data["Charts"] = buildProgressCharts(snaps, user.Login)
			data["SnapshotCount"] = len(snaps)
			data["RefreshSec"] = 600
			if len(snaps) > 0 && status.State == pool.StateLoading {
				status = pool.Status{State: pool.StateReady, UpdatedAt: snaps[len(snaps)-1].At}
			}
		}
		data["MonthLabel"] = poolMonthLabels[month]
		data["Year"] = year
		data["State"] = string(status.State)
		data["Err"] = status.Err
		data["UpdatedAt"] = status.UpdatedAt
		data["SelfURL"] = "/ui/classement?" + self.Encode()
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.tmpl.ExecuteTemplate(w, "ranking_content", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// filterCoalition construit les pills de filtre (une par coalition présente,
// dans l'ordre du classement) et filtre les lignes si une coalition valide
// est sélectionnée.
func filterCoalition(rows []pool.ScoreRow, selected string) ([]coalitionPill, []pool.ScoreRow) {
	var pills []coalitionPill
	seen := map[string]bool{}
	validSelection := false
	for _, row := range rows {
		if seen[row.Coalition] {
			continue
		}
		seen[row.Coalition] = true
		u := url.Values{"tab": {"score"}, "coalition": {row.Coalition}}
		pills = append(pills, coalitionPill{
			Name:   row.Coalition,
			Color:  row.Color,
			URL:    "/ui/classement?" + u.Encode(),
			Active: row.Coalition == selected,
		})
		if row.Coalition == selected {
			validSelection = true
		}
	}
	if !validSelection {
		return pills, rows
	}

	filtered := make([]pool.ScoreRow, 0, len(rows))
	for _, row := range rows {
		if row.Coalition == selected {
			filtered = append(filtered, row)
		}
	}
	return pills, filtered
}
