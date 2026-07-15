package fortytwo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
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
// « Pending evaluations » de l'intra). TTL court : l'identité du corrigé se
// révèle environ 15 minutes avant la défense, la carte doit la voir vite.
func (c *Client) MeScaleTeams(ctx context.Context, login, tok string) ([]ScaleTeam, error) {
	params := url.Values{}
	params.Set("page[size]", "20")
	var out []ScaleTeam
	err := c.get(ctx, login, tok, "/v2/me/scale_teams", params, 45*time.Second, &out)
	return out, err
}

// Slots renvoie les créneaux de l'utilisateur dont le début tombe dans
// [from, to] - TTL court, l'agenda doit reflèter vite les réservations.
func (c *Client) Slots(ctx context.Context, login, tok string, from, to time.Time) ([]Slot, error) {
	params := url.Values{}
	params.Set("range[begin_at]", from.UTC().Format(time.RFC3339)+","+to.UTC().Format(time.RFC3339))
	params.Set("page[size]", "100")
	var out []Slot
	err := c.get(ctx, login, tok, "/v2/me/slots", params, 30*time.Second, &out)
	return out, err
}

// CreateSlot poste une disponibilité [begin, end] ; l'intra la découpe en
// granules de 15 min. Le cache des slots est invalidé pour que le calendrier
// re-rendu juste après voie le nouveau créneau.
func (c *Client) CreateSlot(ctx context.Context, login, tok string, userID int, begin, end time.Time) error {
	form := url.Values{}
	form.Set("slot[user_id]", strconv.Itoa(userID))
	form.Set("slot[begin_at]", begin.UTC().Format(time.RFC3339))
	form.Set("slot[end_at]", end.UTC().Format(time.RFC3339))
	_, err := c.mutate(ctx, login, tok, http.MethodPost, "/v2/slots", form, "/v2/me/slots")
	return err
}

// DeleteSlot supprime un slot (l'API 42 refuse d'elle-même les slots réservés).
func (c *Client) DeleteSlot(ctx context.Context, login, tok string, slotID int) error {
	_, err := c.mutate(ctx, login, tok, http.MethodDelete, fmt.Sprintf("/v2/slots/%d", slotID), nil, "/v2/me/slots")
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
