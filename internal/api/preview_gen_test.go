package api

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tristan-reig/ft-moulinette/internal/auth"
	"github.com/tristan-reig/ft-moulinette/internal/pool"
)

// TestGeneratePreview écrit un rendu statique interactif de l'onglet
// Progression. Activé seulement si RANKING_PREVIEW_OUT est défini.
func TestGeneratePreview(t *testing.T) {
	outDir := os.Getenv("RANKING_PREVIEW_OUT")
	if outDir == "" {
		t.Skip("RANKING_PREVIEW_OUT non défini")
	}

	tmpl, err := loadTemplates()
	if err != nil {
		t.Fatal(err)
	}

	pageData := map[string]any{
		"User":    auth.User{Login: "treig"},
		"IsAdmin": false,
		"Exams":   examTabs,
	}
	var page strings.Builder
	if err := tmpl.ExecuteTemplate(&page, "ranking", pageData); err != nil {
		t.Fatal(err)
	}

	var snaps []pool.Snapshot
	base := time.Date(2026, 7, 1, 20, 0, 0, 0, time.Local)
	for i := 0; i < 8; i++ {
		day := base.AddDate(0, 0, i)
		f := float64(i)
		snaps = append(snaps, pool.Snapshot{
			Date: day.Format("2006-01-02"),
			At:   day,
			Rows: []pool.ScoreRow{
				{Login: "alice", Level: 1 + f*0.9, Score: 300 + int(f*640), Coalition: "La Stack", Color: "#00babc"},
				{Login: "bob", Level: 0.8 + f*0.85, Score: 250 + int(f*550), Coalition: "La Heap", Color: "#f1c40f"},
				{Login: "treig", Level: 0.5 + f*0.52, Score: 120 + int(f*245), Coalition: "La Stack", Color: "#00babc"},
			},
		})
	}

	data := map[string]any{
		"Tab": "progress", "Exam": "00", "ExamLabel": "Exam 00",
		"MonthLabel": "Juillet", "Year": 2026,
		"State": "ready", "Err": "", "UpdatedAt": time.Now(),
		"SelfURL": "#", "RefreshSec": 600,
		"ScoreRows": []pool.ScoreRow{}, "Coalitions": []coalitionPill(nil),
		"ProjectRows": []pool.ProjectRow{}, "ExamRows": []pool.ExamRow{},
		"Charts": buildProgressCharts(snaps, "treig"), "SnapshotCount": len(snaps),
	}

	var frag strings.Builder
	if err := tmpl.ExecuteTemplate(&frag, "ranking_content", data); err != nil {
		t.Fatal(err)
	}
	html := page.String()
	start := strings.Index(html, `<div id="ranking-content">`)
	end := strings.Index(html, "</body>")
	// Garde ranking_chart.js (retire seulement ranking.js qui charge via htmx).
	html = html[:start] + `<div id="ranking-content">` + frag.String() + "</div>\n" + html[end:]
	html = strings.ReplaceAll(html, `<script src="/static/ranking.js" defer></script>`, "")
	html = strings.ReplaceAll(html, `/static/`, "static/")
	if err := os.WriteFile(filepath.Join(outDir, "progress.html"), []byte(html), 0o644); err != nil {
		t.Fatal(err)
	}
}
