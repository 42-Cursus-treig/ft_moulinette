package main

import (
	"log"
	"os"
)

func env(name string, legacy ...string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	for _, l := range legacy {
		if v := os.Getenv(l); v != "" {
			log.Printf("%s est déprécié, utilise %s", l, name)
			return v
		}
	}
	return ""
}
