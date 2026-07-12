package main

import (
	"bufio"
	"os"
	"strings"
)

// loadDotEnv lit un fichier .env (format KEY=VALUE, une variable par ligne)
// et pose chaque variable dans l'environnement du processus - sauf si elle
// y est déjà définie, pour qu'un vrai export shell ou une variable posée
// par Docker garde toujours la priorité sur le fichier local.
// Absence du fichier = pas une erreur (cas normal en prod/CI).
func loadDotEnv(path string) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if _, alreadySet := os.LookupEnv(key); !alreadySet {
			os.Setenv(key, value)
		}
	}
	return scanner.Err()
}
