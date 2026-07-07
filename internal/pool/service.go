package pool

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// poolCursusID est le cursus "C Piscine" côté API 42.
const poolCursusID = 9

// TTL des caches : mêmes ordres de grandeur que le bot Discord
// (boards toutes les 10 min, exams toutes les 60 s).
const (
	rosterTTL   = 10 * time.Minute
	scoreTTL    = 10 * time.Minute
	projectsTTL = 15 * time.Minute
	examTTL     = 60 * time.Second
	errRetry    = 45 * time.Second // délai avant de retenter après un échec
)

// ExamSlugs relie les clés d'exam de l'UI aux slugs de projets intra.
var ExamSlugs = map[string]string{
	"00":    "c-piscine-exam-00",
	"01":    "c-piscine-exam-01",
	"02":    "c-piscine-exam-02",
	"final": "c-piscine-final-exam",
}

// ExamLabels donne les libellés affichés pour chaque exam.
var ExamLabels = map[string]string{
	"00":    "Exam 00",
	"01":    "Exam 01",
	"02":    "Exam 02",
	"final": "Exam Final",
}

// State décrit l'état d'une section de classement pour une session donnée.
type State string

const (
	StateLoading     State = "loading"     // données en cours de récupération
	StateReady       State = "ready"       // données disponibles
	StateUnavailable State = "unavailable" // aucune piscine pour cette session
	StateError       State = "error"       // dernière récupération en échec
)

// Status accompagne chaque snapshot de données.
type Status struct {
	State     State
	UpdatedAt time.Time
	Err       string
}

// Pooler est un piscineux du roster d'une session.
type Pooler struct {
	ID    int
	Login string
	Level float64
}

// ScoreRow est une ligne du classement par score de coalition.
type ScoreRow struct {
	Login     string  `json:"login"`
	Level     float64 `json:"level"`
	Score     int     `json:"score"`
	Coalition string  `json:"coalition"`
	Color     string  `json:"color,omitempty"` // hex validé, ou vide
}

// ProjectRow est une ligne du classement par projets validés.
type ProjectRow struct {
	Login string
	Level float64
	Shell int
	C     int
	Exam  int
	Rush  int
	Total int
}

// ExamRow est une ligne du classement en direct d'un exam.
type ExamRow struct {
	Login     string
	Mark      int
	HasMark   bool
	Status    string
	Validated bool
}

// entry est une valeur mise en cache, rafraîchie en arrière-plan : on sert le
// contenu existant (même périmé) pendant qu'un fetch asynchrone le renouvelle.
type entry[T any] struct {
	data     T
	has      bool
	at       time.Time
	fetching bool
	err      error
	errAt    time.Time
}

// session regroupe les caches d'une piscine (mois + année).
type session struct {
	roster   entry[[]Pooler]
	score    entry[[]ScoreRow]
	projects entry[[]ProjectRow]
	exams    map[string]*entry[[]ExamRow]
}

// Service expose les classements de piscine au reste du serveur, avec cache
// en mémoire (persisté sur disque) et récupération asynchrone depuis l'API 42.
type Service struct {
	c          *client
	campusName string
	cachePath  string
	history    *historyStore

	mu         sync.Mutex
	campusID   int
	coalitions []coalitionJSON
	projectIDs map[string]int
	sessions   map[string]*session
}

// NewService construit le service ; id/secret sont les credentials OAuth de
// l'application 42 (flux client_credentials). cachePath et historyPath sont
// les fichiers de persistance (cache des classements, relevés de progression) ;
// vides pour désactiver.
func NewService(clientID, clientSecret, campusName, cachePath, historyPath string) *Service {
	s := &Service{
		c:          newClient(clientID, clientSecret),
		campusName: campusName,
		cachePath:  cachePath,
		history:    newHistoryStore(historyPath),
		projectIDs: make(map[string]int),
		sessions:   make(map[string]*session),
	}
	s.loadCache()
	return s
}

// CurrentSession renvoie la piscine en cours (les piscines ont lieu en
// juillet, août ou septembre). ok=false hors saison.
func CurrentSession(now time.Time) (month string, year int, ok bool) {
	switch now.Month() {
	case time.July:
		return "july", now.Year(), true
	case time.August:
		return "august", now.Year(), true
	case time.September:
		return "september", now.Year(), true
	default:
		return "", 0, false
	}
}

// StartDailySnapshots garantit un relevé de progression quotidien même sans
// visite sur le site : toutes les heures, si une piscine est en cours, le
// classement score est rafraîchi (ce qui enregistre le relevé du jour).
func (s *Service) StartDailySnapshots() {
	go func() {
		for {
			if month, year, ok := CurrentSession(time.Now()); ok {
				s.Score(month, strconv.Itoa(year))
			}
			time.Sleep(time.Hour)
		}
	}()
}

func (s *Service) sessionLocked(month, year string) *session {
	key := month + "-" + year
	sess, ok := s.sessions[key]
	if !ok {
		sess = &session{exams: make(map[string]*entry[[]ExamRow])}
		s.sessions[key] = sess
	}
	return sess
}

// resolveLocked applique la politique de cache d'une entry : déclenche un
// fetch asynchrone si nécessaire et renvoie l'état courant. s.mu doit être
// tenu par l'appelant.
func resolveLocked[T any](s *Service, e *entry[T], ttl time.Duration, fetch func() (T, error)) (T, Status) {
	now := time.Now()
	stale := !e.has || now.Sub(e.at) > ttl
	retryOK := e.err == nil || now.Sub(e.errAt) > errRetry

	if stale && !e.fetching && retryOK {
		e.fetching = true
		go func() {
			data, err := fetch()
			s.mu.Lock()
			defer s.mu.Unlock()
			e.fetching = false
			if err != nil {
				e.err = err
				e.errAt = time.Now()
				return
			}
			e.data, e.has, e.at = data, true, time.Now()
			e.err = nil
			s.saveCacheLocked()
		}()
	}

	if e.has {
		return e.data, Status{State: StateReady, UpdatedAt: e.at}
	}
	var zero T
	if e.err != nil {
		return zero, Status{State: StateError, Err: e.err.Error()}
	}
	return zero, Status{State: StateLoading}
}

// sectionLocked résout d'abord le roster de la session (qui conditionne la
// disponibilité : pas de piscineux = pas de piscine), puis la section demandée.
func sectionLocked[T any](s *Service, month, year string, e *entry[T], ttl time.Duration, fetch func(roster []Pooler) (T, error)) (T, Status) {
	var zero T

	sess := s.sessionLocked(month, year)
	roster, status := resolveLocked(s, &sess.roster, rosterTTL, func() ([]Pooler, error) {
		return s.fetchPoolers(month, year)
	})
	if status.State != StateReady {
		return zero, status
	}
	if len(roster) == 0 {
		return zero, Status{State: StateUnavailable, UpdatedAt: status.UpdatedAt}
	}
	return resolveLocked(s, e, ttl, func() (T, error) { return fetch(roster) })
}

// Score renvoie le classement par score de coalition (toutes coalitions
// confondues) pour la session demandée. Chaque récupération réussie met à
// jour le relevé de progression du jour.
func (s *Service) Score(month, year string) ([]ScoreRow, Status) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := s.sessionLocked(month, year)
	return sectionLocked(s, month, year, &sess.score, scoreTTL, func(roster []Pooler) ([]ScoreRow, error) {
		rows, err := s.fetchScore(roster)
		if err == nil {
			s.history.record(month+"-"+year, rows)
		}
		return rows, err
	})
}

// History renvoie les relevés quotidiens de progression d'une session,
// du plus ancien au plus récent.
func (s *Service) History(month, year string) []Snapshot {
	return s.history.series(month + "-" + year)
}

// Projects renvoie le classement par nombre de projets validés.
func (s *Service) Projects(month, year string) ([]ProjectRow, Status) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := s.sessionLocked(month, year)
	return sectionLocked(s, month, year, &sess.projects, projectsTTL, s.fetchProjects)
}

// Exam renvoie les notes en direct d'un exam (clé : 00, 01, 02, final).
func (s *Service) Exam(month, year, examKey string) ([]ExamRow, Status) {
	slug, ok := ExamSlugs[examKey]
	if !ok {
		return nil, Status{State: StateError, Err: "exam inconnu : " + examKey}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	sess := s.sessionLocked(month, year)
	e, ok := sess.exams[examKey]
	if !ok {
		e = &entry[[]ExamRow]{}
		sess.exams[examKey] = e
	}
	return sectionLocked(s, month, year, e, examTTL, func(roster []Pooler) ([]ExamRow, error) {
		return s.fetchExam(slug, roster)
	})
}

// --- Récupération API 42 (exécutée hors verrou, dans les goroutines) ---

type flexString string

// UnmarshalJSON accepte indifféremment une chaîne, un nombre ou null
// (pool_year est renvoyé tantôt en string, tantôt en nombre).
func (f *flexString) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		*f = ""
		return nil
	}
	if len(b) > 0 && b[0] == '"' {
		var v string
		if err := json.Unmarshal(b, &v); err != nil {
			return err
		}
		*f = flexString(v)
		return nil
	}
	*f = flexString(b)
	return nil
}

type coalitionJSON struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Slug  string `json:"slug"`
	Color string `json:"color"`
}

func (s *Service) resolveCampusID(ctx context.Context) (int, error) {
	s.mu.Lock()
	id := s.campusID
	s.mu.Unlock()
	if id != 0 {
		return id, nil
	}

	var campuses []struct {
		ID int `json:"id"`
	}
	err := s.c.get(ctx, "/v2/campus", url.Values{"filter[name]": {s.campusName}}, &campuses)
	if err != nil {
		return 0, err
	}
	if len(campuses) == 0 {
		return 0, fmt.Errorf("campus %q introuvable via l'API 42", s.campusName)
	}

	s.mu.Lock()
	s.campusID = campuses[0].ID
	s.saveCacheLocked()
	s.mu.Unlock()
	return campuses[0].ID, nil
}

func (s *Service) fetchPoolers(month, year string) ([]Pooler, error) {
	ctx := context.Background()
	campusID, err := s.resolveCampusID(ctx)
	if err != nil {
		return nil, err
	}

	type cursusUserJSON struct {
		Level float64 `json:"level"`
		User  *struct {
			ID        int        `json:"id"`
			Login     string     `json:"login"`
			Staff     bool       `json:"staff?"`
			PoolMonth string     `json:"pool_month"`
			PoolYear  flexString `json:"pool_year"`
		} `json:"user"`
	}

	cursusUsers, err := getAll[cursusUserJSON](ctx, s.c,
		fmt.Sprintf("/v2/cursus/%d/cursus_users", poolCursusID),
		url.Values{"filter[campus_id]": {strconv.Itoa(campusID)}})
	if err != nil {
		return nil, err
	}

	var poolers []Pooler
	for _, cu := range cursusUsers {
		u := cu.User
		if u == nil || u.Staff || u.PoolMonth != month || string(u.PoolYear) != year {
			continue
		}
		poolers = append(poolers, Pooler{ID: u.ID, Login: u.Login, Level: cu.Level})
	}
	return poolers, nil
}

func (s *Service) fetchCoalitions(ctx context.Context) ([]coalitionJSON, error) {
	s.mu.Lock()
	cached := s.coalitions
	s.mu.Unlock()
	if cached != nil {
		return cached, nil
	}

	campusID, err := s.resolveCampusID(ctx)
	if err != nil {
		return nil, err
	}

	var blocs []struct {
		Coalitions []coalitionJSON `json:"coalitions"`
	}
	err = s.c.get(ctx, "/v2/blocs", url.Values{
		"filter[campus_id]": {strconv.Itoa(campusID)},
		"filter[cursus_id]": {strconv.Itoa(poolCursusID)},
	}, &blocs)
	if err != nil {
		return nil, err
	}

	var coalitions []coalitionJSON
	for _, b := range blocs {
		coalitions = append(coalitions, b.Coalitions...)
	}
	if len(coalitions) == 0 {
		return nil, fmt.Errorf("aucune coalition de piscine trouvée pour ce campus")
	}

	s.mu.Lock()
	s.coalitions = coalitions
	s.saveCacheLocked()
	s.mu.Unlock()
	return coalitions, nil
}

func (s *Service) fetchScore(roster []Pooler) ([]ScoreRow, error) {
	ctx := context.Background()
	coalitions, err := s.fetchCoalitions(ctx)
	if err != nil {
		return nil, err
	}

	byUserID := make(map[int]Pooler, len(roster))
	for _, p := range roster {
		byUserID[p.ID] = p
	}

	type coalitionUserJSON struct {
		UserID int `json:"user_id"`
		Score  int `json:"score"`
	}

	rows := []ScoreRow{}
	for _, coalition := range coalitions {
		users, err := getAll[coalitionUserJSON](ctx, s.c,
			fmt.Sprintf("/v2/coalitions/%d/coalitions_users", coalition.ID), nil)
		if err != nil {
			return nil, err
		}
		color := normalizeHexColor(coalition.Color)
		for _, cu := range users {
			pooler, ok := byUserID[cu.UserID]
			if !ok {
				continue
			}
			rows = append(rows, ScoreRow{
				Login:     pooler.Login,
				Level:     pooler.Level,
				Score:     cu.Score,
				Coalition: coalition.Name,
				Color:     color,
			})
		}
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Score != rows[j].Score {
			return rows[i].Score > rows[j].Score
		}
		return rows[i].Login < rows[j].Login
	})
	return rows, nil
}

// normalizeHexColor valide une couleur hex venue de l'API (utilisée telle
// quelle dans des attributs style) ; renvoie "" si le format est inattendu.
func normalizeHexColor(c string) string {
	if len(c) != 4 && len(c) != 7 {
		return ""
	}
	if c[0] != '#' {
		return ""
	}
	for _, r := range c[1:] {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') && (r < 'A' || r > 'F') {
			return ""
		}
	}
	return c
}

func categorize(slug string) string {
	switch {
	case strings.Contains(slug, "exam"):
		return "exam"
	case strings.Contains(slug, "rush"):
		return "rush"
	case strings.Contains(slug, "shell"):
		return "shell"
	default:
		return "c"
	}
}

func (s *Service) fetchProjects(roster []Pooler) ([]ProjectRow, error) {
	ctx := context.Background()

	type projectsUserJSON struct {
		Validated *bool `json:"validated?"`
		Project   struct {
			Slug string `json:"slug"`
		} `json:"project"`
	}

	rows := []ProjectRow{}
	for _, pooler := range roster {
		projectsUsers, err := getAll[projectsUserJSON](ctx, s.c,
			fmt.Sprintf("/v2/users/%d/projects_users", pooler.ID), nil)
		if err != nil {
			return nil, err
		}

		row := ProjectRow{Login: pooler.Login, Level: pooler.Level}
		for _, pu := range projectsUsers {
			slug := pu.Project.Slug
			if !strings.HasPrefix(slug, "c-piscine") || pu.Validated == nil || !*pu.Validated {
				continue
			}
			switch categorize(slug) {
			case "shell":
				row.Shell++
			case "exam":
				row.Exam++
			case "rush":
				row.Rush++
			default:
				row.C++
			}
		}
		row.Total = row.Shell + row.C + row.Exam + row.Rush
		rows = append(rows, row)
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Total != rows[j].Total {
			return rows[i].Total > rows[j].Total
		}
		if rows[i].Level != rows[j].Level {
			return rows[i].Level > rows[j].Level
		}
		return rows[i].Login < rows[j].Login
	})
	return rows, nil
}

func (s *Service) resolveProjectID(ctx context.Context, slug string) (int, error) {
	s.mu.Lock()
	id, ok := s.projectIDs[slug]
	s.mu.Unlock()
	if ok {
		return id, nil
	}

	var projects []struct {
		ID int `json:"id"`
	}
	if err := s.c.get(ctx, "/v2/projects", url.Values{"filter[slug]": {slug}}, &projects); err != nil {
		return 0, err
	}
	if len(projects) == 0 {
		return 0, fmt.Errorf("projet %q introuvable via l'API 42", slug)
	}

	s.mu.Lock()
	s.projectIDs[slug] = projects[0].ID
	s.saveCacheLocked()
	s.mu.Unlock()
	return projects[0].ID, nil
}

func (s *Service) fetchExam(slug string, roster []Pooler) ([]ExamRow, error) {
	ctx := context.Background()
	projectID, err := s.resolveProjectID(ctx, slug)
	if err != nil {
		return nil, err
	}
	campusID, err := s.resolveCampusID(ctx)
	if err != nil {
		return nil, err
	}

	byUserID := make(map[int]Pooler, len(roster))
	for _, p := range roster {
		byUserID[p.ID] = p
	}

	type projectsUserJSON struct {
		FinalMark *int   `json:"final_mark"`
		Status    string `json:"status"`
		Validated *bool  `json:"validated?"`
		User      struct {
			ID int `json:"id"`
		} `json:"user"`
	}

	projectsUsers, err := getAll[projectsUserJSON](ctx, s.c,
		fmt.Sprintf("/v2/projects/%d/projects_users", projectID),
		url.Values{"filter[campus]": {strconv.Itoa(campusID)}})
	if err != nil {
		return nil, err
	}

	rows := []ExamRow{}
	for _, pu := range projectsUsers {
		pooler, ok := byUserID[pu.User.ID]
		if !ok {
			continue
		}
		row := ExamRow{
			Login:     pooler.Login,
			Status:    pu.Status,
			Validated: pu.Validated != nil && *pu.Validated,
		}
		if pu.FinalMark != nil {
			row.Mark = *pu.FinalMark
			row.HasMark = true
		}
		rows = append(rows, row)
	}

	sort.Slice(rows, func(i, j int) bool {
		mi, mj := -1, -1
		if rows[i].HasMark {
			mi = rows[i].Mark
		}
		if rows[j].HasMark {
			mj = rows[j].Mark
		}
		if mi != mj {
			return mi > mj
		}
		return rows[i].Login < rows[j].Login
	})
	return rows, nil
}
