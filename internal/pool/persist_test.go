package pool

import (
	"path/filepath"
	"testing"
	"time"
)

func TestHistoryRecordAndReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")

	h := newHistoryStore(path)
	rows := []ScoreRow{{Login: "alice", Level: 2.5, Score: 500, Coalition: "Stack"}}
	h.record("july-2026", rows)
	h.record("july-2026", rows) // même jour : remplace, n'ajoute pas

	if got := h.series("july-2026"); len(got) != 1 {
		t.Fatalf("attendu 1 relevé après deux record le même jour, obtenu %d", len(got))
	}

	// Rechargement depuis le disque.
	h2 := newHistoryStore(path)
	got := h2.series("july-2026")
	if len(got) != 1 || len(got[0].Rows) != 1 || got[0].Rows[0].Login != "alice" {
		t.Fatalf("relevés mal rechargés depuis le disque : %+v", got)
	}
}

func TestCacheSaveAndReload(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "cache.json")

	s := NewService("id", "secret", "Perpignan", cachePath, "")
	s.mu.Lock()
	s.campusID = 42
	sess := s.sessionLocked("july", "2026")
	sess.roster = entry[[]Pooler]{data: []Pooler{{ID: 1, Login: "alice", Level: 3.0}}, has: true, at: time.Now()}
	sess.score = entry[[]ScoreRow]{data: []ScoreRow{{Login: "alice", Score: 100, Coalition: "Stack", Color: "#00babc"}}, has: true, at: time.Now()}
	sess.exams["00"] = &entry[[]ExamRow]{data: []ExamRow{{Login: "alice", Mark: 50, HasMark: true}}, has: true, at: time.Now()}
	s.saveCacheLocked()
	s.mu.Unlock()

	// Un nouveau service (même chemin) doit resservir ces données sans fetch :
	// c'est ce qui évite l'écran de chargement après redémarrage.
	s2 := NewService("id", "secret", "Perpignan", cachePath, "")
	s2.mu.Lock()
	defer s2.mu.Unlock()

	if s2.campusID != 42 {
		t.Errorf("campusID non restauré : %d", s2.campusID)
	}
	sess2, ok := s2.sessions["july-2026"]
	if !ok {
		t.Fatal("session july-2026 absente après rechargement")
	}
	if !sess2.roster.has || len(sess2.roster.data) != 1 || sess2.roster.data[0].Login != "alice" {
		t.Errorf("roster non restauré : %+v", sess2.roster)
	}
	if !sess2.score.has || sess2.score.data[0].Color != "#00babc" {
		t.Errorf("score non restauré : %+v", sess2.score)
	}
	if e := sess2.exams["00"]; e == nil || !e.has || !e.data[0].HasMark {
		t.Errorf("exam 00 non restauré")
	}
}

func TestNormalizeHexColor(t *testing.T) {
	cases := map[string]string{
		"#00babc":  "#00babc",
		"#FFF":     "#FFF",
		"":         "",
		"red":      "",
		"#zzzzzz":  "",
		"#00babc;": "",
	}
	for in, want := range cases {
		if got := normalizeHexColor(in); got != want {
			t.Errorf("normalizeHexColor(%q) = %q, attendu %q", in, got, want)
		}
	}
}
