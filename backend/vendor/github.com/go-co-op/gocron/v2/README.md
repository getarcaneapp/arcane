# gocron: A Golang Job Scheduling Package

[![CI State](https://github.com/go-co-op/gocron/actions/workflows/go_test.yml/badge.svg?branch=v2&event=push)](https://github.com/go-co-op/gocron/actions)
![Go Report Card](https://goreportcard.com/badge/github.com/go-co-op/gocron) [![Go Doc](https://godoc.org/github.com/go-co-op/gocron/v2?status.svg)](https://pkg.go.dev/github.com/go-co-op/gocron/v2)

gocron is a job scheduling package which lets you run Go functions at pre-determined intervals.

> Looking for a visual interface?  
> Check out [**gocron-ui**](https://github.com/go-co-op/gocron-ui) — a lightweight web dashboard to monitor, trigger, and manage your `gocron` jobs in real time.

If you want to chat, you can find us on Slack at
[<img src="https://img.shields.io/badge/gophers-gocron-brightgreen?logo=slack">](https://gophers.slack.com/archives/CQ7T0T1FW)

## Quick Start

```
go get github.com/go-co-op/gocron/v2
```

```golang
package main

import (
	"fmt"
	"time"

	"github.com/go-co-op/gocron/v2"
)

func main() {
	// create a scheduler
	s, err := gocron.NewScheduler()
	if err != nil {
		// handle error
	}

	// add a job to the scheduler
	j, err := s.NewJob(
		gocron.DurationJob(
			10*time.Second,
		),
		gocron.NewTask(
			func(a string, b int) {
				// do things
			},
			"hello",
			1,
		),
	)
	if err != nil {
		// handle error
	}
	// each job has a unique id
	fmt.Println(j.ID())

	// start the scheduler
	s.Start()

	// block until you are ready to shut down
	select {
	case <-time.After(time.Minute):
	}

	// when you're done, shut it down
	err = s.Shutdown()
	if err != nil {
		// handle error
	}
}
```

## Examples

- [Go doc examples](https://pkg.go.dev/github.com/go-co-op/gocron/v2#pkg-examples)
- [Examples directory](examples)

## Articles & Blog Posts

Community articles and tutorials about using gocron:

- [Building a dynamic, highly available scheduler in Go](https://tech.efg.gg/posts/2025/highly-available-scheduler-in-go/) - A deep dive into building a highly available scheduler using gocron, MongoDB change streams, and leader election patterns for the FACEIT Watch platform.

## Concepts

- **Job**: The job encapsulates a "task", which is made up of a go function and any function parameters. The Job then
  provides the scheduler with the time the job should next be scheduled to run.
- **Scheduler**: The scheduler keeps track of all the jobs and sends each job to the executor when
  it is ready to be run.
- **Executor**: The executor calls the job's task and manages the complexities of different job
  execution timing requirements (e.g. singletons that shouldn't overrun each other, limiting the max number of jobs running)


## Features

### Job types
Jobs can be run at various intervals.
- [**Duration**](https://pkg.go.dev/github.com/go-co-op/gocron/v2#DurationJob):
Jobs can be run at a fixed `time.Duration`.
- [**Random duration**](https://pkg.go.dev/github.com/go-co-op/gocron/v2#DurationRandomJob):
Jobs can be run at a random `time.Duration` between a min and max.
- [**Cron**](https://pkg.go.dev/github.com/go-co-op/gocron/v2#CronJob):
Jobs can be run using a crontab.
- [**Daily**](https://pkg.go.dev/github.com/go-co-op/gocron/v2#DailyJob):
Jobs can be run every x days at specific times.
- [**Weekly**](https://pkg.go.dev/github.com/go-co-op/gocron/v2#WeeklyJob):
Jobs can be run every x weeks on specific days of the week and at specific times.
- [**Monthly**](https://pkg.go.dev/github.com/go-co-op/gocron/v2#MonthlyJob):
Jobs can be run every x months on specific days of the month and at specific times.
- [**One time**](https://pkg.go.dev/github.com/go-co-op/gocron/v2#OneTimeJob):
Jobs can be run at specific time(s) (either once or many times).

### Interval Timing
Jobs can be scheduled with different interval timing modes.
- [**Interval from scheduled time (default)**](https://pkg.go.dev/github.com/go-co-op/gocron/v2#DurationJob):
By default, jobs calculate their next run time from when they were scheduled to start, resulting in fixed intervals 
regardless of execution time. Good for cron-like scheduling at predictable times.
- [**Interval from completion time**](https://pkg.go.dev/github.com/go-co-op/gocron/v2#WithIntervalFromCompletion):
Jobs can calculate their next run time from when they complete, ensuring consistent rest periods between executions.
Ideal for rate-limited APIs, resource-intensive jobs, and scenarios where execution time varies.

### Concurrency Limits
Jobs can be limited individually or across the entire scheduler.
- [**Per job limiting with singleton mode**](https://pkg.go.dev/github.com/go-co-op/gocron/v2#WithSingletonMode):
Jobs can be limited to a single concurrent execution that either reschedules (skips overlapping executions)
or queues (waits for the previous execution to finish).
- [**Per scheduler limiting with limit mode**](https://pkg.go.dev/github.com/go-co-op/gocron/v2#WithLimitConcurrentJobs):
Jobs can be limited to a certain number of concurrent executions across the entire scheduler
using either reschedule (skip when the limit is met) or queue (jobs are added to a queue to
wait for the limit to be available).
- **Note:** A scheduler limit and a job limit can both be enabled.

### Distributed instances of gocron
Multiple instances of gocron can be run.
- [**Elector**](https://pkg.go.dev/github.com/go-co-op/gocron/v2#WithDistributedElector):
An elector can be used to elect a single instance of gocron to run as the primary with the
other instances checking to see if a new leader needs to be elected.
  - Implementations: [go-co-op electors](https://github.com/go-co-op?q=-elector&type=all&language=&sort=)
    (don't see what you need? request on slack to get a repo created to contribute it!)
- [**Locker**](https://pkg.go.dev/github.com/go-co-op/gocron/v2#WithDistributedLocker):
A locker can be used to lock each run of a job to a single instance of gocron.
Locker can be at job or scheduler, if it is defined both at job and scheduler then locker of job will take precedence.
  - See Notes in the doc for [Locker](https://pkg.go.dev/github.com/go-co-op/gocron/v2#Locker) for
    details and limitations of the locker design.
  - Implementations: [go-co-op lockers](https://github.com/go-co-op?q=-lock&type=all&language=&sort=)
    (don't see what you need? request on slack to get a repo created to contribute it!)

### Events
Job events can trigger actions.
- [**Listeners**](https://pkg.go.dev/github.com/go-co-op/gocron/v2#WithEventListeners):
Can be added to a job, with [event listeners](https://pkg.go.dev/github.com/go-co-op/gocron/v2#EventListener),
or all jobs across the
[scheduler](https://pkg.go.dev/github.com/go-co-op/gocron/v2#WithGlobalJobOptions)
to listen for job events and trigger actions.

### Options
Many job and scheduler options are available.
- [**Job options**](https://pkg.go.dev/github.com/go-co-op/gocron/v2#JobOption):
Job options can be set when creating a job using `NewJob`.
- [**Global job options**](https://pkg.go.dev/github.com/go-co-op/gocron/v2#WithGlobalJobOptions):
Global job options can be set when creating a scheduler using `NewScheduler`
and the `WithGlobalJobOptions` option.
- [**Scheduler options**](https://pkg.go.dev/github.com/go-co-op/gocron/v2#SchedulerOption):
Scheduler options can be set when creating a scheduler using `NewScheduler`.

### Logging
Logs can be enabled.
- [Logger](https://pkg.go.dev/github.com/go-co-op/gocron/v2#Logger):
The Logger interface can be implemented with your desired logging library.
The provided NewLogger uses the standard library's log package.

### Metrics
Metrics may be collected from the execution of each job.
- [**Monitor**](https://pkg.go.dev/github.com/go-co-op/gocron/v2#Monitor):
- [**MonitorStatus**](https://pkg.go.dev/github.com/go-co-op/gocron/v2#MonitorStatus) (includes status and error (if any) of the Job)
A monitor can be used to collect metrics for each job from a scheduler.
  - Implementations: [go-co-op monitors](https://github.com/go-co-op?q=-monitor&type=all&language=&sort=)
    (don't see what you need? request on slack to get a repo created to contribute it!)

### Testing
The gocron library is set up to enable testing.
- Mocks are provided in [the mock package](mocks) using [gomock](https://github.com/uber-go/mock).
- Time can be mocked by passing in a [FakeClock](https://pkg.go.dev/github.com/jonboulle/clockwork#FakeClock)
to [WithClock](https://pkg.go.dev/github.com/go-co-op/gocron/v2#WithClock) -
see the [example on WithClock](https://pkg.go.dev/github.com/go-co-op/gocron/v2#example-WithClock).

## Supporters

We appreciate the support for free and open source software!

This project is supported by:

[JetBrains](https://www.jetbrains.com/?from=gocron)

<a href="https://www.jetbrains.com/?from=gocron">
 <picture>
   <source media="(prefers-color-scheme: dark)" srcset="assets/jetbrains-mono-white.png" />
   <source media="(prefers-color-scheme: light)" srcset="https://resources.jetbrains.com/storage/products/company/brand/logos/jetbrains.png" />
   <img alt="JetBrains logo" src="https://resources.jetbrains.com/storage/products/company/brand/logos/jetbrains.png" />
 </picture>
</a>

[Sentry](https://sentry.io/welcome/)

<a href="https://sentry.io/?utm_source=github&utm_medium=logo">
 <picture>
   <source media="(prefers-color-scheme: dark)" srcset="assets/sentry-wordmark-light-280x84.png" />
   <source media="(prefers-color-scheme: light)" srcset="https://sentry-brand.storage.googleapis.com/sentry-wordmark-dark-280x84.png" />
   <img alt="Sentry logo" src="https://sentry-brand.storage.googleapis.com/sentry-wordmark-dark-280x84.png" />
 </picture>
</a>

## Star History

<a href="https://www.star-history.com/#go-co-op/gocron&Date">
 <picture>
   <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/svg?repos=go-co-op/gocron&type=Date&theme=dark" />
   <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/svg?repos=go-co-op/gocron&type=Date" />
   <img alt="Star History Chart" src="https://api.star-history.com/svg?repos=go-co-op/gocron&type=Date" />
 </picture>
</a>
