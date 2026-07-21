package fortytwo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newMockService monte un serveur HTTP factice imitant l'API 42 et renvoie un
// Service qui le vise, plus le mux pour enregistrer des routes par test.
func newMockService(t *testing.T) (*Service, *http.ServeMux) {
	t.Helper()
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return NewService(srv.URL), mux
}

func mustAuth(t *testing.T, r *http.Request) {
	t.Helper()
	if got := r.Header.Get("Authorization"); got != "Bearer tok" {
		t.Fatalf("Authorization = %q, want Bearer tok", got)
	}
}

func TestProfile(t *testing.T) {
	svc, mux := newMockService(t)
	mux.HandleFunc("/v2/me", func(w http.ResponseWriter, r *http.Request) {
		mustAuth(t, r)
		w.Write([]byte(`{
			"id": 42, "login": "ppilo", "wallet": 300, "correction_point": 5,
			"location": "z1r3p4", "image": {"link": "http://img/ppilo.jpg"},
			"cursus_users": [
				{"level": 1.5, "grade": "Learner", "cursus": {"id": 9, "name": "C Piscine"}},
				{"level": 7.42, "grade": "Member", "cursus": {"id": 21, "name": "42cursus"}}
			]
		}`))
	})
	mux.HandleFunc("/v2/users/42/coalitions", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"id": 1, "name": "The Federation", "color": "#00b4d8"}]`))
	})

	p, err := svc.Profile(context.Background(), "tok")
	if err != nil {
		t.Fatal(err)
	}
	if p.Login != "ppilo" || p.ID != 42 {
		t.Fatalf("identité inattendue: %+v", p)
	}
	if p.Level != 7.42 || p.CursusName != "42cursus" {
		t.Fatalf("cursus principal mal choisi (attendu 42cursus/7.42): %+v", p)
	}
	if p.CoalitionName != "The Federation" || p.CoalitionColor != "#00b4d8" {
		t.Fatalf("coalition inattendue: %+v", p)
	}
	if p.CorrectionPoints != 5 || p.Wallet != 300 {
		t.Fatalf("stats inattendues: %+v", p)
	}
}

func TestProjectsCategorized(t *testing.T) {
	svc, mux := newMockService(t)
	mux.HandleFunc("/v2/me", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id": 42, "login": "ppilo"}`))
	})
	mux.HandleFunc("/v2/users/42/projects_users", func(w http.ResponseWriter, r *http.Request) {
		mustAuth(t, r)
		w.Write([]byte(`[
			{"status": "in_progress", "project": {"id": 1, "name": "libft", "slug": "libft"},
			 "teams": [{"repo_url": "git@vogsphere:ppilo/libft"}]},
			{"status": "finished", "final_mark": 100, "validated?": true,
			 "project": {"id": 2, "name": "get_next_line", "slug": "get_next_line"}}
		]`))
	})

	cats, err := svc.Projects(context.Background(), "tok")
	if err != nil {
		t.Fatal(err)
	}
	if len(cats) != 2 {
		t.Fatalf("attendu 2 catégories non vides, obtenu %d: %+v", len(cats), cats)
	}
	if cats[0].Name != "En cours" || cats[0].Items[0].RepoURL != "git@vogsphere:ppilo/libft" {
		t.Fatalf("catégorie en cours / repo inattendu: %+v", cats[0])
	}
	if cats[1].Name != "Terminés" || !cats[1].Items[0].Validated || cats[1].Items[0].FinalMark != 100 {
		t.Fatalf("catégorie terminés inattendue: %+v", cats[1])
	}
}

func TestCorrectionsReceived(t *testing.T) {
	svc, mux := newMockService(t)
	mux.HandleFunc("/v2/users/42/scale_teams", func(w http.ResponseWriter, r *http.Request) {
		mustAuth(t, r)
		// Une correction reçue (je suis dans correcteds) + une où JE corrige
		// (à ignorer ici).
		w.Write([]byte(`[
			{"id": 10, "final_mark": 84, "comment": "bien", "corrector": {"id": 7, "login": "revu"},
			 "correcteds": [{"id": 42, "login": "ppilo"}], "feedbacks": [],
			 "team": {"project": {"name": "ft_printf", "slug": "ft_printf"}}},
			{"id": 11, "corrector": {"id": 42, "login": "ppilo"},
			 "correcteds": [{"id": 8, "login": "autre"}], "team": {"project": {"name": "libft"}}}
		]`))
	})

	cs, err := svc.CorrectionsReceived(context.Background(), "tok", 42)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 1 {
		t.Fatalf("attendu 1 correction reçue, obtenu %d: %+v", len(cs), cs)
	}
	c := cs[0]
	if c.ProjectName != "ft_printf" || c.CorrectorLogin != "revu" || c.FinalMark != 84 || !c.Validated {
		t.Fatalf("correction inattendue: %+v", c)
	}
	if c.FeedbackSent {
		t.Fatalf("feedback ne devrait pas être marqué envoyé: %+v", c)
	}
}

func TestUpcomingDefenses(t *testing.T) {
	svc, mux := newMockService(t)
	mux.HandleFunc("/v2/users/42/scale_teams", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("filter[future]") != "true" {
			t.Fatalf("filter[future] absent: %s", r.URL.RawQuery)
		}
		w.Write([]byte(`[
			{"id": 20, "corrector": {"id": 42, "login": "ppilo"}, "begin_at": "2999-01-01T10:00:00.000Z",
			 "correcteds": [{"id": 8, "login": "acorriger"}],
			 "team": {"project": {"name": "Born2beroot", "slug": "born2beroot"}}}
		]`))
	})

	ds, err := svc.UpcomingDefenses(context.Background(), "tok", 42)
	if err != nil {
		t.Fatal(err)
	}
	if len(ds) != 1 {
		t.Fatalf("attendu 1 défense, obtenu %d: %+v", len(ds), ds)
	}
	d := ds[0]
	if d.ProjectName != "Born2beroot" || len(d.CorrectedLogins) != 1 || d.CorrectedLogins[0] != "acorriger" {
		t.Fatalf("défense inattendue: %+v", d)
	}
	if d.SubjectURL != "https://projects.intra.42.fr/projects/born2beroot" {
		t.Fatalf("lien sujet inattendu: %q", d.SubjectURL)
	}
	if d.Revealed {
		t.Fatalf("un créneau en 2999 ne devrait pas être dévoilé")
	}
}

func TestRegisterAndFeedbackWrite(t *testing.T) {
	svc, mux := newMockService(t)
	var gotRegister, gotFeedback bool
	mux.HandleFunc("/v2/projects_users", func(w http.ResponseWriter, r *http.Request) {
		mustAuth(t, r)
		if r.Method != http.MethodPost {
			t.Fatalf("méthode %s, attendu POST", r.Method)
		}
		gotRegister = true
		w.WriteHeader(http.StatusCreated)
	})
	mux.HandleFunc("/v2/feedbacks", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("méthode %s, attendu POST", r.Method)
		}
		gotFeedback = true
		w.WriteHeader(http.StatusCreated)
	})

	if err := svc.Register(context.Background(), "tok", 1, 42); err != nil {
		t.Fatal(err)
	}
	if err := svc.SendFeedback(context.Background(), "tok", 10, 5, "top"); err != nil {
		t.Fatal(err)
	}
	if !gotRegister || !gotFeedback {
		t.Fatalf("écritures non reçues: register=%v feedback=%v", gotRegister, gotFeedback)
	}
}
