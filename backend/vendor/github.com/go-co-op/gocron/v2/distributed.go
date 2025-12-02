//go:generate mockgen -destination=mocks/distributed.go -package=gocronmocks . Elector,Locker,Lock
package gocron

import (
	"context"
)

// Elector determines the leader from instances asking to be the leader. Only
// the leader runs jobs. If the leader goes down, a new leader will be elected.
type Elector interface {
	// IsLeader should return  nil if the job should be scheduled by the instance
	// making the request and an error if the job should not be scheduled.
	IsLeader(context.Context) error
}

// Locker represents the required interface to lock jobs when running multiple schedulers.
// The lock is held for the duration of the job's run, and it is expected that the
// locker implementation handles time splay between schedulers.
// The lock key passed is the job's name - which, if not set, defaults to the
// go function's name, e.g. "pkg.myJob" for func myJob() {} in pkg
//
// Notes: The locker and scheduler do not handle synchronization of run times across
// schedulers.
//
//  1. If you are using duration based jobs (DurationJob), you can utilize the JobOption
//     WithStartAt to set a start time for the job to the nearest time rounded to your
//     duration. For example, if you have a job that runs every 5 minutes, you can set
//     the start time to the nearest 5 minute e.g. 12:05, 12:10.
//
//  2. For all jobs, the implementation is still vulnerable to clockskew between scheduler
//     instances. This may result in a single scheduler instance running the majority of the
//     jobs.
//
// For distributed jobs, consider utilizing the Elector option if these notes are not acceptable
// to your use case.
type Locker interface {
	// Lock if an error is returned by lock, the job will not be scheduled.
	Lock(ctx context.Context, key string) (Lock, error)
}

// Lock represents an obtained lock. The lock is released after the execution of the job
// by the scheduler.
type Lock interface {
	Unlock(ctx context.Context) error
}
