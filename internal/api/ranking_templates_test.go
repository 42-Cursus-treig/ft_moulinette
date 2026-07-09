package api

import (
	"strings"
	"testing"
	"time"

	"github.com/tristan-reig/ft-moulinette/internal/auth"
	"github.com/tristan-reig/ft-moulinette/internal/pool"
)

func contentData(tab, state string) map[string]any {
	scoreRows := []pool.ScoreRow{
		{Login: "alice", Level: 4.2, Score: 1200, Coalition: "Stack", Color: "#00babc"},
		{Login: "bob", Level: 3.1, Score: 900, Coalition: "Heap", Color: "#f1c40f"},
	}
	pills, _ := filterCoalition(scoreRows, "")
	snaps := []pool.Snapshot{
		{Date: "2026-07-07", At: time.Now().Add(-24 * time.Hour), Rows: scoreRows},
		{Date: "2026-07-08", At: time.Now(), Rows: scoreRows},
	}

	return map[string]any{
		"Tab":        tab,
		"Exam":       "00",
		"ExamLabel":  "Exam 00",
		"MonthLabel": "Juillet",
		"Year":       2026,
		"State":      state,
		"Err":        "boom",
		"UpdatedAt":  time.Now(),
		"SelfURL":    "/ui/classement?tab=" + tab,
		"RefreshSec": 60,
		"ScoreRows":  scoreRows,
		"Coalitions": pills,
		"ProjectRows": []pool.ProjectRow{
			{Login: "alice", Level: 4.2, Shell: 2, C: 5, Exam: 1, Rush: 1, Total: 9},
		},
		"ExamRows": []pool.ExamRow{
			{Login: "alice", Mark: 80, HasMark: true, Status: "finished", Validated: true},
			{Login: "bob", Status: "in_progress"},
		},
		"Charts":        buildProgressCharts(snaps, "alice"),
		"SnapshotCount": len(snaps),
	}
}

// TestRankingTemplates exécute les templates de la page Classement dans tous
// les états : les erreurs de champ dans html/template ne sortent qu'au rendu.
func TestRankingTemplates(t *testing.T) {
	tmpl, err := loadTemplates()
	if err != nil {
		t.Fatalf("chargement des templates: %v", err)
	}

	pageData := map[string]any{
		"User":    auth.User{Login: "alice"},
		"IsAdmin": false,
		"Exams":   examTabs,
	}
	var page strings.Builder
	if err := tmpl.ExecuteTemplate(&page, "ranking", pageData); err != nil {
		t.Fatalf("rendu de la page ranking: %v", err)
	}

	for _, tab := range []string{"score", "projects", "exam", "progress"} {
		for _, state := range []string{"loading", "unavailable", "error", "ready"} {
			var out strings.Builder
			if err := tmpl.ExecuteTemplate(&out, "ranking_content", contentData(tab, state)); err != nil {
				t.Fatalf("rendu du fragment tab=%s state=%s : %v", tab, state, err)
			}
			if state != "ready" {
				continue
			}
			if tab == "progress" {
				if !strings.Contains(out.String(), "polyline") {
					t.Errorf("tab=progress state=ready : aucune courbe dans le rendu")
				}
				if strings.Contains(out.String(), "ZgotmplZ") {
					t.Errorf("tab=progress : couleur rejetée par html/template (ZgotmplZ)")
				}
			} else if !strings.Contains(out.String(), "alice") {
				t.Errorf("tab=%s state=ready : la ligne du classement est absente", tab)
			}
		}
	}
}

func TestFilterCoalition(t *testing.T) {
	rows := []pool.ScoreRow{
		{Login: "alice", Coalition: "Stack", Color: "#00babc"},
		{Login: "bob", Coalition: "Heap"},
		{Login: "carol", Coalition: "Stack"},
	}

	pills, filtered := filterCoalition(rows, "")
	if len(pills) != 2 || len(filtered) != 3 {
		t.Fatalf("sans filtre : %d pills, %d lignes (attendu 2 et 3)", len(pills), len(filtered))
	}

	pills, filtered = filterCoalition(rows, "Stack")
	if len(filtered) != 2 {
		t.Errorf("filtre Stack : %d lignes (attendu 2)", len(filtered))
	}
	if !pills[0].Active || pills[1].Active {
		t.Errorf("filtre Stack : pill active incorrecte")
	}

	// Coalition inconnue : on ignore le filtre.
	_, filtered = filterCoalition(rows, "Inexistante")
	if len(filtered) != 3 {
		t.Errorf("filtre inconnu : %d lignes (attendu 3)", len(filtered))
	}
}

func TestBuildProgressCharts(t *testing.T) {
	if got := buildProgressCharts(nil, "alice"); got != nil {
		t.Errorf("aucun relevé : attendu nil, obtenu %d charts", len(got))
	}

	snaps := []pool.Snapshot{
		{Date: "2026-07-07", Rows: []pool.ScoreRow{
			{Login: "alice", Level: 1.0, Score: 100, Coalition: "Stack"},
			{Login: "bob", Level: 2.0, Score: 200, Coalition: "Heap"},
		}},
		{Date: "2026-07-08", Rows: []pool.ScoreRow{
			{Login: "alice", Level: 1.5, Score: 150, Coalition: "Stack"},
			{Login: "bob", Level: 2.5, Score: 250, Coalition: "Heap"},
		}},
	}
	charts := buildProgressCharts(snaps, "alice")
	if len(charts) != 2 {
		t.Fatalf("attendu 2 charts, obtenu %d", len(charts))
	}
	// Niveau : Stack + Heap + la courbe de l'utilisateur.
	if len(charts[0].Series) != 3 {
		t.Errorf("chart niveau : %d séries (attendu 3 avec la courbe utilisateur)", len(charts[0].Series))
	}
	if len(charts[1].Series) != 2 {
		t.Errorf("chart score : %d séries (attendu 2)", len(charts[1].Series))
	}
	for _, s := range charts[0].Series {
		if s.Polyline == "" {
			t.Errorf("chart niveau : série %q sans polyline malgré 2 relevés", s.Label)
		}
		if s.Color == "" {
			t.Errorf("chart niveau : série %q sans couleur (fallback attendu)", s.Label)
		}
	}
}

func TestBuildExamScheduleParisTZ(t *testing.T) {
	// Exam 12:00→16:00 UTC un jour de juillet : la France est en heure d'été
	// (CEST, UTC+2), donc l'affichage doit être 14:00→18:00, quelle que soit
	// la timezone du serveur.
	begin := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	end := time.Date(2026, 7, 10, 16, 0, 0, 0, time.UTC)

	v := buildExamSchedule(begin, end)
	if v.Date != "10/07/2026" {
		t.Errorf("Date = %q, attendu 10/07/2026", v.Date)
	}
	if v.Start != "14:00" || v.End != "18:00" {
		t.Errorf("horaire = %s → %s, attendu 14:00 → 18:00 (heure de France)", v.Start, v.End)
	}
	if v.Duration != "4h00" {
		t.Errorf("Duration = %q, attendu 4h00", v.Duration)
	}

	// Un exam d'hiver (janvier) : CET = UTC+1, donc 12:00 UTC → 13:00.
	winter := buildExamSchedule(
		time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC),
		time.Date(2026, 1, 15, 16, 0, 0, 0, time.UTC),
	)
	if winter.Start != "13:00" {
		t.Errorf("hiver : Start = %q, attendu 13:00 (CET)", winter.Start)
	}
}

func TestCurrentPoolSession(t *testing.T) {
	cases := []struct {
		now       time.Time
		wantMonth string
		wantYear  int
		wantOK    bool
	}{
		{time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC), "july", 2026, true},
		{time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC), "august", 2026, true},
		{time.Date(2027, 9, 30, 0, 0, 0, 0, time.UTC), "september", 2027, true},
		{time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC), "", 0, false},
		{time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), "", 0, false},
		{time.Date(2027, 1, 15, 0, 0, 0, 0, time.UTC), "", 0, false},
	}
	for _, c := range cases {
		month, year, ok := pool.CurrentSession(c.now)
		if month != c.wantMonth || year != c.wantYear || ok != c.wantOK {
			t.Errorf("CurrentSession(%s) = %q %d %v, attendu %q %d %v",
				c.now.Format("2006-01-02"), month, year, ok, c.wantMonth, c.wantYear, c.wantOK)
		}
	}
}
