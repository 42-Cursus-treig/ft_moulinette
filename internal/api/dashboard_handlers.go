package api

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"log"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tristan-reig/ft-moulinette/internal/auth"
	"github.com/tristan-reig/ft-moulinette/internal/fortytwo"
	"github.com/tristan-reig/ft-moulinette/internal/pool"
)

// dashboardPage (GET /dashboard) rend la coquille de la page : chaque carte
// se charge ensuite en htmx (hx-trigger="load"), pour que les latences et
// pannes de l'API 42 ne bloquent jamais la page — une carte en échec propose
// « Réessayer », les autres vivent leur vie.
func (h *handlers) dashboardPage(w http.ResponseWriter, r *http.Request) {
	user, _ := userFromContext(r.Context())
	data := h.navFlags(user)
	data["User"] = user
	data["Page"] = "dashboard"
	if err := h.tmpl.ExecuteTemplate(w, "dashboard", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// dashCards associe chaque carte à son constructeur de données.
// Clé = segment d'URL (/ui/dashboard/{card}) = suffixe du template (dash_{card}).
var dashCards = map[string]func(*handlers, context.Context, auth.User, string) (any, error){
	"hero":         (*handlers).dashHero,
	"logtime":      (*handlers).dashLogtime,
	"projects":     (*handlers).dashProjects,
	"skills":       (*handlers).dashSkills,
	"coalition":    (*handlers).dashCoalition,
	"evals":        (*handlers).dashEvals,
	"points":       (*handlers).dashPoints,
	"achievements": (*handlers).dashAchievements,
	"events":       (*handlers).dashEvents,
	"promo":        (*handlers).dashPromo,
	"exam":         (*handlers).dashExam,
	"defenses":     (*handlers).dashDefenses,
	"progress":     (*handlers).dashProgress,
	"ready":        (*handlers).dashReady,
	"lookup":       (*handlers).dashLookup,
}

// dashLocalCards se servent des données déjà en cache côté serveur (service
// pool) ou d'aucune donnée : pas besoin de token 42, elles marchent même si
// le refresh échoue.
var dashLocalCards = map[string]bool{"promo": true, "exam": true, "progress": true, "lookup": true}

// dashboardCard (GET /ui/dashboard/{card}) rend une carte du dashboard.
// Les erreurs sortent en 200 avec un panneau « Réessayer » : htmx ne swappe
// pas les réponses non-2xx, on garderait sinon un squelette éternel.
func (h *handlers) dashboardCard(w http.ResponseWriter, r *http.Request) {
	card := r.PathValue("card")
	build, ok := dashCards[card]
	if !ok {
		http.NotFound(w, r)
		return
	}
	user, _ := userFromContext(r.Context())

	var accessToken string
	var err error
	if !dashLocalCards[card] {
		var tok auth.Token
		if tok, err = h.sessions.FreshToken(r, h.oauth); err == nil {
			accessToken = tok.AccessToken
		}
	}
	var data any
	if err == nil {
		// Les requêtes 42 d'un utilisateur sont sérialisées à ~2 req/s : avec
		// autant de cartes lancées en parallèle, les dernières patientent dans
		// la file. Le timeout couvre la file plus un aller-retour lent.
		ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
		defer cancel()
		data, err = build(h, ctx, user, accessToken)
	}
	if err != nil {
		h.renderDashError(w, card, err)
		return
	}
	if err := h.tmpl.ExecuteTemplate(w, "dash_"+card, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// renderDashError affiche l'état d'erreur d'une carte, adapté à la cause :
// une panne se retente (bouton Réessayer), un refus de scope est permanent
// (aucun bouton — réessayer ne changera jamais rien), une session morte
// propose de se reconnecter.
func (h *handlers) renderDashError(w http.ResponseWriter, card string, err error) {
	kind := "warn"
	msg := "L'API 42 n'a pas répondu. Elle connaît régulièrement des pannes — réessaie dans un instant."
	var apiErr *fortytwo.APIError
	switch {
	case errors.Is(err, fortytwo.ErrDown):
		msg = "L'API 42 est en panne en ce moment. Réessaie dans une ou deux minutes."
	case errors.As(err, &apiErr) && apiErr.Status == http.StatusForbidden:
		kind = "scope"
		msg = "Cette donnée demande le scope « projects », que ton jeton n'a pas. Coche « projects » sur l'app 42, lance le serveur avec MOULINETTE_42_SCOPE=\"public projects\", puis reconnecte-toi."
	case errors.As(err, &apiErr) && apiErr.Status == http.StatusUnauthorized:
		kind = "auth"
		msg = "Ta session 42 n'est plus valide."
	case strings.Contains(err.Error(), "session"), strings.Contains(err.Error(), "refresh"):
		kind = "auth"
		msg = "Ta session a expiré."
	}
	log.Printf("[dashboard] carte %s : %v", card, err)
	if terr := h.tmpl.ExecuteTemplate(w, "dash_error", map[string]any{
		"Kind":    kind,
		"Message": msg,
		"Retry":   "/ui/dashboard/" + card,
	}); terr != nil {
		http.Error(w, terr.Error(), http.StatusInternalServerError)
	}
}

// scopeForbidden dit si err est un refus définitif de l'API 42 (403 : le
// scope de l'app ne couvre pas l'endpoint) — utile aux cartes qui préfèrent
// dégrader leur contenu plutôt que d'afficher un panneau d'erreur entier.
func scopeForbidden(err error) bool {
	var apiErr *fortytwo.APIError
	return errors.As(err, &apiErr) && apiErr.Status == http.StatusForbidden
}

// bestCursus choisit le cursus à mettre en avant : le plus récemment commencé
// (pour un pisciner, sa piscine ; pour un étudiant, le cursus principal).
func bestCursus(me *fortytwo.Me) *fortytwo.CursusUser {
	var best *fortytwo.CursusUser
	for i := range me.CursusUsers {
		cu := &me.CursusUsers[i]
		if best == nil || cu.BeginAt.After(best.BeginAt) {
			best = cu
		}
	}
	return best
}

// --- Carte héro (profil) ---

type dashHeroView struct {
	Login, Displayname, Avatar string
	Title                      string // titre sélectionné sur l'intra, %login remplacé
	CampusLine                 string // « Perpignan · France »
	PoolBadge                  string // « Piscine juillet 2026 »
	MemberSince                string
	CursusName                 string
	Grade                      string
	Level                      string // « 8.42 »
	LevelPct                   int    // décimales du niveau, en % vers le suivant
	Wallet                     int
	CorrectionPoint            int
	Location                   string // host du poste si loggé en cluster
}

func (h *handlers) dashHero(ctx context.Context, user auth.User, tok string) (any, error) {
	me, err := h.ft.Me(ctx, user.Login, tok)
	if err != nil {
		return nil, err
	}

	v := dashHeroView{
		Login:           me.Login,
		Displayname:     me.Displayname,
		Avatar:          me.Image.Link,
		Wallet:          me.Wallet,
		CorrectionPoint: me.CorrectionPoint,
		Location:        me.Location,
	}
	if v.Displayname == "" {
		v.Displayname = me.Login
	}
	if me.Image.Versions.Medium != "" {
		v.Avatar = me.Image.Versions.Medium
	}

	for _, tu := range me.TitlesUsers {
		if !tu.Selected {
			continue
		}
		for _, t := range me.Titles {
			if t.ID == tu.TitleID {
				v.Title = strings.ReplaceAll(t.Name, "%login", me.Login)
			}
		}
	}

	primaryID := 0
	for _, cu := range me.CampusUsers {
		if cu.IsPrimary {
			primaryID = cu.CampusID
		}
	}
	for i, c := range me.Campus {
		if i == 0 || c.ID == primaryID {
			v.CampusLine = c.Name
			if c.Country != "" {
				v.CampusLine += " · " + c.Country
			}
			if c.ID == primaryID {
				break
			}
		}
	}

	if me.PoolMonth != "" && me.PoolYear != "" {
		v.PoolBadge = "Piscine " + frMonthName(me.PoolMonth) + " " + me.PoolYear
	}
	if !me.CreatedAt.IsZero() {
		t := me.CreatedAt.Local()
		v.MemberSince = fmt.Sprintf("Sur l'intra depuis %s %d", frMonthsFull[t.Month()-1], t.Year())
	}

	if cu := bestCursus(me); cu != nil {
		v.CursusName = cu.Cursus.Name
		v.Level = fmt.Sprintf("%.2f", cu.Level)
		v.LevelPct = int((cu.Level - math.Floor(cu.Level)) * 100)
		if cu.Grade != nil {
			v.Grade = *cu.Grade
		}
	}
	return v, nil
}

// --- Carte temps de présence (heatmap façon contribution graph) ---

type hmCell struct {
	Class string // l0..l4 selon les heures, lx = jour hors période
	Title string
}

type dashLogtimeView struct {
	Weeks      [][]hmCell // une colonne par semaine, 7 cases lun→dim
	MonthRow   []string   // libellé de mois au-dessus de chaque colonne ("" = rien)
	Last7      string
	Last30     string
	BestDay    string
	ActiveDays int
	Streak     int // jours actifs consécutifs, série en cours
	BestStreak int
	Location   string
}

func (h *handlers) dashLogtime(ctx context.Context, user auth.User, tok string) (any, error) {
	me, err := h.ft.Me(ctx, user.Login, tok)
	if err != nil {
		return nil, err
	}

	today := time.Now()
	weekday := (int(today.Weekday()) + 6) % 7 // lundi = 0
	monday := today.AddDate(0, 0, -weekday)
	start := monday.AddDate(0, 0, -7*11) // 12 semaines pleines

	stats, err := h.ft.LocationsStats(ctx, user.Login, tok, me.ID, start, today)
	if err != nil {
		return nil, err
	}

	v := dashLogtimeView{Location: me.Location}
	v.Weeks, v.MonthRow = buildHeatmap(stats, start, today)

	var last7, last30, best float64
	for k, raw := range stats {
		d, err := time.ParseInLocation("2006-01-02", k, time.Local)
		if err != nil {
			continue
		}
		hrs := parseLogHours(raw)
		if hrs <= 0 {
			continue
		}
		v.ActiveDays++
		if hrs > best {
			best = hrs
		}
		age := today.Sub(d)
		if age < 7*24*time.Hour {
			last7 += hrs
		}
		if age < 30*24*time.Hour {
			last30 += hrs
		}
	}
	v.Last7 = fmtHours(last7)
	v.Last30 = fmtHours(last30)
	v.BestDay = fmtHours(best)
	v.Streak, v.BestStreak = logStreaks(stats, start, today)
	return v, nil
}

// logStreaks calcule la série de jours actifs en cours et la meilleure série
// de la période. Un aujourd'hui encore vide ne casse pas la série : la
// journée n'est pas finie.
func logStreaks(stats map[string]string, start, today time.Time) (current, best int) {
	todayKey := today.Format("2006-01-02")
	run := 0
	for d := start; ; d = d.AddDate(0, 0, 1) {
		key := d.Format("2006-01-02")
		if key > todayKey {
			break
		}
		if parseLogHours(stats[key]) > 0 {
			run++
			if run > best {
				best = run
			}
		} else if key != todayKey {
			run = 0
		}
	}
	return run, best
}

// buildHeatmap découpe la période en colonnes hebdomadaires, avec un libellé
// de mois quand une colonne change de mois par rapport à la précédente.
func buildHeatmap(stats map[string]string, start, today time.Time) ([][]hmCell, []string) {
	var weeks [][]hmCell
	var months []string
	prevMonth := time.Month(0)
	for w := 0; ; w++ {
		monday := start.AddDate(0, 0, w*7)
		if monday.After(today) {
			break
		}
		label := ""
		if monday.Month() != prevMonth {
			label = frMonthsShort[monday.Month()-1]
			prevMonth = monday.Month()
		}
		months = append(months, label)

		col := make([]hmCell, 7)
		for d := 0; d < 7; d++ {
			date := monday.AddDate(0, 0, d)
			if date.Format("2006-01-02") > today.Format("2006-01-02") {
				col[d] = hmCell{Class: "lx"}
				continue
			}
			hrs := parseLogHours(stats[date.Format("2006-01-02")])
			col[d] = hmCell{
				Class: hmClass(hrs),
				Title: fmt.Sprintf("%s %d %s — %s", frDaysShort[date.Weekday()], date.Day(), frMonthsShort[date.Month()-1], fmtHours(hrs)),
			}
		}
		weeks = append(weeks, col)
	}
	return weeks, months
}

func hmClass(hours float64) string {
	switch {
	case hours <= 0:
		return "l0"
	case hours < 2:
		return "l1"
	case hours < 4:
		return "l2"
	case hours < 7:
		return "l3"
	default:
		return "l4"
	}
}

// parseLogHours convertit une durée 42 « HH:MM:SS.micro » en heures.
func parseLogHours(s string) float64 {
	if s == "" {
		return 0
	}
	parts := strings.Split(s, ":")
	if len(parts) != 3 {
		return 0
	}
	hrs, _ := strconv.Atoi(parts[0])
	mins, _ := strconv.Atoi(parts[1])
	secs, _ := strconv.ParseFloat(parts[2], 64)
	return float64(hrs) + float64(mins)/60 + secs/3600
}

// --- Carte projets ---

type donutSeg struct {
	Class  string
	Dash   string
	Offset string
}

type dashProjectRow struct {
	Name   string
	Mark   string
	Status string // ok | ko | wip | wait
	Date   string
}

type dashProjectsView struct {
	CursusName string
	Rows       []dashProjectRow
	More       int
	Validated  int
	Failed     int
	InProgress int
	Total      int
	Segments   []donutSeg
}

func (h *handlers) dashProjects(ctx context.Context, user auth.User, tok string) (any, error) {
	me, err := h.ft.Me(ctx, user.Login, tok)
	if err != nil {
		return nil, err
	}

	v := dashProjectsView{CursusName: "tous cursus"}
	cursusID := 0
	if cu := bestCursus(me); cu != nil {
		cursusID = cu.Cursus.ID
		v.CursusName = cu.Cursus.Name
	}

	var kept []fortytwo.ProjectUser
	for _, pu := range me.ProjectsUsers {
		if cursusID != 0 && !containsInt(pu.CursusIDs, cursusID) {
			continue
		}
		kept = append(kept, pu)
	}
	// Données inattendues (aucun projet rattaché au cursus retenu) : tout montrer.
	if len(kept) == 0 {
		kept = me.ProjectsUsers
		v.CursusName = "tous cursus"
	}

	sort.SliceStable(kept, func(i, j int) bool {
		return projectDate(kept[i]).After(projectDate(kept[j]))
	})

	for _, pu := range kept {
		status := projectStatus(pu)
		switch status {
		case "ok":
			v.Validated++
		case "ko":
			v.Failed++
		default:
			v.InProgress++
		}
		row := dashProjectRow{Name: pu.Project.Name, Status: status, Mark: "—"}
		if pu.FinalMark != nil {
			row.Mark = strconv.Itoa(*pu.FinalMark)
		}
		if pu.MarkedAt != nil {
			row.Date = frDateShort(*pu.MarkedAt)
		}
		v.Rows = append(v.Rows, row)
	}
	v.Total = len(v.Rows)
	if len(v.Rows) > 18 {
		v.More = len(v.Rows) - 18
		v.Rows = v.Rows[:18]
	}
	v.Segments = donutSegments(
		[]int{v.Validated, v.Failed, v.InProgress},
		[]string{"ok", "ko", "wip"},
	)
	return v, nil
}

func projectDate(pu fortytwo.ProjectUser) time.Time {
	if pu.MarkedAt != nil {
		return *pu.MarkedAt
	}
	return pu.CreatedAt
}

func projectStatus(pu fortytwo.ProjectUser) string {
	if pu.Validated != nil {
		if *pu.Validated {
			return "ok"
		}
		return "ko"
	}
	if pu.Status == "waiting_for_correction" {
		return "wait"
	}
	return "wip"
}

// donutSegments transforme des effectifs en arcs SVG : le cercle de rayon
// 15.9155 a une circonférence de 100, les dash-arrays parlent donc en %.
func donutSegments(counts []int, classes []string) []donutSeg {
	total := 0
	for _, c := range counts {
		total += c
	}
	if total == 0 {
		return nil
	}
	var segs []donutSeg
	start := 0.0
	for i, cnt := range counts {
		if cnt == 0 {
			continue
		}
		frac := float64(cnt) / float64(total) * 100
		segs = append(segs, donutSeg{
			Class: classes[i],
			Dash:  fmt.Sprintf("%.2f %.2f", frac, 100-frac),
			// 0-start et non -start : nier 0.0 donne le zéro négatif IEEE,
			// qui s'affiche « -0.00 ».
			Offset: fmt.Sprintf("%.2f", 0-start),
		})
		start += frac
	}
	return segs
}

func containsInt(xs []int, x int) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// --- Carte compétences ---

type dashSkillRow struct {
	Name  string
	Level string
	Pct   int
}

type dashSkillsView struct {
	CursusName string
	Skills     []dashSkillRow
}

func (h *handlers) dashSkills(ctx context.Context, user auth.User, tok string) (any, error) {
	me, err := h.ft.Me(ctx, user.Login, tok)
	if err != nil {
		return nil, err
	}

	v := dashSkillsView{}
	cu := bestCursus(me)
	if cu == nil {
		return v, nil
	}
	v.CursusName = cu.Cursus.Name

	maxLevel := 0.0
	for _, s := range cu.Skills {
		if s.Level > maxLevel {
			maxLevel = s.Level
		}
	}
	denom := math.Max(math.Ceil(maxLevel), 1)

	skills := append([]fortytwo.Skill(nil), cu.Skills...)
	sort.Slice(skills, func(i, j int) bool { return skills[i].Level > skills[j].Level })
	for _, s := range skills {
		v.Skills = append(v.Skills, dashSkillRow{
			Name:  s.Name,
			Level: fmt.Sprintf("%.2f", s.Level),
			Pct:   int(s.Level / denom * 100),
		})
	}
	return v, nil
}

// --- Carte coalition ---

type dashCoalitionView struct {
	Empty bool
	Name  string
	Color template.CSS // validée par regex avant d'être marquée sûre
	Score string       // score personnel apporté à la coalition
	Total string       // score global de la coalition
	Rank  string       // « #3 » dans la coalition, si connu
}

var hexColorRe = regexp.MustCompile(`^#[0-9a-fA-F]{3,8}$`)

func (h *handlers) dashCoalition(ctx context.Context, user auth.User, tok string) (any, error) {
	me, err := h.ft.Me(ctx, user.Login, tok)
	if err != nil {
		return nil, err
	}
	cols, err := h.ft.Coalitions(ctx, user.Login, tok, me.ID)
	if err != nil {
		return nil, err
	}
	if len(cols) == 0 {
		return dashCoalitionView{Empty: true}, nil
	}

	// L'adhésion la plus récente (id le plus haut) désigne la coalition
	// active ; sans coalitions_users on retombe sur la première listée.
	chosen := cols[0]
	v := dashCoalitionView{}
	if cus, err := h.ft.CoalitionUsers(ctx, user.Login, tok, me.ID); err == nil && len(cus) > 0 {
		best := cus[0]
		for _, cu := range cus[1:] {
			if cu.ID > best.ID {
				best = cu
			}
		}
		for _, c := range cols {
			if c.ID == best.CoalitionID {
				chosen = c
			}
		}
		v.Score = fmtInt(best.Score)
		if best.Rank > 0 {
			v.Rank = "#" + strconv.Itoa(best.Rank)
		}
	}

	v.Name = chosen.Name
	v.Total = fmtInt(chosen.Score)
	v.Color = template.CSS("var(--accent)")
	if hexColorRe.MatchString(chosen.Color) {
		v.Color = template.CSS(chosen.Color)
	}
	return v, nil
}

// --- Carte évaluations ---

type dashEvalRow struct {
	Who      string
	Mark     string
	HasMark  bool
	Pending  bool // défense planifiée, pas encore remplie
	Flag     string
	Positive bool
	Date     string
	Comment  string
}

type dashEvalsView struct {
	Received []dashEvalRow // l'utilisateur a été corrigé
	Given    []dashEvalRow // l'utilisateur a corrigé
	Note     string        // un des deux volets a échoué : on montre l'autre
}

func (h *handlers) dashEvals(ctx context.Context, user auth.User, tok string) (any, error) {
	me, err := h.ft.Me(ctx, user.Login, tok)
	if err != nil {
		return nil, err
	}
	received, errR := h.ft.ScaleTeams(ctx, user.Login, tok, me.ID, "as_corrected")
	given, errG := h.ft.ScaleTeams(ctx, user.Login, tok, me.ID, "as_corrector")
	if errR != nil && errG != nil {
		return nil, errR
	}

	v := dashEvalsView{}
	if errR != nil || errG != nil {
		v.Note = "Une partie des évaluations n'a pas pu être chargée."
	}
	for _, st := range received {
		v.Received = append(v.Received, evalRow(st, true))
	}
	for _, st := range given {
		v.Given = append(v.Given, evalRow(st, false))
	}
	return v, nil
}

func evalRow(st fortytwo.ScaleTeam, received bool) dashEvalRow {
	row := dashEvalRow{
		Date:     frDateShort(st.BeginAt),
		Flag:     st.Flag.Name,
		Positive: st.Flag.Positive,
	}
	if received {
		row.Who = st.Corrector.Login
	} else {
		var names []string
		for _, cu := range st.Correcteds {
			if cu.Login != "" {
				names = append(names, cu.Login)
			}
		}
		row.Who = strings.Join(names, ", ")
	}
	if row.Who == "" {
		row.Who = "anonyme"
	}
	if st.FinalMark != nil {
		row.Mark = strconv.Itoa(*st.FinalMark)
		row.HasMark = true
	} else if st.FilledAt == nil {
		row.Pending = true
	}
	if st.Comment != nil {
		row.Comment = truncate(strings.TrimSpace(*st.Comment), 140)
	}
	return row
}

// --- Carte points de correction ---

type dashPointMove struct {
	Delta  string
	Neg    bool
	Reason string
	Date   string
}

type dashPointsView struct {
	Current  int
	Spark    string // points de la polyline SVG (évolution chronologique)
	HasSpark bool
	Moves    []dashPointMove
	Note     string // historique inaccessible : le solde reste affiché
}

func (h *handlers) dashPoints(ctx context.Context, user auth.User, tok string) (any, error) {
	me, err := h.ft.Me(ctx, user.Login, tok)
	if err != nil {
		return nil, err
	}
	hist, err := h.ft.PointHistorics(ctx, user.Login, tok, me.ID)
	if err != nil {
		// Le solde vient de /v2/me : autant l'afficher même sans historique.
		v := dashPointsView{Current: me.CorrectionPoint}
		if scopeForbidden(err) {
			v.Note = "L'historique n'est pas lisible avec le scope « public » de l'application — le solde, lui, est à jour."
		} else {
			v.Note = "Historique momentanément indisponible (API 42)."
		}
		return v, nil
	}

	v := dashPointsView{Current: me.CorrectionPoint}

	// L'historique arrive du plus récent au plus ancien : on reconstruit le
	// solde après chaque mouvement en remontant depuis le solde actuel.
	totals := make([]int, len(hist))
	running := me.CorrectionPoint
	for i, mv := range hist {
		totals[i] = running
		running -= mv.Sum
	}
	// Remise en ordre chronologique pour la courbe, bornée aux 30 derniers.
	for i, j := 0, len(totals)-1; i < j; i, j = i+1, j-1 {
		totals[i], totals[j] = totals[j], totals[i]
	}
	if len(totals) > 30 {
		totals = totals[len(totals)-30:]
	}
	v.Spark = sparkline(totals)
	v.HasSpark = v.Spark != ""

	for i, mv := range hist {
		if i == 6 {
			break
		}
		v.Moves = append(v.Moves, dashPointMove{
			Delta:  fmt.Sprintf("%+d", mv.Sum),
			Neg:    mv.Sum < 0,
			Reason: frReason(mv.Reason),
			Date:   frDateShort(mv.CreatedAt),
		})
	}
	return v, nil
}

// sparkline projette une série sur une polyline dans un viewBox 120×32.
func sparkline(vals []int) string {
	if len(vals) < 2 {
		return ""
	}
	lo, hi := vals[0], vals[0]
	for _, v := range vals {
		if v < lo {
			lo = v
		}
		if v > hi {
			hi = v
		}
	}
	span := hi - lo
	if span == 0 {
		span = 1
	}
	var b strings.Builder
	for i, v := range vals {
		if i > 0 {
			b.WriteByte(' ')
		}
		x := 2 + float64(i)*116/float64(len(vals)-1)
		y := 29 - float64(v-lo)/float64(span)*26
		fmt.Fprintf(&b, "%.1f,%.1f", x, y)
	}
	return b.String()
}

// --- Carte succès ---

type dashAchievement struct {
	Name        string
	Description string
	Tier        string
	TierClass   string
}

type dashAchievementsView struct {
	Total int
	Items []dashAchievement
	More  int
}

var tierWeight = map[string]int{"challenge": 4, "hard": 3, "medium": 2, "easy": 1}
var tierLabel = map[string]string{"challenge": "Challenge", "hard": "Difficile", "medium": "Moyen", "easy": "Facile"}

func (h *handlers) dashAchievements(ctx context.Context, user auth.User, tok string) (any, error) {
	me, err := h.ft.Me(ctx, user.Login, tok)
	if err != nil {
		return nil, err
	}

	items := append([]fortytwo.Achievement(nil), me.Achievements...)
	sort.SliceStable(items, func(i, j int) bool {
		wi, wj := tierWeight[items[i].Tier], tierWeight[items[j].Tier]
		if wi != wj {
			return wi > wj
		}
		return items[i].Name < items[j].Name
	})

	v := dashAchievementsView{Total: len(items)}
	for i, a := range items {
		if i == 12 {
			v.More = len(items) - 12
			break
		}
		item := dashAchievement{Name: a.Name, Description: a.Description, TierClass: "t" + strconv.Itoa(tierWeight[a.Tier])}
		if lbl, ok := tierLabel[a.Tier]; ok {
			item.Tier = lbl
		}
		v.Items = append(v.Items, item)
	}
	return v, nil
}

// --- Carte événements ---

type dashEventRow struct {
	Name     string
	Kind     string
	When     string
	Location string
}

type dashEventsView struct {
	Upcoming []dashEventRow
	Total    int
}

var frEventKinds = map[string]string{
	"exam": "Exam", "meet_up": "Meetup", "workshop": "Atelier",
	"conference": "Conférence", "hackathon": "Hackathon", "association": "Asso",
	"pedago": "Pédago", "rush": "Rush", "event": "Événement", "extern": "Externe",
	"speed_working": "Speed working", "partnership": "Partenariat", "challenge": "Challenge",
}

func (h *handlers) dashEvents(ctx context.Context, user auth.User, tok string) (any, error) {
	me, err := h.ft.Me(ctx, user.Login, tok)
	if err != nil {
		return nil, err
	}
	events, err := h.ft.Events(ctx, user.Login, tok, me.ID)
	if err != nil {
		return nil, err
	}

	v := dashEventsView{Total: len(events)}
	now := time.Now()
	// L'API renvoie du plus récent au plus ancien : on collecte les futurs
	// puis on inverse pour afficher le plus proche en premier.
	var upcoming []fortytwo.Event
	for _, ev := range events {
		if ev.BeginAt.After(now) {
			upcoming = append(upcoming, ev)
		}
	}
	for i := len(upcoming) - 1; i >= 0 && len(v.Upcoming) < 4; i-- {
		ev := upcoming[i]
		kind := ev.Kind
		if lbl, ok := frEventKinds[kind]; ok {
			kind = lbl
		}
		t := ev.BeginAt.Local()
		v.Upcoming = append(v.Upcoming, dashEventRow{
			Name:     ev.Name,
			Kind:     kind,
			When:     fmt.Sprintf("%s · %dh%02d", frDateShort(ev.BeginAt), t.Hour(), t.Minute()),
			Location: ev.Location,
		})
	}
	return v, nil
}

// --- Carte classement promo (données locales du service pool) ---

type dashPromoRow struct {
	Medal string
	Login string
	Score string
}

type dashPromoView struct {
	Unavailable bool
	Reason      string
	InRoster    bool
	Rank        string
	Total       int
	Score       string
	Level       string
	Coalition   string
	Ahead       string         // écart avec le rang au-dessus ("" si premier)
	Podium      []dashPromoRow // top 3, montré quand l'utilisateur n'est pas classé
}

// dashPromo situe l'utilisateur dans le classement de la promo, à partir du
// cache du service pool (rafraîchi en continu côté serveur) : aucun appel à
// l'API 42, la carte répond instantanément même en pleine panne.
func (h *handlers) dashPromo(_ context.Context, user auth.User, _ string) (any, error) {
	v := dashPromoView{}
	if h.pool == nil {
		v.Unavailable, v.Reason = true, "Service de classement inactif."
		return v, nil
	}
	month, year, ok := pool.CurrentSession(time.Now())
	if !ok {
		v.Unavailable, v.Reason = true, "Pas de piscine en cours."
		return v, nil
	}
	rows, _ := h.pool.Score(month, strconv.Itoa(year))
	if len(rows) == 0 {
		v.Unavailable, v.Reason = true, "Classement pas encore chargé — repasse dans une minute."
		return v, nil
	}

	sorted := append([]pool.ScoreRow(nil), rows...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Score > sorted[j].Score })
	v.Total = len(sorted)
	for i, row := range sorted {
		if row.Login != user.Login {
			continue
		}
		v.InRoster = true
		v.Rank = "#" + strconv.Itoa(i+1)
		v.Score = fmtInt(row.Score)
		v.Level = fmt.Sprintf("%.2f", row.Level)
		v.Coalition = row.Coalition
		if i > 0 {
			v.Ahead = fmt.Sprintf("à %s pts du rang au-dessus", fmtInt(sorted[i-1].Score-row.Score))
		}
		break
	}
	if !v.InRoster {
		medals := []string{"🥇", "🥈", "🥉"}
		for i := 0; i < len(sorted) && i < 3; i++ {
			v.Podium = append(v.Podium, dashPromoRow{Medal: medals[i], Login: sorted[i].Login, Score: fmtInt(sorted[i].Score)})
		}
	}
	return v, nil
}

// --- Carte exams (horaire local du service pool) ---

type dashExamView struct {
	Available bool
	State     string // upcoming | active | done
	Label     string
	Date      string
	Hours     string // « 08:00 → 12:00 (4h00) »
	Countdown string
	Windows   []examCountdownItem
}

func (h *handlers) dashExam(_ context.Context, _ auth.User, _ string) (any, error) {
	v := dashExamView{}
	if h.pool == nil {
		return v, nil
	}
	cd := buildExamCountdown(h.pool.AllExamWindows())
	if cd.Focus == nil {
		return v, nil
	}
	v.Available = true
	v.State = cd.State
	v.Label = cd.Focus.Label
	v.Date = cd.Focus.Date
	v.Hours = fmt.Sprintf("%s → %s (%s)", cd.Focus.Start, cd.Focus.End, cd.Focus.Duration)
	v.Windows = cd.Items

	begin, _ := time.Parse(time.RFC3339, cd.Focus.Begin)
	end, _ := time.Parse(time.RFC3339, cd.Focus.Finish)
	switch cd.State {
	case "active":
		v.Countdown = "se termine " + humanUntil(end)
	case "upcoming":
		v.Countdown = humanUntil(begin)
	default:
		v.Countdown = "tous les exams sont passés"
	}
	return v, nil
}

// humanUntil rend un délai lisible : « dans 2 j 05 h », « dans 3 h 12 min »…
// Arrondi à la minute, sinon « dans 1 h 35 min » s'afficherait « 1 h 34 »
// sitôt la première nanoseconde écoulée.
func humanUntil(t time.Time) string {
	d := time.Until(t)
	if d <= 0 {
		return "imminent"
	}
	mins := int(d.Round(time.Minute) / time.Minute)
	switch {
	case mins >= 24*60:
		return fmt.Sprintf("dans %d j %02d h", mins/(24*60), (mins%(24*60))/60)
	case mins >= 60:
		return fmt.Sprintf("dans %d h %02d min", mins/60, mins%60)
	case mins >= 1:
		return fmt.Sprintf("dans %d min", mins)
	default:
		return "dans moins d'une minute"
	}
}

// --- Carte progression personnelle (relevés quotidiens du service pool) ---

type dashProgressView struct {
	Unavailable bool
	Reason      string
	LevelSpark  string
	ScoreSpark  string
	LevelNow    string
	ScoreNow    string
	Days        int
	FirstDate   string
	LastDate    string
}

// dashProgress trace le niveau et le score de l'utilisateur jour par jour, à
// partir des relevés que le serveur enregistre déjà pour le classement —
// aucun appel API.
func (h *handlers) dashProgress(_ context.Context, user auth.User, _ string) (any, error) {
	v := dashProgressView{}
	if h.pool == nil {
		v.Unavailable, v.Reason = true, "Service de classement inactif."
		return v, nil
	}
	month, year, ok := pool.CurrentSession(time.Now())
	if !ok {
		v.Unavailable, v.Reason = true, "Pas de piscine en cours."
		return v, nil
	}

	var levels, scores []int
	for _, snap := range h.pool.History(month, strconv.Itoa(year)) {
		for _, row := range snap.Rows {
			if row.Login != user.Login {
				continue
			}
			levels = append(levels, int(row.Level*100))
			scores = append(scores, row.Score)
			if v.FirstDate == "" {
				v.FirstDate = frSnapDate(snap.Date)
			}
			v.LastDate = frSnapDate(snap.Date)
			break
		}
	}
	if len(levels) < 2 {
		v.Unavailable, v.Reason = true, "Pas encore assez de relevés — la courbe se construit un point par jour."
		return v, nil
	}
	v.LevelSpark = sparkline(levels)
	v.ScoreSpark = sparkline(scores)
	v.LevelNow = fmt.Sprintf("%.2f", float64(levels[len(levels)-1])/100)
	v.ScoreNow = fmtInt(scores[len(scores)-1])
	v.Days = len(levels)
	return v, nil
}

// frSnapDate reformate la clé de relevé « 2026-07-14 » en « mar. 14 juil. ».
func frSnapDate(s string) string {
	if t, err := time.ParseInLocation("2006-01-02", s, time.Local); err == nil {
		return frDateShort(t)
	}
	return s
}

// --- Carte « prêt à rendre » (croisement moulinette locale × intra) ---

type dashReadyRow struct {
	Name       string
	Score      int
	State      string
	StateClass string // wip | wait | ko
}

type dashReadyView struct {
	Rows []dashReadyRow
	Note string
}

// dashReady liste les exercices qui passent la moulinette mais ne sont pas
// (encore) validés à l'intra. Seul /v2/me est consulté — déjà en cache pour
// les autres cartes, donc coût API nul en pratique.
func (h *handlers) dashReady(ctx context.Context, user auth.User, tok string) (any, error) {
	me, err := h.ft.Me(ctx, user.Login, tok)
	if err != nil {
		return nil, err
	}
	v := dashReadyView{}
	if h.queue == nil {
		v.Note = "Moulinette inactive."
		return v, nil
	}

	// Dernier verdict moulinette par exercice.
	type verdict struct {
		passed bool
		score  int
		at     time.Time
	}
	best := map[string]verdict{}
	for _, job := range h.jobsForUser(user) {
		if job.Result == nil || !jobFinished(job.Status) {
			continue
		}
		key := normalizeProjectKey(job.Exercise)
		if cur, ok := best[key]; !ok || job.CreatedAt.After(cur.at) {
			best[key] = verdict{passed: job.Result.Passed, score: job.Result.Score, at: job.CreatedAt}
		}
	}

	type intraState struct {
		validated *bool
		status    string
	}
	intra := map[string]intraState{}
	for _, pu := range me.ProjectsUsers {
		intra[normalizeProjectKey(pu.Project.Name)] = intraState{validated: pu.Validated, status: pu.Status}
	}

	var keys []string
	for k, verd := range best {
		if verd.passed {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		row := dashReadyRow{Name: prettyProjectKey(k), Score: best[k].score}
		it, known := intra[k]
		switch {
		case known && it.validated != nil && *it.validated:
			continue // déjà validé à l'intra : rien à signaler
		case known && it.validated != nil:
			row.State, row.StateClass = "échoué à l'intra — la moulinette passe, retente !", "ko"
		case known && it.status == "waiting_for_correction":
			row.State, row.StateClass = "en attente de correction à l'intra", "wait"
		case known:
			row.State, row.StateClass = "commencé à l'intra, pas encore rendu", "wip"
		default:
			row.State, row.StateClass = "pas encore rendu à l'intra", "wip"
		}
		v.Rows = append(v.Rows, row)
	}
	if len(v.Rows) == 0 {
		v.Note = "Rien en attente : tout ce qui passe la moulinette est déjà validé (ou rien ne passe encore)."
	}
	return v, nil
}

// normalizeProjectKey aligne l'ID moulinette (« c02 ») et le nom de projet
// intra (« C 02 ») sur une même clé.
func normalizeProjectKey(s string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(s), " ", ""))
}

// prettyProjectKey remet une clé normalisée en libellé (« c02 » → « C 02 »).
func prettyProjectKey(k string) string {
	if len(k) >= 2 {
		if head := k[0]; head >= 'a' && head <= 'z' {
			rest := k[1:]
			digitsOnly := true
			for _, r := range rest {
				if r < '0' || r > '9' {
					digitsOnly = false
					break
				}
			}
			if digitsOnly {
				return strings.ToUpper(k[:1]) + " " + rest
			}
		}
	}
	return strings.ToUpper(k[:1]) + k[1:]
}

// --- Carte recherche + fiche pisciner ---

// dashLookup rend juste le formulaire ; la fiche arrive via /ui/user.
func (h *handlers) dashLookup(_ context.Context, _ auth.User, _ string) (any, error) {
	return nil, nil
}

type userCardView struct {
	Err             string
	Login           string
	Displayname     string
	Avatar          string
	Title           string
	CursusName      string
	Level           string
	Validated       int
	CorrectionPoint int
	Wallet          int
	PoolBadge       string
	IntraURL        string
}

// userCard (GET /ui/user?login=x) affiche la fiche publique d'un étudiant.
// Un appel API par login recherché, en cache 15 min.
func (h *handlers) userCard(w http.ResponseWriter, r *http.Request) {
	user, _ := userFromContext(r.Context())
	render := func(v userCardView) {
		if err := h.tmpl.ExecuteTemplate(w, "user_card", v); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}

	target := strings.ToLower(strings.TrimSpace(r.FormValue("login")))
	if !icsLoginRe.MatchString(target) {
		render(userCardView{Err: "Login invalide (lettres minuscules, chiffres, tirets)."})
		return
	}
	tok, err := h.sessions.FreshToken(r, h.oauth)
	if err != nil {
		render(userCardView{Err: "Ta session a expiré — reconnecte-toi."})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	p, err := h.ft.UserProfile(ctx, user.Login, tok.AccessToken, target)
	if err != nil {
		var apiErr *fortytwo.APIError
		if errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound {
			render(userCardView{Err: "Aucun compte « " + target + " » sur l'intra."})
			return
		}
		render(userCardView{Err: "L'API 42 n'a pas répondu — réessaie dans un instant."})
		return
	}

	v := userCardView{
		Login:           p.Login,
		Displayname:     p.Displayname,
		Avatar:          p.Image.Link,
		CorrectionPoint: p.CorrectionPoint,
		Wallet:          p.Wallet,
		IntraURL:        "https://profile.intra.42.fr/users/" + url.PathEscape(p.Login),
	}
	if p.Image.Versions.Medium != "" {
		v.Avatar = p.Image.Versions.Medium
	}
	for _, tu := range p.TitlesUsers {
		if !tu.Selected {
			continue
		}
		for _, t := range p.Titles {
			if t.ID == tu.TitleID {
				v.Title = strings.ReplaceAll(t.Name, "%login", p.Login)
			}
		}
	}
	if cu := bestCursus(p); cu != nil {
		v.CursusName = cu.Cursus.Name
		v.Level = fmt.Sprintf("%.2f", cu.Level)
	}
	for _, pu := range p.ProjectsUsers {
		if pu.Validated != nil && *pu.Validated {
			v.Validated++
		}
	}
	if p.PoolMonth != "" && p.PoolYear != "" {
		v.PoolBadge = "Piscine " + frMonthName(p.PoolMonth) + " " + p.PoolYear
	}
	render(v)
}

// --- Helpers de formatage français ---

var frDaysShort = [7]string{"dim.", "lun.", "mar.", "mer.", "jeu.", "ven.", "sam."}
var frMonthsShort = [12]string{"janv.", "févr.", "mars", "avr.", "mai", "juin", "juil.", "août", "sept.", "oct.", "nov.", "déc."}
var frMonthsFull = [12]string{"janvier", "février", "mars", "avril", "mai", "juin", "juillet", "août", "septembre", "octobre", "novembre", "décembre"}

var frMonthNames = map[string]string{
	"january": "janvier", "february": "février", "march": "mars", "april": "avril",
	"may": "mai", "june": "juin", "july": "juillet", "august": "août",
	"september": "septembre", "october": "octobre", "november": "novembre", "december": "décembre",
}

func frMonthName(en string) string {
	if fr, ok := frMonthNames[strings.ToLower(en)]; ok {
		return fr
	}
	return en
}

func frDateShort(t time.Time) string {
	t = t.Local()
	return fmt.Sprintf("%s %d %s", frDaysShort[t.Weekday()], t.Day(), frMonthsShort[t.Month()-1])
}

// frReason traduit les motifs de points de correction les plus courants.
func frReason(reason string) string {
	switch lower := strings.ToLower(reason); {
	case strings.Contains(lower, "defense plannified"), strings.Contains(lower, "defense planified"):
		return "Défense planifiée"
	case strings.Contains(lower, "earning after defense"):
		return "Gain après une défense"
	case strings.Contains(lower, "defense cancel"):
		return "Défense annulée"
	case strings.Contains(lower, "adjustment"):
		return "Ajustement par le staff"
	case strings.Contains(lower, "pool"):
		return "Échange avec la cagnotte"
	default:
		return reason
	}
}

func fmtHours(h float64) string {
	if h <= 0 {
		return "0h"
	}
	total := int(h*60 + 0.5)
	return fmt.Sprintf("%dh%02d", total/60, total%60)
}

// fmtInt insère une espace fine insécable tous les trois chiffres.
func fmtInt(n int) string {
	s := strconv.Itoa(n)
	neg := false
	if strings.HasPrefix(s, "-") {
		neg, s = true, s[1:]
	}
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteRune(' ')
		}
		b.WriteRune(r)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return strings.TrimSpace(string(runes[:max])) + "…"
}
