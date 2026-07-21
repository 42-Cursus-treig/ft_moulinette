package api

import (
	"embed"
	"fmt"
	"html/template"
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
}

func loadTemplates() (*template.Template, error) {
	return template.New("").Funcs(templateFuncs).ParseFS(templateFS, "templates/*.html")
}
