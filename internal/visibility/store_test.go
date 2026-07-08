package visibility

import (
	"path/filepath"
	"testing"
)

func TestStoreDefaultsVisible(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "visibility.json"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for _, sec := range Sections {
		if !s.Visible(sec.Key) {
			t.Errorf("section %q devrait être visible par défaut", sec.Key)
		}
	}
}

func TestStoreSetAndPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "visibility.json")
	s, err := New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := s.Set(SectionMoulinette, false); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if s.Visible(SectionMoulinette) {
		t.Errorf("Moulinette devrait être masquée après Set(false)")
	}
	if !s.Visible(SectionClassement) {
		t.Errorf("Classement devrait rester visible")
	}

	// Rechargement depuis le disque : l'état masqué doit persister.
	reloaded, err := New(path)
	if err != nil {
		t.Fatalf("New (reload): %v", err)
	}
	if reloaded.Visible(SectionMoulinette) {
		t.Errorf("Moulinette devrait rester masquée après rechargement")
	}
	all := reloaded.All()
	if all[SectionMoulinette] || !all[SectionClassement] || !all[SectionHistory] {
		t.Errorf("All() incohérent après rechargement: %v", all)
	}

	// Réactivation.
	if err := reloaded.Set(SectionMoulinette, true); err != nil {
		t.Fatalf("Set(true): %v", err)
	}
	if !reloaded.Visible(SectionMoulinette) {
		t.Errorf("Moulinette devrait être visible après réactivation")
	}
}

func TestStoreSetUnknownSection(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "visibility.json"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := s.Set("inexistante", false); err == nil {
		t.Errorf("Set d'une section inconnue devrait échouer")
	}
}
