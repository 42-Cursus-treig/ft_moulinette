package api

import (
	"fmt"
	"regexp"
	"strings"
)

// githubRepoPattern n'autorise que des dépôts GitHub publics en HTTPS -
// pas Vogsphere (VPN 42 requis, hors de portée pour l'instant), pas de
// git@ SSH (nécessiterait une clé côté serveur), et surtout pas de chemin
// local (voir normalizeRepoURL).
var githubRepoPattern = regexp.MustCompile(`^https://github\.com/[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+(\.git)?/?$`)

// normalizeRepoURL est l'unique porte d'entrée pour repo_url, que la
// soumission vienne du formulaire web ou de l'API JSON. Tolère les
// variantes de saisie courantes (schéma omis, http://, www.) en les
// ramenant à la forme canonique avant validation - sinon "github.com/x/y"
// (sans le "https://", un oubli fréquent) serait rejeté pour rien.
//
// sandbox.fetchSource accepte davantage en interne (chemins locaux,
// pratique en dev), mais ça ne doit jamais être atteignable depuis une
// requête HTTP réelle : sans cette validation, un repo_url comme
// "/etc/passwd" ou "/home/.../.env" serait traité comme un chemin local à
// copier tel quel dans le "dépôt" de l'élève - une vraie divulgation de
// fichiers du serveur.
func normalizeRepoURL(raw string) (string, error) {
	url := strings.TrimSpace(raw)
	if url == "" {
		return "", fmt.Errorf("repo_url est requis")
	}

	url = strings.TrimPrefix(url, "http://")
	url = strings.TrimPrefix(url, "https://")
	url = strings.TrimPrefix(url, "www.")
	url = "https://" + url

	if !githubRepoPattern.MatchString(url) {
		return "", fmt.Errorf("seuls les liens GitHub publics sont acceptés pour l'instant (https://github.com/<owner>/<repo>)")
	}
	return url, nil
}
