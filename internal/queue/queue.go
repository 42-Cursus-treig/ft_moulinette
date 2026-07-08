// Package queue implémente une file de jobs en mémoire, avec persistance
// disque optionnelle pour survivre à un redémarrage.
package queue

import (
	"log"
	"sort"
	"sync"
	"time"

	"github.com/tristan-reig/ft-moulinette/internal/history"
	"github.com/tristan-reig/ft-moulinette/internal/models"
)

type Handler func(job models.Job, report func(done, total int)) (*models.Result, error)

type Queue struct {
	jobs    chan models.Job
	handler Handler
	workers int
	wg      sync.WaitGroup
	history *history.Store

	mu    sync.RWMutex
	store map[string]*models.Job
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

// LoadHistory recharge les jobs persistés
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
		if job.Status == models.StatusPending || job.Status == models.StatusRunning {
			job.Status = models.StatusError
			if job.Result == nil {
				job.Result = &models.Result{}
			}
			job.Result.Error = "job interrompu par un redémarrage du serveur"
			now := time.Now()
			job.EndedAt = &now
		}
		j := job
		q.store[j.ID] = &j
	}
	return nil
}

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

func (q *Queue) Submit(job models.Job) {
	job.Status = models.StatusPending
	q.mu.Lock()
	q.store[job.ID] = &job
	q.mu.Unlock()

	q.persist(job)
	q.jobs <- job
}

func (q *Queue) Get(id string) (*models.Job, bool) {
	q.mu.RLock()
	defer q.mu.RUnlock()
	job, ok := q.store[id]
	return job, ok
}

// List renvoie tous les jobs connus, du plus récent au plus ancien.
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

	q.persist(jobCopy)
}

// setProgress n'est pas persisté : information transitoire, seul l'état final
// compte pour l'historique.
func (q *Queue) setProgress(id string, done, total int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if job, ok := q.store[id]; ok {
		job.Progress = &models.Progress{Done: done, Total: total}
	}
}

func (q *Queue) persist(job models.Job) {
	if q.history == nil {
		return
	}
	if err := q.history.Save(job); err != nil {
		log.Printf("échec de la persistance du job %s: %v", job.ID, err)
	}
}
