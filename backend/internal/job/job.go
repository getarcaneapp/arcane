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

	time.Sleep(100 * time.Millisecond)
	s.LogJobStatus(ctx)

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
		gocron.WithName(name),
		gocron.WithEventListeners(
			gocron.BeforeJobRuns(func(jobID uuid.UUID, jobName string) {
				slog.Info("Job starting", "name", name, "id", jobID.String())
			}),
			gocron.AfterJobRuns(func(jobID uuid.UUID, jobName string) {
				slog.Info("Job finished successfully", "name", name, "id", jobID.String())
			}),
			gocron.AfterJobRunsWithError(func(jobID uuid.UUID, jobName string, err error) {
				slog.Error("Job failed", "name", name, "id", jobID.String(), "error", err)
			}),
		),
	}

	task := gocron.NewTask(func() {
		if err := taskFunc(ctx); err != nil {
			slog.Error("Error executing task function", "name", name, "error", err)
		}
	})

	var job gocron.Job
	var err error
	if runImmediately {
		job, err = s.scheduler.NewJob(definition, task, append(jobOptions, gocron.WithStartAt(gocron.WithStartImmediately()))...)
	} else {
		job, err = s.scheduler.NewJob(definition, task, jobOptions...)
	}
	if err != nil {
		return fmt.Errorf("failed to register job %q: %w", name, err)
	}

	nextRun, _ := job.NextRun()
	slog.InfoContext(ctx, "Job registered successfully", "name", name, "nextRun", nextRun.Format(time.RFC3339), "runImmediately", runImmediately)
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
	slog.DebugContext(ctx, "Rescheduling job", "name", name, "interval", interval.String())
	s.RemoveJobByName(name)
	definition := gocron.DurationJob(interval)
	return s.RegisterJob(ctx, name, definition, taskFunc, runImmediately)
}

func (s *Scheduler) LogJobStatus(ctx context.Context) {
	jobs := s.scheduler.Jobs()
	slog.InfoContext(ctx, "Active jobs in scheduler", "count", len(jobs))
	for _, j := range jobs {
		nextRun, err := j.NextRun()
		if err != nil {
			slog.WarnContext(ctx, "Could not get next run for job", "name", j.Name(), "error", err)
			continue
		}
		slog.InfoContext(ctx, "Job status", "name", j.Name(), "id", j.ID().String(), "nextRun", nextRun.Format(time.RFC3339))
	}
}
