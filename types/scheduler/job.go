package scheduler

import "context"

type Job interface {
	Name() string
	Schedule() string
	Run(ctx context.Context)
}
