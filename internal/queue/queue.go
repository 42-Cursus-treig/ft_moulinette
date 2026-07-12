// Package queue implémente une file de jobs en mémoire, avec persistance
// disque optionnelle (voir internal/history) pour survivre à un
// redémarrage du serveur.
package queue

import (
	"log"
	"sort"
	"sync"
	"time"

	"github.com/tristan-reig/ft-moulinette/internal/history"
	"github.com/tristan-reig/ft-moulinette/internal/models"
)

// Handler exécute un job (typiquement : sandbox.Run) et renvoie le résultat.
// report doit être appelé au fil de l'exécution (ex: après chaque exercice)
// pour que la progression soit visible en temps réel côté UI.
type Handler func(job models.Job, report func(done, total int)) (*models.Result, error)

type Queue struct {
	jobs    chan models.Job
	handler Handler
	workers int
	wg      sync.WaitGroup
	history *history.Store // peut être nil : persistance alors désactivée

	mu    sync.RWMutex
	store map[string]*models.Job // suivi de l'état de chaque job, par ID
}

func New(workers int, handler Handler, hist *history.Store) *Queue {
	return &Queue{
		jobs:    make(chan models.Job, 100),
		handler: handler,
		workers: workers,
		history: hist,
		store:   make(map[string]*models.Job),
	}
}

// LoadHistory recharge les jobs persistés sur disque dans la file en
// mémoire - à appeler une fois au démarrage, avant Start(). Avec le nouveau
// modèle de persistance (un flush groupé par utilisateur, uniquement pour
// les jobs déjà terminés), un job encore pending/running au moment d'un
// crash n'a jamais été écrit sur disque : il n'y a donc rien à "marquer en
// erreur" ici, contrairement à l'ancien système qui persistait chaque
// changement de statut.
func (q *Queue) LoadHistory() error {
	if q.history == nil {
		return nil
	}
	jobs, err := q.history.LoadAll()
	if err != nil {
		return err
	}

	q.mu.Lock()
	defer q.mu.Unlock()
	for _, job := range jobs {
		j := job
		q.store[j.ID] = &j
	}
	return nil
}

// FlushSession force l'écriture immédiate de l'historique en attente d'un
// utilisateur - appelé à la déconnexion ou à la fermeture du site (best
// effort côté navigateur, voir handlers.closeSession).
func (q *Queue) FlushSession(login string) error {
	if q.history == nil {
		return nil
	}
	return q.history.FlushSession(login)
}

// FlushAllSessions force l'écriture de tout l'historique en attente, tous
// utilisateurs confondus - utilisé à l'arrêt propre du serveur pour limiter
// le risque de perte par rapport à une coupure brutale.
func (q *Queue) FlushAllSessions() error {
	if q.history == nil {
		return nil
	}
	return q.history.FlushAll()
}

// Start lance les workers. Chacun boucle sur le channel jobs.
func (q *Queue) Start() {
	for i := 0; i < q.workers; i++ {
		q.wg.Add(1)
		go q.worker()
	}
}

func (q *Queue) Stop() {
	close(q.jobs)
	q.wg.Wait()
}

func (q *Queue) worker() {
	defer q.wg.Done()
	for job := range q.jobs {
		q.setStatus(job.ID, models.StatusRunning, nil)

		result, err := q.handler(job, func(done, total int) {
			q.setProgress(job.ID, done, total)
		})
		if err != nil {
			log.Printf("job %s (exercice %s) en erreur: %v", job.ID, job.Exercise, err)
			q.setStatus(job.ID, models.StatusError, &models.Result{Error: err.Error()})
			continue
		}

		status := models.StatusFailed
		if result.Passed {
			status = models.StatusPassed
		}
		q.setStatus(job.ID, status, result)
	}
}

// Submit enregistre le job et le pousse dans la file. Un job pending n'est
// pas encore persisté - seuls les jobs terminés entrent dans l'historique
// (voir setStatus).
func (q *Queue) Submit(job models.Job) {
	job.Status = models.StatusPending
	q.mu.Lock()
	q.store[job.ID] = &job
	q.mu.Unlock()

	q.jobs <- job
}

// Get renvoie l'état courant d'un job.
func (q *Queue) Get(id string) (*models.Job, bool) {
	q.mu.RLock()
	defer q.mu.RUnlock()
	job, ok := q.store[id]
	return job, ok
}

// List renvoie tous les jobs connus (mémoire + historique rechargé au
// démarrage), du plus récent au plus ancien.
func (q *Queue) List() []models.Job {
	q.mu.RLock()
	defer q.mu.RUnlock()

	jobs := make([]models.Job, 0, len(q.store))
	for _, j := range q.store {
		jobs = append(jobs, *j)
	}
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].CreatedAt.After(jobs[j].CreatedAt) })
	return jobs
}

func (q *Queue) setStatus(id string, status models.Status, result *models.Result) {
	q.mu.Lock()
	job, ok := q.store[id]
	if !ok {
		q.mu.Unlock()
		return
	}
	job.Status = status
	job.Result = result

	now := time.Now()
	switch status {
	case models.StatusRunning:
		job.StartedAt = &now
	case models.StatusPassed, models.StatusFailed, models.StatusError:
		job.EndedAt = &now
	}
	jobCopy := *job
	q.mu.Unlock()

	// Seuls les jobs arrivés à un statut terminal entrent dans la session
	// d'historique de leur propriétaire - pending/running restent purement
	// en mémoire, visibles en direct via le polling, mais jamais persistés
	// tels quels.
	if q.history != nil && jobFinished(status) {
		q.history.RecordFinished(jobCopy)
	}
}

func jobFinished(status models.Status) bool {
	return status == models.StatusPassed || status == models.StatusFailed || status == models.StatusError
}

// setProgress met à jour l'avancement d'un job en cours d'exécution.
func (q *Queue) setProgress(id string, done, total int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if job, ok := q.store[id]; ok {
		job.Progress = &models.Progress{Done: done, Total: total}
	}
}
