package api

import (
	"strings"
	"testing"

	"github.com/tristan-reig/ft-moulinette/internal/auth"
	"github.com/tristan-reig/ft-moulinette/internal/visibility"
)

// navData reproduit les drapeaux de navigation injectés par navFlags, plus le
// contexte minimal commun aux pages.
func navData(isAdmin, moul, classement, history bool, page string) map[string]any {
	return map[string]any{
		"User":          auth.User{Login: "alice"},
		"IsAdmin":       isAdmin,
		"NavMoulinette": moul,
		"NavClassement": classement,
		"NavHistory":    history,
		"Page":          page,
	}
}

// TestPageTemplatesVisibility rend chaque page membre avec le partiel d'en-tête
// et vérifie que les liens de section apparaissent/disparaissent selon les
// drapeaux - les erreurs de champ html/template ne sortent qu'au rendu.
func TestPageTemplatesVisibility(t *testing.T) {
	tmpl, err := loadTemplates()
	if err != nil {
		t.Fatalf("chargement des templates: %v", err)
	}

	// Membre : Moulinette masquée, Classement visible, Historique masqué.
	member := navData(false, false, true, false, "")
	var disabled strings.Builder
	if err := tmpl.ExecuteTemplate(&disabled, "disabled", member); err != nil {
		t.Fatalf("rendu page disabled: %v", err)
	}
	out := disabled.String()
	if !strings.Contains(out, "Section désactivée") {
		t.Errorf("page disabled : message manquant")
	}
	if !strings.Contains(out, `href="/classement"`) {
		t.Errorf("page disabled : lien classement attendu (section visible)")
	}
	if strings.Contains(out, `href="/history"`) {
		t.Errorf("page disabled : lien historique présent alors que masqué")
	}
	if strings.Contains(out, `href="/admin"`) {
		t.Errorf("page disabled : lien admin présent pour un non-admin")
	}

	// index / history exécutent aussi le partiel : on vérifie juste l'absence
	// d'erreur de rendu avec des données représentatives.
	idx := navData(true, true, true, true, "moulinette")
	idx["Exercises"] = nil
	idx["Jobs"] = nil
	if err := tmpl.ExecuteTemplate(&strings.Builder{}, "index", idx); err != nil {
		t.Fatalf("rendu page index: %v", err)
	}

	hist := navData(false, true, false, true, "history")
	hist["Jobs"] = nil
	if err := tmpl.ExecuteTemplate(&strings.Builder{}, "history", hist); err != nil {
		t.Fatalf("rendu page history: %v", err)
	}
}

// TestAdminTemplateVisibility rend le panel admin avec les interrupteurs de
// visibilité (états mixtes).
func TestAdminTemplateVisibility(t *testing.T) {
	tmpl, err := loadTemplates()
	if err != nil {
		t.Fatalf("chargement des templates: %v", err)
	}

	data := map[string]any{
		"User":      auth.User{Login: "treig"},
		"Exercises": nil,
		"Sections":  visibility.Sections,
		"Visibility": map[string]bool{
			visibility.SectionMoulinette: true,
			visibility.SectionClassement: false,
			visibility.SectionHistory:    true,
		},
	}
	var out strings.Builder
	if err := tmpl.ExecuteTemplate(&out, "admin", data); err != nil {
		t.Fatalf("rendu page admin: %v", err)
	}
	s := out.String()
	if !strings.Contains(s, "Visible") || !strings.Contains(s, "Masqué") {
		t.Errorf("panel admin : badges de visibilité manquants")
	}
	if !strings.Contains(s, `action="/admin/visibility"`) {
		t.Errorf("panel admin : formulaire de visibilité manquant")
	}
}
