package api

import (
	"embed"
	"html/template"
)

//go:embed templates/*.html
var templateFS embed.FS

func loadTemplates() (*template.Template, error) {
	return template.ParseFS(templateFS, "templates/*.html")
}
