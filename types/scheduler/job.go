package scheduler

import "context"

type Job interface {
	Name() string
	Schedule(ctx context.Context) string
	Run(ctx context.Context)
}
