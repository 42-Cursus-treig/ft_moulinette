package api

import (
	"embed"
	"fmt"
	"html/template"
	"time"
)

//go:embed templates/*.html
var templateFS embed.FS

var templateFuncs = template.FuncMap{
	// medal renvoie la médaille du podium ou le rang (#04, #05, …).
	"medal": func(i int) string {
		switch i {
		case 0:
			return "🥇"
		case 1:
			return "🥈"
		case 2:
			return "🥉"
		default:
			return fmt.Sprintf("#%02d", i+1)
		}
	},
	// ranknum renvoie le rang sans médaille (#01, #02, …) - utilisé quand la
	// position ne récompense encore rien (ex. exam sans note).
	"ranknum": func(i int) string {
		return fmt.Sprintf("#%02d", i+1)
	},
	// frdt formate une date/heure en français court (dashboard : corrections,
	// défenses). Une date nulle donne « — ».
	"frdt": func(t time.Time) string {
		if t.IsZero() {
			return "—"
		}
		return t.Format("02/01 à 15h04")
	},
	// levelpct renvoie la partie fractionnaire d'un niveau 42 en pourcentage
	// (7.42 → 42) pour remplir la barre de progression du hero.
	"levelpct": func(level float64) int {
		frac := level - float64(int(level))
		return int(frac * 100)
	},
	// untilhuman renvoie un délai lisible avant t (« dans 2h13 », « maintenant »).
	"untilhuman": func(t time.Time) string {
		if t.IsZero() {
			return ""
		}
		d := time.Until(t)
		if d <= 0 {
			return "maintenant"
		}
		if d < time.Hour {
			return fmt.Sprintf("dans %dmin", int(d.Minutes()))
		}
		if d < 24*time.Hour {
			return fmt.Sprintf("dans %dh%02d", int(d.Hours()), int(d.Minutes())%60)
		}
		return fmt.Sprintf("dans %dj", int(d.Hours()/24))
	},
}

func loadTemplates() (*template.Template, error) {
	return template.New("").Funcs(templateFuncs).ParseFS(templateFS, "templates/*.html")
}
