package api

import (
	"encoding/json"
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

// parisLoc est le fuseau horaire de la France (Europe/Paris, CET/CEST avec
// heure d'été). On l'utilise pour afficher les horaires d'exam quelle que
// soit la timezone du serveur (le VPS tourne en UTC). La base tzdata est
// embarquée dans le binaire (import time/tzdata dans cmd/server) pour que
// LoadLocation fonctionne même dans un conteneur sans tzdata.
var parisLoc = loadParis()

func loadParis() *time.Location {
	loc, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		// Repli : CET fixe (UTC+1). Sans tzdata on perd l'heure d'été,
		// mais on évite d'afficher de l'UTC brut.
		return time.FixedZone("CET", 3600)
	}
	return loc
}

// examScheduleView décrit l'horaire d'un exam pour l'affichage sous les
// boutons (date, plage horaire, durée), en heure de France.
type examScheduleView struct {
	Date     string // ex. 11/07/2026
	Start    string // ex. 08:00
	End      string // ex. 12:00
	Duration string // ex. 4h00
	Active   bool   // l'exam est en cours en ce moment
}

// buildExamSchedule met en forme la fenêtre [begin, end] d'un exam, en
// heure de France.
func buildExamSchedule(begin, end time.Time) examScheduleView {
	d := end.Sub(begin) // durée = écart d'instants, indépendant du fuseau
	now := time.Now()
	begin, end = begin.In(parisLoc), end.In(parisLoc)
	return examScheduleView{
		Date:     begin.Format("02/01/2006"),
		Start:    begin.Format("15:04"),
		End:      end.Format("15:04"),
		Duration: fmt.Sprintf("%dh%02d", int(d.Hours()), int(d.Minutes())%60),
		Active:   !now.Before(begin) && !now.After(end),
	}
}

// examCountdownItem est une fenêtre d'exam transmise au client pour le
// décompte : libellés déjà formatés (heure de France) + instants RFC3339
// pour que le JS calcule le temps restant contre l'horloge du visiteur.
type examCountdownItem struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Date     string `json:"date"`
	Start    string `json:"start"`
	End      string `json:"end"`
	Duration string `json:"duration"`
	Begin    string `json:"begin"`  // RFC3339
	Finish   string `json:"finish"` // RFC3339
}

// examCountdownView alimente l'encadré « statut exam » : la liste complète des
// fenêtres connues (pour le JS) et l'exam mis en avant au rendu initial.
type examCountdownView struct {
	JSON  string
	Focus *examCountdownItem
	State string // "upcoming" | "active" | "done"
}

// buildExamCountdown choisit l'exam pertinent (en cours, sinon le prochain à
// venir, sinon — tous finis — le dernier) et sérialise toutes les fenêtres.
func buildExamCountdown(windows []pool.ExamWindowInfo) examCountdownView {
	if len(windows) == 0 {
		return examCountdownView{}
	}

	items := make([]examCountdownItem, len(windows))
	for i, w := range windows {
		sched := buildExamSchedule(w.Begin, w.End)
		items[i] = examCountdownItem{
			Key:      w.Key,
			Label:    pool.ExamLabels[w.Key],
			Date:     sched.Date,
			Start:    sched.Start,
			End:      sched.End,
			Duration: sched.Duration,
			Begin:    w.Begin.Format(time.RFC3339),
			Finish:   w.End.Format(time.RFC3339),
		}
	}

	now := time.Now()
	focus, state := len(windows)-1, "done"
	for i, w := range windows {
		if now.After(w.End) {
			continue // exam terminé, on regarde le suivant
		}
		if now.Before(w.Begin) {
			focus, state = i, "upcoming"
		} else {
			focus, state = i, "active"
		}
		break
	}

	view := examCountdownView{Focus: &items[focus], State: state}
	if raw, err := json.Marshal(items); err == nil {
		view.JSON = string(raw)
	}
	return view
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
			data["ExamCountdown"] = buildExamCountdown(h.pool.AllExamWindows())
			// « En cours » ne doit s'afficher que si l'exam est réellement dans
			// sa fenêtre : un piscineux inscrit tôt est "in_progress" côté API
			// bien avant le début.
			data["ExamActive"] = h.pool.ExamActive(exam)
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
