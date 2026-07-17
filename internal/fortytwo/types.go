package fortytwo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"time"
)

// Me est le profil complet renvoyé par /v2/me. Seuls les champs affichés par
// le dashboard sont mappés ; le JSON réel en contient bien davantage.
type Me struct {
	ID              int       `json:"id"`
	Login           string    `json:"login"`
	Displayname     string    `json:"displayname"`
	CorrectionPoint int       `json:"correction_point"`
	Wallet          int       `json:"wallet"`
	PoolMonth       string    `json:"pool_month"`
	PoolYear        string    `json:"pool_year"`
	Location        string    `json:"location"` // host du poste si loggé en cluster, sinon null
	CreatedAt       time.Time `json:"created_at"`
	Image           struct {
		Link     string `json:"link"`
		Versions struct {
			Medium string `json:"medium"`
		} `json:"versions"`
	} `json:"image"`
	Campus []struct {
		ID      int    `json:"id"`
		Name    string `json:"name"`
		Country string `json:"country"`
	} `json:"campus"`
	CampusUsers []struct {
		CampusID  int  `json:"campus_id"`
		IsPrimary bool `json:"is_primary"`
	} `json:"campus_users"`
	CursusUsers   []CursusUser  `json:"cursus_users"`
	ProjectsUsers []ProjectUser `json:"projects_users"`
	Achievements  []Achievement `json:"achievements"`
	Titles        []struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"titles"`
	TitlesUsers []struct {
		TitleID  int  `json:"title_id"`
		Selected bool `json:"selected"`
	} `json:"titles_users"`
}

type CursusUser struct {
	Grade   *string   `json:"grade"`
	Level   float64   `json:"level"`
	BeginAt time.Time `json:"begin_at"`
	Skills  []Skill   `json:"skills"`
	Cursus  struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
		Slug string `json:"slug"`
	} `json:"cursus"`
}

type Skill struct {
	Name  string  `json:"name"`
	Level float64 `json:"level"`
}

type ProjectUser struct {
	ID        int        `json:"id"`
	FinalMark *int       `json:"final_mark"`
	Status    string     `json:"status"`
	Validated *bool      `json:"validated?"`
	CursusIDs []int      `json:"cursus_ids"`
	MarkedAt  *time.Time `json:"marked_at"`
	CreatedAt time.Time  `json:"created_at"`
	Project   struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
	} `json:"project"`
}

type Achievement struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Tier        string `json:"tier"`
	Kind        string `json:"kind"`
}

type Coalition struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Color    string `json:"color"`
	Score    int    `json:"score"`
	ImageURL string `json:"image_url"`
}

type CoalitionUser struct {
	ID          int `json:"id"`
	CoalitionID int `json:"coalition_id"`
	UserID      int `json:"user_id"`
	Score       int `json:"score"`
	Rank        int `json:"rank"`
}

type PointHistoric struct {
	Sum       int       `json:"sum"`
	Total     *int      `json:"total"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"created_at"`
}

// ScaleUser tolère les deux formes renvoyées pour un participant
// d'évaluation : un objet {"login": …} ou la chaîne "invisible" (anonymisé).
type ScaleUser struct{ Login string }

func (s *ScaleUser) UnmarshalJSON(b []byte) error {
	var v struct {
		Login string `json:"login"`
	}
	if err := json.Unmarshal(b, &v); err == nil {
		s.Login = v.Login
	}
	return nil // chaîne "invisible" ou forme inattendue : login vide
}

// ScaleUsers tolère de même un tableau d'objets ou la chaîne "invisible".
type ScaleUsers []ScaleUser

func (s *ScaleUsers) UnmarshalJSON(b []byte) error {
	var arr []ScaleUser
	if err := json.Unmarshal(b, &arr); err == nil {
		*s = arr
	}
	return nil
}

type ScaleTeam struct {
	ID         int        `json:"id"`
	FinalMark  *int       `json:"final_mark"`
	Comment    *string    `json:"comment"`
	Feedback   *string    `json:"feedback"`
	BeginAt    time.Time  `json:"begin_at"`
	FilledAt   *time.Time `json:"filled_at"`
	Corrector  ScaleUser  `json:"corrector"`
	Correcteds ScaleUsers `json:"correcteds"`
	Flag       struct {
		Name     string `json:"name"`
		Positive bool   `json:"positive"`
	} `json:"flag"`
	Team struct {
		Name      string `json:"name"`
		ProjectID int    `json:"project_id"`
	} `json:"team"`
}

type Event struct {
	Name     string    `json:"name"`
	Kind     string    `json:"kind"`
	Location string    `json:"location"`
	BeginAt  time.Time `json:"begin_at"`
	EndAt    time.Time `json:"end_at"`
}

// Slot est un créneau de disponibilité de correcteur (granule de 15 min côté
// intra). Un slot réservé porte le scale_team de la défense.
type Slot struct {
	ID        int         `json:"id"`
	BeginAt   time.Time   `json:"begin_at"`
	EndAt     time.Time   `json:"end_at"`
	ScaleTeam SlotBooking `json:"scale_team"`
}

// SlotBooking tolère les trois formes de scale_team d'un slot : null (slot
// libre), la chaîne "invisible" (réservé, identité pas encore révélée) et
// l'objet complet (réservé, révélé ~15 min avant la défense).
type SlotBooking struct {
	Present    bool // le slot est réservé
	ID         int
	BeginAt    time.Time
	Correcteds ScaleUsers
}

func (b *SlotBooking) UnmarshalJSON(raw []byte) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	b.Present = true
	if raw[0] == '"' {
		return nil // "invisible"
	}
	var v struct {
		ID         int        `json:"id"`
		BeginAt    time.Time  `json:"begin_at"`
		Correcteds ScaleUsers `json:"correcteds"`
	}
	if err := json.Unmarshal(raw, &v); err == nil {
		b.ID, b.BeginAt, b.Correcteds = v.ID, v.BeginAt, v.Correcteds
	}
	return nil
}

// Me récupère le profil du token - la source de la moitié des cartes du
// dashboard (héro, projets, skills, succès), d'où son cache partagé.
func (c *Client) Me(ctx context.Context, login, tok string) (*Me, error) {
	var me Me
	if err := c.get(ctx, login, tok, "/v2/me", nil, 3*time.Minute, &me); err != nil {
		return nil, err
	}
	return &me, nil
}

// LocationsStats renvoie le temps de présence par jour ("2026-07-14" →
// "05:47:23.366"), borné à [from, to].
func (c *Client) LocationsStats(ctx context.Context, login, tok string, userID int, from, to time.Time) (map[string]string, error) {
	params := url.Values{}
	params.Set("begin_at", from.Format("2006-01-02"))
	params.Set("end_at", to.Format("2006-01-02"))
	out := map[string]string{}
	err := c.get(ctx, login, tok, fmt.Sprintf("/v2/users/%d/locations_stats", userID), params, 5*time.Minute, &out)
	return out, err
}

func (c *Client) Coalitions(ctx context.Context, login, tok string, userID int) ([]Coalition, error) {
	var out []Coalition
	err := c.get(ctx, login, tok, fmt.Sprintf("/v2/users/%d/coalitions", userID), nil, 15*time.Minute, &out)
	return out, err
}

func (c *Client) CoalitionUsers(ctx context.Context, login, tok string, userID int) ([]CoalitionUser, error) {
	var out []CoalitionUser
	err := c.get(ctx, login, tok, fmt.Sprintf("/v2/users/%d/coalitions_users", userID), nil, 5*time.Minute, &out)
	return out, err
}

func (c *Client) PointHistorics(ctx context.Context, login, tok string, userID int) ([]PointHistoric, error) {
	params := url.Values{}
	params.Set("sort", "-created_at")
	params.Set("page[size]", "100")
	var out []PointHistoric
	err := c.get(ctx, login, tok, fmt.Sprintf("/v2/users/%d/correction_point_historics", userID), params, 5*time.Minute, &out)
	return out, err
}

// ScaleTeams liste les évaluations de l'utilisateur ; side vaut
// "as_corrected" (il a été noté) ou "as_corrector" (il a noté).
func (c *Client) ScaleTeams(ctx context.Context, login, tok string, userID int, side string) ([]ScaleTeam, error) {
	params := url.Values{}
	params.Set("sort", "-begin_at")
	params.Set("page[size]", "6")
	var out []ScaleTeam
	err := c.get(ctx, login, tok, fmt.Sprintf("/v2/users/%d/scale_teams/%s", userID, side), params, 5*time.Minute, &out)
	return out, err
}

func (c *Client) Events(ctx context.Context, login, tok string, userID int) ([]Event, error) {
	params := url.Values{}
	params.Set("sort", "-begin_at")
	params.Set("page[size]", "50")
	var out []Event
	err := c.get(ctx, login, tok, fmt.Sprintf("/v2/users/%d/events", userID), params, 15*time.Minute, &out)
	return out, err
}

// MeScaleTeams liste les évaluations où l'utilisateur est correcteur (le
// « Pending evaluations » de l'intra). Le TTL arbitre entre fraîcheur (le
// corrigé se révèle ~15 min avant la défense) et quota API (1200 req/h pour
// toute l'app) : 100 s + un polling client à 2 min ≈ 30 req/h par onglet.
func (c *Client) MeScaleTeams(ctx context.Context, login, tok string) ([]ScaleTeam, error) {
	params := url.Values{}
	params.Set("page[size]", "20")
	var out []ScaleTeam
	err := c.get(ctx, login, tok, "/v2/me/scale_teams", params, 100*time.Second, &out)
	return out, err
}

// Slots renvoie les créneaux de l'utilisateur dont le début tombe dans
// [from, to]. TTL 90 s : assez frais pour voir arriver les réservations,
// assez long pour ménager le quota.
func (c *Client) Slots(ctx context.Context, login, tok string, from, to time.Time) ([]Slot, error) {
	params := url.Values{}
	params.Set("range[begin_at]", from.UTC().Format(time.RFC3339)+","+to.UTC().Format(time.RFC3339))
	params.Set("page[size]", "100")
	var out []Slot
	err := c.get(ctx, login, tok, "/v2/me/slots", params, 90*time.Second, &out)
	return out, err
}

// CoalitionTop renvoie les meilleurs membres d'une coalition. L'API 42 ne
// sait pas trier coalitions_users par score (« The score field is not
// sortable ») : on récupère jusqu'à 3 pages de 100 et on trie nous-mêmes —
// exact pour des coalitions de campus, et chaque page est en cache 15 min.
func (c *Client) CoalitionTop(ctx context.Context, login, tok string, coalitionID, count int) ([]CoalitionUser, error) {
	var all []CoalitionUser
	for page := 1; page <= 3; page++ {
		params := url.Values{}
		params.Set("page[size]", "100")
		params.Set("page[number]", strconv.Itoa(page))
		var chunk []CoalitionUser
		if err := c.get(ctx, login, tok, fmt.Sprintf("/v2/coalitions/%d/coalitions_users", coalitionID), params, 15*time.Minute, &chunk); err != nil {
			if page == 1 {
				return nil, err
			}
			break // la première page suffit pour un top raisonnable
		}
		all = append(all, chunk...)
		if len(chunk) < 100 {
			break
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Score > all[j].Score })
	if len(all) > count {
		all = all[:count]
	}
	return all, nil
}

// UserProfile renvoie le profil public d'un autre étudiant, par login.
// L'endpoint /v2/users accepte le login comme identifiant. TTL long : une
// fiche de pisciner ne bouge pas à la minute et chaque recherche coûte un
// appel du quota.
func (c *Client) UserProfile(ctx context.Context, login, tok, target string) (*Me, error) {
	var me Me
	err := c.get(ctx, login, tok, "/v2/users/"+url.PathEscape(target), nil, 15*time.Minute, &me)
	if err != nil {
		return nil, err
	}
	return &me, nil
}

// CreateSlot poste une disponibilité [begin, end] ; l'intra la découpe en
// granules de 15 min et renvoie les slots créés (leurs ids servent au client
// pour un futur déplacement/suppression). Le cache des slots est invalidé.
func (c *Client) CreateSlot(ctx context.Context, login, tok string, userID int, begin, end time.Time) ([]Slot, error) {
	form := url.Values{}
	form.Set("slot[user_id]", strconv.Itoa(userID))
	form.Set("slot[begin_at]", begin.UTC().Format(time.RFC3339))
	form.Set("slot[end_at]", end.UTC().Format(time.RFC3339))
	body, err := c.mutate(ctx, login, tok, http.MethodPost, "/v2/slots", form, "/v2/me/slots")
	if err != nil {
		return nil, err
	}
	// 42 renvoie un tableau pour une plage, parfois un objet seul pour une
	// granule unique : on tolère les deux formes.
	var created []Slot
	if err := json.Unmarshal(body, &created); err != nil {
		var one Slot
		if json.Unmarshal(body, &one) == nil && one.ID != 0 {
			created = []Slot{one}
		}
	}
	return created, nil
}

// DeleteSlot supprime un slot (l'API 42 refuse d'elle-même les slots réservés).
// Un 404 est traité comme un succès : la granule n'existe déjà plus côté 42 —
// soit l'id était périmé, soit 42 a supprimé des granules sœurs en cascade —
// et dans les deux cas l'état visé (« ce créneau n'existe plus ») est atteint.
func (c *Client) DeleteSlot(ctx context.Context, login, tok string, slotID int) error {
	_, err := c.mutate(ctx, login, tok, http.MethodDelete, fmt.Sprintf("/v2/slots/%d", slotID), nil, "/v2/me/slots")
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound {
		return nil
	}
	return err
}

// Project renvoie le nom d'un projet - pour libeller les défenses à venir.
// Peu de projets distincts pendant une piscine : cache long.
func (c *Client) Project(ctx context.Context, login, tok string, id int) (string, error) {
	var out struct {
		Name string `json:"name"`
	}
	err := c.get(ctx, login, tok, fmt.Sprintf("/v2/projects/%d", id), nil, 24*time.Hour, &out)
	return out.Name, err
}
