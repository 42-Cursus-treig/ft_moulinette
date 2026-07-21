package fortytwo

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"time"
)

// intraProjectsBase sert à construire le lien vers le sujet de correction d'un
// projet (page projet sur l'intra). Indépendant de l'API : c'est l'intra web.
const intraProjectsBase = "https://projects.intra.42.fr/projects/"

// Service porte la logique métier du dashboard au-dessus du Client bas niveau.
// Chaque méthode reçoit l'access token de l'utilisateur courant.
type Service struct {
	c *Client
}

// NewService construit le service en visant baseURL (API 42 ou mock).
func NewService(baseURL string) *Service {
	return &Service{c: NewClient(baseURL)}
}

// SubjectURL renvoie le lien vers le sujet/page d'un projet sur l'intra.
func SubjectURL(slug string) string {
	if slug == "" {
		return ""
	}
	return intraProjectsBase + slug
}

// Profile récupère le hero : identité, cursus principal, niveau, coalition.
func (s *Service) Profile(ctx context.Context, token string) (Profile, error) {
	var me apiMe
	if err := s.c.get(ctx, token, "/v2/me", nil, &me); err != nil {
		return Profile{}, err
	}

	p := Profile{
		ID:               me.ID,
		Login:            me.Login,
		ImageURL:         me.Image.Link,
		Wallet:           me.Wallet,
		CorrectionPoints: me.CorrectionPoint,
		Location:         me.Location,
	}

	if cu := primaryCursus(me.CursusUsers); cu != nil {
		p.CursusID = cu.Cursus.ID
		p.CursusName = cu.Cursus.Name
		p.Level = cu.Level
		p.Grade = cu.Grade
		if !cu.BlackHole.IsZero() && cu.BlackHole.After(time.Now()) {
			p.HasBlackHole = true
			p.BlackHoleIn = int(time.Until(cu.BlackHole).Hours() / 24)
		}
	}

	// Coalition : best-effort, on n'échoue pas le hero si l'appel casse.
	var coals []apiCoalition
	if err := s.c.get(ctx, token, fmt.Sprintf("/v2/users/%d/coalitions", me.ID), nil, &coals); err == nil && len(coals) > 0 {
		p.CoalitionName = coals[0].Name
		p.CoalitionColor = coals[0].Color
	}
	return p, nil
}

// primaryCursus choisit le cursus « principal » : celui de plus haut niveau
// (heuristique simple et stable — un étudiant a souvent piscine + cursus).
func primaryCursus(cus []apiCursusUser) *apiCursusUser {
	if len(cus) == 0 {
		return nil
	}
	best := 0
	for i := 1; i < len(cus); i++ {
		if cus[i].Level > cus[best].Level {
			best = i
		}
	}
	return &cus[best]
}

// Projects renvoie les projets de l'utilisateur regroupés par catégorie de
// statut (en cours / en attente / terminés), avec le repo une fois inscrit.
// Fiable et peu coûteux : tout vient de /v2/me (projects_users).
func (s *Service) Projects(ctx context.Context, token string) ([]ProjectCategory, error) {
	// projects_users avec les teams (repo_url) : /v2/me n'embarque pas toujours
	// les teams, on passe donc par la collection dédiée.
	var me apiMe
	if err := s.c.get(ctx, token, "/v2/me", nil, &me); err != nil {
		return nil, err
	}
	pus, err := getAll[apiProjectUser](ctx, s.c, token, fmt.Sprintf("/v2/users/%d/projects_users", me.ID), nil)
	if err != nil {
		// Dégradation : au pire on retombe sur ce que /v2/me embarque.
		pus = me.ProjectsUsers
	}

	inProgress := ProjectCategory{Name: "En cours"}
	waiting := ProjectCategory{Name: "En attente de correction"}
	done := ProjectCategory{Name: "Terminés"}

	for _, pu := range pus {
		item := projectItem(pu)
		switch pu.Status {
		case "in_progress", "creating_group", "searching_a_group":
			inProgress.Items = append(inProgress.Items, item)
		case "waiting_for_correction":
			waiting.Items = append(waiting.Items, item)
		case "finished":
			done.Items = append(done.Items, item)
		default:
			// Statut inconnu : on le classe en cours plutôt que de le perdre.
			inProgress.Items = append(inProgress.Items, item)
		}
	}

	cats := make([]ProjectCategory, 0, 3)
	for _, c := range []ProjectCategory{inProgress, waiting, done} {
		if len(c.Items) > 0 {
			sort.Slice(c.Items, func(i, j int) bool { return c.Items[i].Name < c.Items[j].Name })
			cats = append(cats, c)
		}
	}
	return cats, nil
}

func projectItem(pu apiProjectUser) ProjectItem {
	item := ProjectItem{
		ProjectID:  pu.Project.ID,
		Name:       pu.Project.Name,
		Slug:       pu.Project.Slug,
		Status:     pu.Status,
		Registered: true,
		SubjectURL: SubjectURL(pu.Project.Slug),
	}
	if pu.Validated != nil {
		item.Validated = *pu.Validated
	}
	if pu.FinalMark != nil {
		item.FinalMark = *pu.FinalMark
		item.HasMark = true
	}
	// Repo : dernière team qui en porte un (la plus récente inscription).
	for i := len(pu.Teams) - 1; i >= 0; i-- {
		if pu.Teams[i].RepoURL != "" {
			item.RepoURL = pu.Teams[i].RepoURL
			break
		}
	}
	return item
}

// AvailableProjects liste le catalogue de projets d'un cursus non encore
// commencés par l'utilisateur (catégorie « Disponibles »). Best-effort et
// borné : un catalogue vide/inaccessible renvoie simplement une liste vide.
func (s *Service) AvailableProjects(ctx context.Context, token string, cursusID int, done map[int]bool) (ProjectCategory, error) {
	cat := ProjectCategory{Name: "Disponibles"}
	if cursusID == 0 {
		return cat, nil
	}
	projs, err := getAll[apiProject](ctx, s.c, token, fmt.Sprintf("/v2/cursus/%d/projects", cursusID), url.Values{})
	if err != nil {
		return cat, err
	}
	for _, p := range projs {
		if done[p.ID] || p.ParentID != nil { // on masque les sous-projets
			continue
		}
		cat.Items = append(cat.Items, ProjectItem{
			ProjectID:  p.ID,
			Name:       p.Name,
			Slug:       p.Slug,
			SubjectURL: SubjectURL(p.Slug),
		})
	}
	sort.Slice(cat.Items, func(i, j int) bool { return cat.Items[i].Name < cat.Items[j].Name })
	return cat, nil
}

// Register inscrit l'utilisateur à un projet (POST /v2/projects_users). Écriture
// réelle sur l'intra : exige un scope OAuth adéquat côté app 42.
func (s *Service) Register(ctx context.Context, token string, projectID, userID int) error {
	payload := map[string]any{
		"projects_user": map[string]any{
			"project_id": projectID,
			"user_id":    userID,
		},
	}
	return s.c.postJSON(ctx, token, "/v2/projects_users", payload, nil)
}

// CorrectionsReceived renvoie l'historique des corrections reçues (l'utilisateur
// était le corrigé), du plus récent au plus ancien.
func (s *Service) CorrectionsReceived(ctx context.Context, token string, userID int) ([]Correction, error) {
	sts, err := getAll[apiScaleTeam](ctx, s.c, token, fmt.Sprintf("/v2/users/%d/scale_teams", userID), nil)
	if err != nil {
		return nil, err
	}
	var out []Correction
	for _, st := range sts {
		if !isCorrected(st, userID) { // garder seulement là où on était corrigé
			continue
		}
		c := Correction{
			ScaleTeamID:    st.ID,
			ProjectName:    projectName(st),
			CorrectorLogin: st.Corrector.Login,
			CorrectorImage: st.Corrector.Image.Link,
			Comment:        st.Comment,
			When:           correctionTime(st),
			FeedbackSent:   len(st.Feedbacks) > 0,
		}
		if st.FinalMark != nil {
			c.FinalMark = *st.FinalMark
			c.HasMark = true
			c.Validated = *st.FinalMark >= 50
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].When.After(out[j].When) })
	return out, nil
}

// SendFeedback envoie un feedback à son correcteur (POST). Écriture réelle.
func (s *Service) SendFeedback(ctx context.Context, token string, scaleTeamID, rating int, comment string) error {
	payload := map[string]any{
		"feedback": map[string]any{
			"scale_team_id": scaleTeamID,
			"comment":       comment,
			"rating":        rating,
		},
	}
	return s.c.postJSON(ctx, token, "/v2/feedbacks", payload, nil)
}

// UpcomingDefenses renvoie les créneaux à venir où l'utilisateur doit corriger
// (il est le correcteur), avec le lien vers le sujet de correction.
func (s *Service) UpcomingDefenses(ctx context.Context, token string, userID int) ([]Defense, error) {
	params := url.Values{}
	params.Set("filter[future]", "true")
	sts, err := getAll[apiScaleTeam](ctx, s.c, token, fmt.Sprintf("/v2/users/%d/scale_teams", userID), params)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	var out []Defense
	for _, st := range sts {
		if st.Corrector.ID != userID { // seulement les défenses où JE corrige
			continue
		}
		if !st.BeginAt.IsZero() && st.BeginAt.Before(now) {
			continue
		}
		d := Defense{
			ScaleTeamID: st.ID,
			ProjectName: projectName(st),
			ProjectSlug: st.Team.Project.Slug,
			BeginAt:     st.BeginAt,
			SubjectURL:  SubjectURL(st.Team.Project.Slug),
			// 42 ne dévoile le corrigé que ~15 min avant le créneau.
			Revealed: !st.BeginAt.IsZero() && time.Until(st.BeginAt) <= 15*time.Minute,
		}
		for _, u := range st.Correcteds {
			d.CorrectedLogins = append(d.CorrectedLogins, u.Login)
		}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].BeginAt.Before(out[j].BeginAt) })
	return out, nil
}

// --- helpers ---

func isCorrected(st apiScaleTeam, userID int) bool {
	for _, u := range st.Correcteds {
		if u.ID == userID {
			return true
		}
	}
	return false
}

func projectName(st apiScaleTeam) string {
	if st.Team.Project.Name != "" {
		return st.Team.Project.Name
	}
	if st.Team.Name != "" {
		return st.Team.Name
	}
	return "projet #" + strconv.Itoa(st.Team.ProjectID)
}

func correctionTime(st apiScaleTeam) time.Time {
	if !st.BeginAt.IsZero() {
		return st.BeginAt
	}
	return st.CreatedAt
}
