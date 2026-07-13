package models

import (
	"time"
)

type Status string

const (
	StatusPending Status = "pending"
	StatusRunning Status = "running"
	StatusPassed  Status = "passed"
	StatusFailed  Status = "failed"
	StatusError   Status = "error"
)

// Progress reflète l'avancement d'un job : Done exercices traités sur Total.
type Progress struct {
	Done  int `json:"done"`
	Total int `json:"total"`
}

// Job est une demande de correction : une archive uploadée ou une URL git.
// Exercise contient l'ID du projet, qui peut regrouper plusieurs
// exercices.
type Job struct {
	ID           string     `json:"id"`
	RepoURL      string     `json:"repo_url,omitempty"`
	GitToken     string     `json:"-"`        // jeton d'accès pour un dépôt privé ; jamais exposé ni persisté
	ArchivePath  string     `json:"-"`        // chemin serveur, jamais exposé au client
	SourceLabel  string     `json:"source"`   // nom de fichier ou URL, affiché dans l'UI
	Owner        string     `json:"owner"`    // login 42
	Exercise     string     `json:"exercise"` // ID du projet
	Status       Status     `json:"status"`
	Progress     *Progress  `json:"progress,omitempty"`
	Result       *Result    `json:"result,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	EndedAt      *time.Time `json:"ended_at,omitempty"`
	ServerBootID string     `json:"-"` // démarrage serveur ayant créé le job, jamais exposé
}

// ProgressPercent renvoie l'avancement en pourcentage (0 si inconnu).
func (j Job) ProgressPercent() int {
	if j.Progress == nil || j.Progress.Total == 0 {
		return 0
	}
	return j.Progress.Done * 100 / j.Progress.Total
}

// Result est le verdict global. Passed signifie Score >= 50
// Error n'est renseigné qu'en cas d'échec avant tout test
type Result struct {
	Passed          bool             `json:"passed"`
	Score           int              `json:"score"`
	ExerciseResults []ExerciseResult `json:"exercises"`
	Error           string           `json:"error,omitempty"`
}

// ExerciseStatus reprend le vocabulaire de verdict de la vraie moulinette 42.
type ExerciseStatus string

const (
	ExerciseOK           ExerciseStatus = "ok"
	ExerciseKO           ExerciseStatus = "ko"
	ExerciseMissing      ExerciseStatus = "missing"
	ExerciseCompileError ExerciseStatus = "compile_error"
	ExerciseTimeout      ExerciseStatus = "timeout"
	ExerciseNormeError   ExerciseStatus = "norme_error"
	ExerciseCheating     ExerciseStatus = "cheating"
	ExerciseLeak         ExerciseStatus = "leak"
)

func (s ExerciseStatus) Label() string {
	switch s {
	case ExerciseOK:
		return "OK"
	case ExerciseKO:
		return "KO"
	case ExerciseMissing:
		return "Nothing turned in"
	case ExerciseCompileError:
		return "Does not compile"
	case ExerciseTimeout:
		return "Timeout reached"
	case ExerciseNormeError:
		return "Norme error"
	case ExerciseCheating:
		return "Cheating"
	case ExerciseLeak:
		return "Memory leak"
	default:
		return string(s)
	}
}

// CSSClass regroupe les statuts par couleur : vert (ok), rouge (échec franc),
// orange (cas intermédiaires).
func (s ExerciseStatus) CSSClass() string {
	switch s {
	case ExerciseOK:
		return "pass"
	case ExerciseKO, ExerciseCompileError, ExerciseCheating:
		return "fail"
	case ExerciseMissing, ExerciseTimeout, ExerciseNormeError, ExerciseLeak:
		return "warn"
	default:
		return "fail"
	}
}

type ExerciseResult struct {
	Name       string         `json:"name"`
	Status     ExerciseStatus `json:"status"`
	TestOutput []TestResult   `json:"tests,omitempty"`
	Log        string         `json:"log,omitempty"`
}

type TestResult struct {
	Name     string `json:"name"`
	Passed   bool   `json:"passed"`
	Expected string `json:"expected,omitempty"`
	Got      string `json:"got,omitempty"`
	Error    string `json:"error,omitempty"`
	LeakLog  string `json:"leak_log,omitempty"`
}
