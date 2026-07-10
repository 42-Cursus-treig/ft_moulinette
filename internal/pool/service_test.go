package pool

import (
	"testing"
	"time"
)

func TestNightTTLAt(t *testing.T) {
	base := 10 * time.Minute
	day := time.Date(2026, 7, 10, 14, 0, 0, 0, time.Local)       // 14h : journée
	morning := time.Date(2026, 7, 10, 8, 0, 0, 0, time.Local)    // 8h pile : journée
	lateNight := time.Date(2026, 7, 10, 23, 0, 0, 0, time.Local) // 23h : nuit
	night := time.Date(2026, 7, 10, 3, 0, 0, 0, time.Local)      // 3h : nuit

	if got := nightTTLAt(base, day); got != base {
		t.Errorf("14h : TTL = %v, attendu %v", got, base)
	}
	if got := nightTTLAt(base, morning); got != base {
		t.Errorf("8h : TTL = %v, attendu %v", got, base)
	}
	if got := nightTTLAt(base, lateNight); got != time.Hour {
		t.Errorf("23h : TTL = %v, attendu 1h", got)
	}
	if got := nightTTLAt(base, night); got != time.Hour {
		t.Errorf("3h : TTL = %v, attendu 1h", got)
	}
	// Un TTL déjà long (ex. examIdleTTL) ne doit pas être raccourci ni changé.
	if got := nightTTLAt(2*time.Hour, night); got != 2*time.Hour {
		t.Errorf("TTL long la nuit : %v, attendu 2h", got)
	}
}

func TestBetterWindow(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	mk := func(beginOffset, endOffset time.Duration) examWindow {
		return examWindow{Begin: now.Add(beginOffset), End: now.Add(endOffset)}
	}

	ended := mk(-6*time.Hour, -2*time.Hour)
	endedRecent := mk(-4*time.Hour, -1*time.Hour)
	upcoming := mk(2*time.Hour, 6*time.Hour)
	upcomingLater := mk(10*time.Hour, 14*time.Hour)
	ongoing := mk(-1*time.Hour, 1*time.Hour)

	// Une fenêtre non terminée bat une terminée (et pas l'inverse).
	if !betterWindow(ended, upcoming, now) {
		t.Error("à venir doit remplacer terminé")
	}
	if betterWindow(upcoming, ended, now) {
		t.Error("terminé ne doit pas remplacer à venir")
	}
	// Entre deux non terminées : la plus proche (Begin le plus tôt).
	if !betterWindow(upcomingLater, upcoming, now) {
		t.Error("la fenêtre la plus proche doit gagner")
	}
	if betterWindow(upcoming, upcomingLater, now) {
		t.Error("une fenêtre plus lointaine ne doit pas remplacer la plus proche")
	}
	// En cours compte comme non terminée.
	if !betterWindow(ended, ongoing, now) {
		t.Error("en cours doit remplacer terminé")
	}
	// Entre deux terminées : la plus récente.
	if !betterWindow(ended, endedRecent, now) {
		t.Error("la fenêtre terminée la plus récente doit gagner")
	}
	if betterWindow(endedRecent, ended, now) {
		t.Error("une fenêtre terminée plus ancienne ne doit pas remplacer la récente")
	}
}
