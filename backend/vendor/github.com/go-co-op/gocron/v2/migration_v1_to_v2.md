# Migration Guide: `gocron` v1 → v2

This guide helps you migrate your code from the `v1` branch to the `v2` branch of [go-co-op/gocron](https://github.com/go-co-op/gocron).
Version 2 is a major rewrite focusing on improving the internals of gocron, while also enhancing the user interfaces and error handling.
All major functionality has been ported over.

---

## Table of Contents

- [Overview of Major Changes](#overview-of-major-changes)
- [Installation](#installation)
- [API Changes](#api-changes)
- [Scheduler Creation](#scheduler-creation)
- [Job Definition](#job-definition)
- [Starting and Stopping the Scheduler](#starting-and-stopping-the-scheduler)
- [Error Handling](#error-handling)
- [Distributed Scheduling](#distributed-scheduling)
- [Examples Migration](#examples-migration)
- [Testing and Validation](#testing-and-validation)
- [Troubleshooting](#troubleshooting)
- [References](#references)

---

## Overview of Major Changes

- **Breaking API changes**: All major interfaces and types have changed.
- **Improved error reporting**: Most functions now return errors.
- **Job IDs and cancellation**: Jobs have unique IDs and can be cancelled.
- **Distributed and monitored scheduling**: Built-in support for distributed schedulers and job monitors.
- **Context and logging enhancements**: Improved support for cancellation, context, and custom logging interfaces.

---

## Installation

Update your dependency to v2:

```sh
go get github.com/go-co-op/gocron/v2
```

**Note:** The import path is `github.com/go-co-op/gocron/v2`.

---

## API Changes

### 1. Scheduler Creation

**v1:**
```go
import "github.com/go-co-op/gocron"

s := gocron.NewScheduler(time.UTC)
```

**v2:**
```go
import "github.com/go-co-op/gocron/v2"

s, err := gocron.NewScheduler()
if err != nil { panic(err) }
```
- **v2** returns an error on creation.
- **v2** does not require a location/timezone argument. Use `WithLocation()` if needed.

---

### 2. Job Creation

**v1:**
```go
s.Every(1).Second().Do(taskFunc)
```

**v2:**
```go
j, err := s.NewJob(
    gocron.DurationJob(1*time.Second),
    gocron.NewTask(taskFunc),
)
if err != nil { panic(err) }
```
- **v2** uses explicit job types (`DurationJob`, `CronJob`, etc).
- **v2** jobs have unique IDs: `j.ID()`.
- **v2** returns an error on job creation.

#### Cron Expressions

**v1:**
```go
s.Cron("*/5 * * * *").Do(taskFunc)
```

**v2:**
```go
j, err := s.NewJob(
    gocron.CronJob("*/5 * * * *"),
    gocron.NewTask(taskFunc),
)
```

#### Arguments

**v1:**
```go
s.Every(1).Second().Do(taskFunc, arg1, arg2)
```

**v2:**
```go
j, err := s.NewJob(
    gocron.DurationJob(1*time.Second),
    gocron.NewTask(taskFunc, arg1, arg2),
)
```

---

### 3. Starting and Stopping the Scheduler

**v1:**
```go
s.StartAsync()
s.Stop()
```

**v2:**
```go
s.Start()
s.Shutdown()
```

- Always call `Shutdown()` for graceful cleanup.

---

### 4. Error Handling

- Most v2 methods return errors. Always check `err`.
- Use `errors.go` for error definitions.

---

## References

- [v2 API Documentation](https://pkg.go.dev/github.com/go-co-op/gocron/v2)
- [Examples](https://pkg.go.dev/github.com/go-co-op/gocron/v2#pkg-examples)
- [Release Notes](https://github.com/go-co-op/gocron/releases)

---

**If you encounter issues, open a GitHub Issue or consider contributing a fix by checking out the [CONTRIBUTING.md](CONTRIBUTING.md) guide.**
