package pool

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// La persistance du cache évite l'écran « Récupération des données… » après
// un redémarrage : on ressert les dernières données connues immédiatement,
// et les TTL habituels déclenchent le rafraîchissement en arrière-plan.

type persistedEntry[T any] struct {
	Data T         `json:"data"`
	At   time.Time `json:"at"`
}

type persistedSession struct {
	Roster   *persistedEntry[[]Pooler]             `json:"roster,omitempty"`
	Score    *persistedEntry[[]ScoreRow]           `json:"score,omitempty"`
	Projects *persistedEntry[[]ProjectRow]         `json:"projects,omitempty"`
	Exams    map[string]*persistedEntry[[]ExamRow] `json:"exams,omitempty"`
}

type persistedCache struct {
	CampusID   int                          `json:"campus_id,omitempty"`
	Coalitions []coalitionJSON              `json:"coalitions,omitempty"`
	ProjectIDs map[string]int               `json:"project_ids,omitempty"`
	Sessions   map[string]*persistedSession `json:"sessions"`
}

func persistEntry[T any](e *entry[T]) *persistedEntry[T] {
	if !e.has {
		return nil
	}
	return &persistedEntry[T]{Data: e.data, At: e.at}
}

func restoreEntry[T any](p *persistedEntry[T], e *entry[T]) {
	if p == nil {
		return
	}
	e.data, e.has, e.at = p.Data, true, p.At
}

// saveCacheLocked écrit l'état du cache sur disque. s.mu doit être tenu.
func (s *Service) saveCacheLocked() {
	if s.cachePath == "" {
		return
	}

	pc := persistedCache{
		CampusID:   s.campusID,
		Coalitions: s.coalitions,
		ProjectIDs: s.projectIDs,
		Sessions:   make(map[string]*persistedSession, len(s.sessions)),
	}
	for key, sess := range s.sessions {
		ps := &persistedSession{
			Roster:   persistEntry(&sess.roster),
			Score:    persistEntry(&sess.score),
			Projects: persistEntry(&sess.projects),
		}
		for examKey, e := range sess.exams {
			if p := persistEntry(e); p != nil {
				if ps.Exams == nil {
					ps.Exams = make(map[string]*persistedEntry[[]ExamRow])
				}
				ps.Exams[examKey] = p
			}
		}
		pc.Sessions[key] = ps
	}

	data, err := json.Marshal(pc)
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(s.cachePath), 0o755)
	tmp := s.cachePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, s.cachePath)
}

// loadCache recharge le cache écrit par saveCacheLocked. Toute erreur est
// ignorée : on repart simplement d'un cache vide.
func (s *Service) loadCache() {
	if s.cachePath == "" {
		return
	}
	data, err := os.ReadFile(s.cachePath)
	if err != nil {
		return
	}
	var pc persistedCache
	if err := json.Unmarshal(data, &pc); err != nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.campusID = pc.CampusID
	s.coalitions = pc.Coalitions
	if pc.ProjectIDs != nil {
		s.projectIDs = pc.ProjectIDs
	}
	for key, ps := range pc.Sessions {
		sess := &session{exams: make(map[string]*entry[[]ExamRow])}
		restoreEntry(ps.Roster, &sess.roster)
		restoreEntry(ps.Score, &sess.score)
		restoreEntry(ps.Projects, &sess.projects)
		for examKey, p := range ps.Exams {
			e := &entry[[]ExamRow]{}
			restoreEntry(p, e)
			sess.exams[examKey] = e
		}
		s.sessions[key] = sess
	}
}
