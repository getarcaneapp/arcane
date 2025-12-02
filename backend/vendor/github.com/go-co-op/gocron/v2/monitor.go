package gocron

import (
	"time"

	"github.com/google/uuid"
)

// JobStatus is the status of job run that should be collected with the metric.
type JobStatus string

// The different statuses of job that can be used.
const (
	Fail                 JobStatus = "fail"
	Success              JobStatus = "success"
	Skip                 JobStatus = "skip"
	SingletonRescheduled JobStatus = "singleton_rescheduled"
)

// Monitor represents the interface to collect jobs metrics.
type Monitor interface {
	// IncrementJob will provide details about the job and expects the underlying implementation
	// to handle instantiating and incrementing a value
	IncrementJob(id uuid.UUID, name string, tags []string, status JobStatus)
	// RecordJobTiming will provide details about the job and the timing and expects the underlying implementation
	// to handle instantiating and recording the value
	RecordJobTiming(startTime, endTime time.Time, id uuid.UUID, name string, tags []string)
}

// MonitorStatus extends RecordJobTiming with the job status.
type MonitorStatus interface {
	Monitor
	// RecordJobTimingWithStatus will provide details about the job, its status, error and the timing and expects the underlying implementation
	// to handle instantiating and recording the value
	RecordJobTimingWithStatus(startTime, endTime time.Time, id uuid.UUID, name string, tags []string, status JobStatus, err error)
}
