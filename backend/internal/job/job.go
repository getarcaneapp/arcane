package job

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/go-co-op/gocron/v2"
	"github.com/google/uuid"
)

type Scheduler struct {
	scheduler gocron.Scheduler
}

func NewScheduler() (*Scheduler, error) {
	s, err := gocron.NewScheduler()
	if err != nil {
		return nil, fmt.Errorf("failed to create new gocron scheduler: %w", err)
	}
	return &Scheduler{scheduler: s}, nil
}

func (s *Scheduler) Run(ctx context.Context) error {
	slog.InfoContext(ctx, "Starting job scheduler")
	s.scheduler.Start() // Start the scheduler, non-blocking

	// Wait for the context to be done (e.g., application shutdown)
	<-ctx.Done()

	slog.InfoContext(ctx, "Shutting down job scheduler...")
	err := s.scheduler.Shutdown()
	if err != nil {
		slog.ErrorContext(ctx, "Error shutting down job scheduler", "error", err)
		return fmt.Errorf("error during scheduler shutdown: %w", err)
	}

	slog.InfoContext(ctx, "Job scheduler shut down successfully")
	return nil
}

func (s *Scheduler) RegisterJob(
	ctx context.Context,
	name string,
	definition gocron.JobDefinition,
	taskFunc func(ctx context.Context) error,
	runImmediately bool,
) error {
	jobOptions := []gocron.JobOption{
		gocron.WithContext(ctx),
		gocron.WithName(name),
		gocron.WithEventListeners(
			gocron.BeforeJobRuns(func(jobID uuid.UUID, jobName string) {
				slog.InfoContext(ctx, "Job starting", "name", name, "id", jobID.String())
			}),
			gocron.AfterJobRuns(func(jobID uuid.UUID, jobName string) {
				slog.InfoContext(ctx, "Job finished successfully", "name", name, "id", jobID.String())
			}),
			gocron.AfterJobRunsWithError(func(jobID uuid.UUID, jobName string, err error) {
				slog.ErrorContext(ctx, "Job failed", "name", name, "id", jobID.String(), "error", err)
			}),
		),
	}

	task := gocron.NewTask(func() {
		if err := taskFunc(ctx); err != nil {
			slog.ErrorContext(ctx, "Error executing task function", "name", name, "error", err)
		}
	})

	var err error
	if runImmediately {
		_, err = s.scheduler.NewJob(definition, task, append(jobOptions, gocron.WithStartAt(gocron.WithStartImmediately()))...)
	} else {
		_, err = s.scheduler.NewJob(definition, task, jobOptions...)
	}
	if err != nil {
		return fmt.Errorf("failed to register job %q: %w", name, err)
	}

	slog.InfoContext(ctx, "Job registered successfully", "name", name)
	return nil
}

func (s *Scheduler) RemoveJobByName(name string) {
	for _, j := range s.scheduler.Jobs() {
		if j.Name() == name {
			_ = s.scheduler.RemoveJob(j.ID())
		}
	}
}

func (s *Scheduler) RescheduleDurationJobByName(
	ctx context.Context,
	name string,
	interval time.Duration,
	taskFunc func(ctx context.Context) error,
	runImmediately bool,
) error {
	s.RemoveJobByName(name)
	definition := gocron.DurationJob(interval)
	return s.RegisterJob(ctx, name, definition, taskFunc, runImmediately)
}
