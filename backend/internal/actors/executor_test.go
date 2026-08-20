package actors

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/fx/fxtest"
)

func TestExecutorSerializesHeterogeneousTasksInternal(t *testing.T) {
	lifecycle := fxtest.NewLifecycle(t)
	runtime, err := NewRuntime(t.Context(), lifecycle)
	require.NoError(t, err)
	executor, err := NewExecutor(t.Context(), runtime, "executor-test", "serial", 1)
	require.NoError(t, err)

	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstResult := make(chan error, 1)
	go func() {
		_, executeErr := executor.Execute(t.Context(), "first", func(context.Context) (int, error) {
			close(firstStarted)
			<-releaseFirst
			return 1, nil
		}, nil)
		firstResult <- executeErr
	}()
	<-firstStarted

	secondStarted := make(chan struct{})
	secondResult := make(chan string, 1)
	go func() {
		value, executeErr := executor.Execute(t.Context(), "second", func(context.Context) (string, error) {
			close(secondStarted)
			return "two", nil
		}, nil)
		if executeErr != nil {
			secondResult <- executeErr.Error()
			return
		}
		secondResult <- value
	}()

	require.Never(t, func() bool { return len(secondStarted) > 0 }, 20*time.Millisecond, time.Millisecond)
	close(releaseFirst)
	require.NoError(t, <-firstResult)
	require.Equal(t, "two", <-secondResult)

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, executor.Stop(stopCtx))
	require.NoError(t, lifecycle.Stop(stopCtx))
}

func TestExecutorPublishesResultBeforeCompletionCallbackInternal(t *testing.T) {
	lifecycle := fxtest.NewLifecycle(t)
	runtime, err := NewRuntime(t.Context(), lifecycle)
	require.NoError(t, err)
	executor, err := NewExecutor(t.Context(), runtime, "executor-test", "completion-order", 1)
	require.NoError(t, err)

	afterStarted := make(chan struct{})
	releaseAfter := make(chan struct{})
	result := make(chan int, 1)
	go func() {
		value, _ := executor.Execute(t.Context(), "ordered", func(context.Context) (int, error) {
			return 42, nil
		}, func(int, error) {
			close(afterStarted)
			<-releaseAfter
		})
		result <- value
	}()

	<-afterStarted
	require.Equal(t, 42, <-result)
	close(releaseAfter)

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, executor.Stop(stopCtx))
	require.NoError(t, lifecycle.Stop(stopCtx))
}

func TestExecutorContainsTaskPanicAndContinuesInternal(t *testing.T) {
	lifecycle := fxtest.NewLifecycle(t)
	runtime, err := NewRuntime(t.Context(), lifecycle)
	require.NoError(t, err)
	executor, err := NewExecutor(t.Context(), runtime, "executor-test", "panic", 1)
	require.NoError(t, err)

	_, err = executor.Execute(t.Context(), "panic task", func(context.Context) (NoPayload, error) {
		panic("deliberate executor panic")
	}, nil)
	require.ErrorContains(t, err, "panic task panicked")

	value, err := executor.Execute(t.Context(), "following task", func(context.Context) (int, error) {
		return 7, nil
	}, nil)
	require.NoError(t, err)
	require.Equal(t, 7, value)

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, executor.Stop(stopCtx))
	require.NoError(t, lifecycle.Stop(stopCtx))
}
