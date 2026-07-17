package pool

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// poolCursusID est le cursus "C Piscine" côté API 42.
const poolCursusID = 9

// TTL des caches. Les boards (score/projets) se rafraîchissent toutes les
// 10 min. Les exams sont particuliers : hors de leur fenêtre planifiée on
// lève le pied (examIdleTTL) pour épargner l'API, et on n'accélère
// (examLiveTTL) que pendant l'exam. examSchedTTL est le TTL de l'horaire
// des exams (begin_at/end_at) récupéré depuis l'API 42.
const (
	rosterTTL    = 10 * time.Minute
	scoreTTL     = 10 * time.Minute
	projectsTTL  = 10 * time.Minute
	examLiveTTL  = 100 * time.Second // exam en cours : classement quasi live
	examIdleTTL  = time.Hour         // hors fenêtre d'exam : on lève le pied
	examSchedTTL = time.Hour         // horaire des exams (begin_at/end_at)
	errRetry     = 45 * time.Second  // délai avant de retenter après un échec
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
	Login      string
	Mark       int
	HasMark    bool
	Status     string
	Validated  bool
	Registered bool // l'étudiant a le projet exam dans projects_users
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

// examWindow est la fenêtre planifiée d'un exam telle que renvoyée par
// l'API 42 (/v2/exams : begin_at / end_at).
type examWindow struct {
	Begin, End time.Time
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

	// examSched : horaire des exams du campus (slug -> fenêtre), rafraîchi en
	// arrière-plan comme les autres caches. Non persisté (bon marché à refaire).
	examSched entry[map[string]examWindow]
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

// poolRefreshTick est le pas de la boucle de rafraîchissement serveur. Il est
// plus court que les TTL (10 min) pour ne pas rater la fenêtre de péremption ;
// chaque appel est un no-op tant que le cache n'est pas périmé, donc le coût
// API réel reste calé sur les TTL.
const poolRefreshTick = 2 * time.Minute

// StartRefreshLoop garde les classements Score et Projets chauds côté serveur,
// indépendamment de toute visite : sans lui, les données ne se rafraîchissent
// que lorsqu'un navigateur poll la page (htmx), donc restent figées la nuit.
// Chaque tick, si une piscine est en cours, on rappelle Score et Projects : la
// politique stale-while-revalidate déclenche un fetch en tâche de fond dès que
// le cache dépasse son TTL. Rappeler Score enregistre aussi le relevé de
// progression du jour, ce qui garantit un point quotidien même sans visiteur.
func (s *Service) StartRefreshLoop() {
	go func() {
		for {
			if month, year, ok := CurrentSession(time.Now()); ok {
				y := strconv.Itoa(year)
				s.Score(month, y)
				s.Projects(month, y)
			}
			time.Sleep(poolRefreshTick)
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

// nightTTL espace les rafraîchissements la nuit (23h → 8h, heure de France :
// le serveur est en Europe/Paris via time.Local) : les TTL courts passent à
// une heure pour épargner le quota API quand personne ne regarde. Les TTL des
// exams ne passent pas par ici (un exam nocturne resterait live).
func nightTTL(base time.Duration) time.Duration {
	return nightTTLAt(base, time.Now())
}

func nightTTLAt(base time.Duration, now time.Time) time.Duration {
	h := now.Hour()
	if (h >= 23 || h < 8) && base < time.Hour {
		return time.Hour
	}
	return base
}

// resolveLocked applique la politique de cache d'une entry : déclenche un
// fetch asynchrone si nécessaire et renvoie l'état courant. name identifie la
// donnée dans les logs. s.mu doit être tenu par l'appelant.
func resolveLocked[T any](s *Service, name string, e *entry[T], ttl time.Duration, fetch func() (T, error)) (T, Status) {
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
				log.Printf("[pool] échec fetch %s : %v", name, err)
				return
			}
			if e.err != nil {
				log.Printf("[pool] fetch %s rétabli", name)
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
func sectionLocked[T any](s *Service, name, month, year string, e *entry[T], ttl time.Duration, fetch func(roster []Pooler) (T, error)) (T, Status) {
	var zero T

	sess := s.sessionLocked(month, year)
	roster, status := resolveLocked(s, "roster "+month+"-"+year, &sess.roster, nightTTL(rosterTTL), func() ([]Pooler, error) {
		return s.fetchPoolers(month, year)
	})
	if status.State != StateReady {
		return zero, status
	}
	if len(roster) == 0 {
		return zero, Status{State: StateUnavailable, UpdatedAt: status.UpdatedAt}
	}
	return resolveLocked(s, name, e, ttl, func() (T, error) { return fetch(roster) })
}

// Score renvoie le classement par score de coalition (toutes coalitions
// confondues) pour la session demandée. Chaque récupération réussie met à
// jour le relevé de progression du jour.
func (s *Service) Score(month, year string) ([]ScoreRow, Status) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := s.sessionLocked(month, year)
	return sectionLocked(s, "score", month, year, &sess.score, nightTTL(scoreTTL), func(roster []Pooler) ([]ScoreRow, error) {
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
	return sectionLocked(s, "projets", month, year, &sess.projects, nightTTL(projectsTTL), s.fetchProjects)
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
	return sectionLocked(s, "exam "+examKey, month, year, e, s.examTTLLocked(examKey), func(roster []Pooler) ([]ExamRow, error) {
		return s.fetchExam(slug, roster)
	})
}

// InvalidateExam marque le cache d'un exam comme périmé pour que le prochain
// accès re-fetche depuis l'API 42. Purge aussi une éventuelle erreur pour
// court-circuiter le backoff (errRetry) : le bouton « Rafraîchir » doit pouvoir
// relancer une tentative immédiatement. Sert quand un piscineux vient de
// s'inscrire à l'exam et n'apparaît pas encore dans la liste en cache.
func (s *Service) InvalidateExam(month, year, examKey string) {
	if _, ok := ExamSlugs[examKey]; !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := s.sessionLocked(month, year)
	e, ok := sess.exams[examKey]
	if !ok {
		return // jamais chargé : le prochain Exam() fetchera de toute façon
	}
	e.has = false
	e.at = time.Time{}
	e.err = nil
	e.errAt = time.Time{}
}

// ExamRefresh renvoie le pas de rafraîchissement recommandé pour l'onglet d'un
// exam : court (examLiveTTL) quand l'exam est dans sa fenêtre planifiée, long
// (examIdleTTL) sinon. Le front l'utilise pour son propre rythme de polling,
// aligné sur le TTL du cache pour ne pas solliciter l'API pour rien.
func (s *Service) ExamRefresh(examKey string) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.examTTLLocked(examKey)
}

// ExamActive indique si l'exam est actuellement dans sa fenêtre planifiée
// (d'après l'horaire intra). Sert à ne montrer « En cours » que pendant l'exam.
func (s *Service) ExamActive(examKey string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.examActiveLocked(examKey)
}

// ExamKeysOrder est l'ordre chronologique canonique des exams de piscine.
var ExamKeysOrder = []string{"00", "01", "02", "final"}

// ExamWindowInfo décrit la fenêtre planifiée d'un exam (clé + début/fin).
type ExamWindowInfo struct {
	Key   string
	Begin time.Time
	End   time.Time
}

// AllExamWindows renvoie les fenêtres planifiées connues de tous les exams,
// dans l'ordre chronologique des exams (00, 01, 02, final). Résout (et
// rafraîchit en arrière-plan) le cache d'horaire ; renvoie nil tant qu'il
// n'est pas prêt.
func (s *Service) AllExamWindows() []ExamWindowInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	windows, status := resolveLocked(s, "horaire-exams", &s.examSched, examSchedTTL, s.fetchExamSchedule)
	if status.State != StateReady {
		return nil
	}
	var out []ExamWindowInfo
	for _, key := range ExamKeysOrder {
		if w, ok := windows[ExamSlugs[key]]; ok {
			out = append(out, ExamWindowInfo{Key: key, Begin: w.Begin, End: w.End})
		}
	}
	// Tri chronologique réel : l'ordre des clés (00, 01, 02, final) est une
	// convention, pas une garantie de l'intra. Le JSON du décompte côté client
	// hérite de cet ordre.
	sort.Slice(out, func(i, j int) bool { return out[i].Begin.Before(out[j].Begin) })
	return out
}

// ExamWindow renvoie la fenêtre planifiée (début, fin) d'un exam d'après
// l'horaire intra, si elle est connue. Résout (et rafraîchit en arrière-plan)
// le cache d'horaire, comme examActiveLocked.
func (s *Service) ExamWindow(examKey string) (begin, end time.Time, ok bool) {
	slug, exists := ExamSlugs[examKey]
	if !exists {
		return time.Time{}, time.Time{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	windows, status := resolveLocked(s, "horaire-exams", &s.examSched, examSchedTTL, s.fetchExamSchedule)
	if status.State != StateReady {
		return time.Time{}, time.Time{}, false
	}
	w, exists := windows[slug]
	if !exists {
		return time.Time{}, time.Time{}, false
	}
	return w.Begin, w.End, true
}

// examTTLLocked choisit le TTL de l'exam selon qu'il est en cours ou non.
// s.mu doit être tenu.
func (s *Service) examTTLLocked(examKey string) time.Duration {
	if s.examActiveLocked(examKey) {
		return examLiveTTL
	}
	return examIdleTTL
}

// examActiveLocked indique si l'exam est actuellement dans sa fenêtre planifiée
// d'après l'horaire intra. Il résout (et rafraîchit en arrière-plan) le cache
// d'horaire ; tant que l'horaire est inconnu on répond false, donc on reste sur
// le rythme lent - quitte à basculer en rapide dès que l'horaire est chargé.
// s.mu doit être tenu.
func (s *Service) examActiveLocked(examKey string) bool {
	slug, ok := ExamSlugs[examKey]
	if !ok {
		return false
	}
	windows, status := resolveLocked(s, "horaire-exams", &s.examSched, examSchedTTL, s.fetchExamSchedule)
	if status.State != StateReady {
		return false
	}
	w, ok := windows[slug]
	if !ok {
		return false
	}
	now := time.Now()
	return !now.Before(w.Begin) && !now.After(w.End)
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

// isPoolExamSlug indique si un slug de projet correspond à l'un des exams de
// piscine qu'on affiche (les valeurs d'ExamSlugs).
func isPoolExamSlug(slug string) bool {
	for _, s := range ExamSlugs {
		if s == slug {
			return true
		}
	}
	return false
}

// examDoneRetention est la durée pendant laquelle on garde la fenêtre d'un
// exam terminé : l'encadré affiche « Exam terminé » jusqu'à ce que l'horaire
// du suivant soit publié, au lieu de retomber sur « horaire non disponible ».
const examDoneRetention = 12 * time.Hour

// betterWindow renvoie true si w doit remplacer cur comme fenêtre affichée
// pour un exam : une fenêtre non terminée bat une terminée ; entre deux non
// terminées on garde celle qui commence le plus tôt ; entre deux terminées,
// la plus récente.
func betterWindow(cur, w examWindow, now time.Time) bool {
	curEnded := cur.End.Before(now)
	wEnded := w.End.Before(now)
	if curEnded != wEnded {
		return curEnded
	}
	if wEnded {
		return w.End.After(cur.End)
	}
	return w.Begin.Before(cur.Begin)
}

// fetchExamSchedule récupère l'horaire des exams de piscine du campus depuis
// l'API 42 et le réduit à une fenêtre (begin_at/end_at) par slug d'exam. On ne
// demande que les exams récents ou à venir (fenêtre bornée côté serveur) ; les
// exams terminés depuis moins de examDoneRetention sont conservés pour que
// « Exam terminé » reste affiché en attendant l'horaire du suivant.
func (s *Service) fetchExamSchedule() (map[string]examWindow, error) {
	ctx := context.Background()
	campusID, err := s.resolveCampusID(ctx)
	if err != nil {
		return nil, err
	}

	type examJSON struct {
		BeginAt  time.Time `json:"begin_at"`
		EndAt    time.Time `json:"end_at"`
		Projects []struct {
			Slug string `json:"slug"`
		} `json:"projects"`
	}

	now := time.Now()
	// Borne basse : rétention (12h après end_at) + marge pour la durée de
	// l'exam lui-même, puisque le filtre API porte sur begin_at.
	exams, err := getAll[examJSON](ctx, s.c,
		fmt.Sprintf("/v2/campus/%d/exams", campusID),
		url.Values{"range[begin_at]": {
			now.Add(-(examDoneRetention + 12*time.Hour)).Format(time.RFC3339) + "," +
				now.AddDate(0, 2, 0).Format(time.RFC3339),
		}})
	if err != nil {
		return nil, err
	}

	windows := make(map[string]examWindow)
	for _, ex := range exams {
		if ex.EndAt.Before(now.Add(-examDoneRetention)) {
			continue // terminé depuis trop longtemps
		}
		for _, p := range ex.Projects {
			if !isPoolExamSlug(p.Slug) {
				continue
			}
			w := examWindow{Begin: ex.BeginAt, End: ex.EndAt}
			if cur, ok := windows[p.Slug]; !ok || betterWindow(cur, w, now) {
				windows[p.Slug] = w
			}
		}
	}
	return windows, nil
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

	// Inscrits à l'exam : présence d'une entrée projects_users, indexée par
	// user_id pour croiser avec le roster complet.
	byUser := make(map[int]projectsUserJSON, len(projectsUsers))
	for _, pu := range projectsUsers {
		byUser[pu.User.ID] = pu
	}

	// On liste TOUT le roster : un piscineux sans entrée projects_users pour cet
	// exam est simplement « non inscrit ».
	rows := make([]ExamRow, 0, len(roster))
	for _, pooler := range roster {
		row := ExamRow{Login: pooler.Login}
		if pu, ok := byUser[pooler.ID]; ok {
			row.Registered = true
			row.Status = pu.Status
			row.Validated = pu.Validated != nil && *pu.Validated
			if pu.FinalMark != nil {
				row.Mark = *pu.FinalMark
				row.HasMark = true
			}
		}
		rows = append(rows, row)
	}

	// Tri : inscrits avant non-inscrits, puis par note décroissante, puis login.
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.Registered != b.Registered {
			return a.Registered
		}
		mi, mj := -1, -1
		if a.HasMark {
			mi = a.Mark
		}
		if b.HasMark {
			mj = b.Mark
		}
		if mi != mj {
			return mi > mj
		}
		return a.Login < b.Login
	})
	return rows, nil
}
