package api

import (
	"strings"
	"testing"
	"time"

	"github.com/tristan-reig/ft-moulinette/internal/auth"
	"github.com/tristan-reig/ft-moulinette/internal/fortytwo"
)

// TestDashboardTemplateLight rend le dashboard sans jeton 42 (Live=false) :
// on doit obtenir l'invite de reconnexion, pas les briques.
func TestDashboardTemplateLight(t *testing.T) {
	tmpl, err := loadTemplates()
	if err != nil {
		t.Fatalf("chargement des templates: %v", err)
	}
	data := map[string]any{
		"User": auth.User{Login: "alice"}, "Page": "dashboard",
		"NavDashboard": true, "Live": false,
	}
	var out strings.Builder
	if err := tmpl.ExecuteTemplate(&out, "dashboard", data); err != nil {
		t.Fatalf("rendu dashboard light: %v", err)
	}
	s := out.String()
	if !strings.Contains(s, "Connexion 42 requise") {
		t.Errorf("dashboard light : invite de reconnexion manquante")
	}
	if !strings.Contains(s, `href="/auth/login"`) {
		t.Errorf("dashboard light : bouton de reconnexion manquant")
	}
}

// TestDashboardTemplateLive rend le dashboard complet avec les 4 briques et
// vérifie que chaque donnée représentative apparaît sans erreur de champ.
func TestDashboardTemplateLive(t *testing.T) {
	tmpl, err := loadTemplates()
	if err != nil {
		t.Fatalf("chargement des templates: %v", err)
	}
	data := map[string]any{
		"User": auth.User{Login: "alice"}, "Page": "dashboard",
		"NavDashboard": true, "Live": true,
		"Profile": fortytwo.Profile{
			Login: "alice", CursusName: "42cursus", Level: 7.42, Grade: "Member",
			Wallet: 300, CorrectionPoints: 5, CoalitionName: "Federation",
			CoalitionColor: "#00b4d8", HasBlackHole: true, BlackHoleIn: 12,
		},
		"ProjectCats": []fortytwo.ProjectCategory{
			{Name: "En cours", Items: []fortytwo.ProjectItem{
				{ProjectID: 1, Name: "libft", Status: "in_progress", Registered: true, RepoURL: "git@vogsphere:alice/libft", SubjectURL: "https://x/libft"},
			}},
			{Name: "Disponibles", Items: []fortytwo.ProjectItem{
				{ProjectID: 2, Name: "ft_printf", SubjectURL: "https://x/ft_printf"},
			}},
		},
		"Corrections": []fortytwo.Correction{
			{ScaleTeamID: 10, ProjectName: "get_next_line", CorrectorLogin: "bob",
				FinalMark: 84, HasMark: true, Validated: true, Comment: "clean", When: time.Now()},
			{ScaleTeamID: 11, ProjectName: "libft", CorrectorLogin: "carol", FeedbackSent: true},
		},
		"Defenses": []fortytwo.Defense{
			{ScaleTeamID: 20, ProjectName: "Born2beroot", BeginAt: time.Now().Add(2 * time.Hour),
				SubjectURL: "https://x/born2beroot", Revealed: false},
		},
	}
	var out strings.Builder
	if err := tmpl.ExecuteTemplate(&out, "dashboard", data); err != nil {
		t.Fatalf("rendu dashboard live: %v", err)
	}
	s := out.String()
	for _, want := range []string{
		"alice", "42cursus", "niveau 7.42", "Federation", // hero
		"libft", "Disponibles", "data-repo=", "S'inscrire", // projets
		"get_next_line", "84/100", "Feedback envoyé", // corrections (bob non-envoyé -> form, carol envoyé)
		"Born2beroot", "dévoilé 15 min avant", // défenses
	} {
		if !strings.Contains(s, want) {
			t.Errorf("dashboard live : fragment attendu absent: %q", want)
		}
	}
	// La correction de bob (non envoyée) doit exposer le formulaire de feedback.
	if !strings.Contains(s, `hx-post="/ui/corrections/feedback"`) {
		t.Errorf("dashboard live : formulaire de feedback manquant pour une correction non notée")
	}
}

// TestDashboardFragments rend les petits fragments htmx renvoyés par les
// actions d'écriture (inscription / feedback).
func TestDashboardFragments(t *testing.T) {
	tmpl, err := loadTemplates()
	if err != nil {
		t.Fatalf("chargement des templates: %v", err)
	}
	cases := []struct {
		name, tmplName, want string
		data                 any
	}{
		{"register_ok", "register_ok", "Inscrit à libft", map[string]any{"Name": "libft"}},
		{"register_error", "register_error", "refusée", map[string]any{"Msg": "inscription refusée par 42"}},
		{"feedback_done", "feedback_done", "Feedback envoyé", nil},
		{"feedback_error", "feedback_error", "indispo", map[string]any{"Msg": "indispo"}},
	}
	for _, c := range cases {
		var out strings.Builder
		if err := tmpl.ExecuteTemplate(&out, c.tmplName, c.data); err != nil {
			t.Fatalf("rendu %s: %v", c.name, err)
		}
		if !strings.Contains(out.String(), c.want) {
			t.Errorf("%s : attendu %q dans %q", c.name, c.want, out.String())
		}
	}
}
