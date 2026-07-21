package fortytwo

import "time"

// ---------------------------------------------------------------------------
// Formes brutes de l'API v2 de 42 (décodage JSON). On ne mappe que les champs
// exploités par le dashboard ; l'API en renvoie beaucoup d'autres, ignorés.
// ---------------------------------------------------------------------------

type apiUser struct {
	ID    int    `json:"id"`
	Login string `json:"login"`
	Image struct {
		Link string `json:"link"`
	} `json:"image"`
}

type apiCursus struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type apiCursusUser struct {
	Level     float64   `json:"level"`
	Grade     string    `json:"grade"`
	CursusID  int       `json:"cursus_id"`
	Cursus    apiCursus `json:"cursus"`
	BeginAt   time.Time `json:"begin_at"`
	BlackHole time.Time `json:"blackholed_at"`
}

type apiProject struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Slug     string `json:"slug"`
	ParentID *int   `json:"parent_id"`
}

type apiTeam struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	RepoURL  string `json:"repo_url"`
	RepoUUID string `json:"repo_uuid"`
}

type apiProjectUser struct {
	ID        int        `json:"id"`
	FinalMark *int       `json:"final_mark"`
	Status    string     `json:"status"`
	Validated *bool      `json:"validated?"`
	Project   apiProject `json:"project"`
	CursusIDs []int      `json:"cursus_ids"`
	MarkedAt  time.Time  `json:"marked_at"`
	Teams     []apiTeam  `json:"teams"`
}

// apiMe est le sous-ensemble de /v2/me utilisé pour le hero (le profil complet
// arrive avec le scope « public » ; niveau et cursus exigent au moins ce scope).
type apiMe struct {
	ID    int    `json:"id"`
	Login string `json:"login"`
	Email string `json:"email"`
	Image struct {
		Link string `json:"link"`
	} `json:"image"`
	Location        string           `json:"location"`
	Wallet          int              `json:"wallet"`
	CorrectionPoint int              `json:"correction_point"`
	CursusUsers     []apiCursusUser  `json:"cursus_users"`
	ProjectsUsers   []apiProjectUser `json:"projects_users"`
}

type apiCoalition struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Slug     string `json:"slug"`
	Color    string `json:"color"`
	CoverURL string `json:"cover_url"`
}

// apiScaleTeam est une évaluation (correction) : qui corrige qui, la note, le
// commentaire, le créneau, l'équipe corrigée (dont le repo) et le projet.
type apiScaleTeam struct {
	ID         int       `json:"id"`
	Comment    string    `json:"comment"`
	FinalMark  *int      `json:"final_mark"`
	CreatedAt  time.Time `json:"created_at"`
	BeginAt    time.Time `json:"begin_at"`
	Corrector  apiUser   `json:"corrector"`
	Correcteds []apiUser `json:"correcteds"`
	Team       struct {
		ID        int        `json:"id"`
		Name      string     `json:"name"`
		ProjectID int        `json:"project_id"`
		RepoURL   string     `json:"repo_url"`
		Project   apiProject `json:"project"`
	} `json:"team"`
	Scale struct {
		ID int `json:"id"`
	} `json:"scale"`
	Feedbacks []struct {
		ID      int    `json:"id"`
		Comment string `json:"comment"`
		Rating  int    `json:"rating"`
	} `json:"feedbacks"`
}

// ---------------------------------------------------------------------------
// Modèles de vue consommés par les templates du dashboard.
// ---------------------------------------------------------------------------

// Profile alimente le hero du dashboard.
type Profile struct {
	ID               int
	Login            string
	ImageURL         string
	CursusID         int
	CursusName       string
	Level            float64
	Grade            string
	Wallet           int
	CorrectionPoints int
	Location         string
	CoalitionName    string
	CoalitionColor   string
	BlackHoleIn      int // jours restants avant black hole (0 si non concerné)
	HasBlackHole     bool
}

// ProjectItem est une tuile de l'explorateur de projets.
type ProjectItem struct {
	ProjectID  int
	Name       string
	Slug       string
	Status     string // "", "in_progress", "waiting_for_correction", "finished"…
	Validated  bool
	FinalMark  int
	HasMark    bool
	Registered bool
	RepoURL    string // présent une fois inscrit → bouton « copier »
	SubjectURL string
}

// ProjectCategory regroupe les projets par catégorie (nom lisible).
type ProjectCategory struct {
	Name  string
	Items []ProjectItem
}

// Correction est une évaluation reçue (le correcteur nous a corrigé).
type Correction struct {
	ScaleTeamID    int
	ProjectName    string
	CorrectorLogin string
	CorrectorImage string
	FinalMark      int
	HasMark        bool
	Comment        string
	When           time.Time
	Validated      bool
	FeedbackSent   bool
}

// Defense est un créneau où l'on doit corriger quelqu'un.
type Defense struct {
	ScaleTeamID     int
	ProjectName     string
	ProjectSlug     string
	CorrectedLogins []string
	BeginAt         time.Time
	SubjectURL      string // lien vers le sujet de correction
	Revealed        bool   // le corrigé est-il déjà dévoilé (≤15 min) ?
}
