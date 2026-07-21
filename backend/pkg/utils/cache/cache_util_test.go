package cache

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestKeyedCacheGetSet(t *testing.T) {
	c := NewKeyed[string, *int]()

	if _, ok := c.Get("missing").Get(); ok {
		t.Fatal("expected miss for unset key")
	}

	c.Set("hit", new(42))
	got, ok := c.Get("hit").Get()
	if !ok || got == nil || *got != 42 {
		t.Fatalf("expected cached value 42, got %v (ok=%v)", got, ok)
	}

	// Nil values are valid entries and must report as hits.
	c.Set("nil-entry", nil)
	got, ok = c.Get("nil-entry").Get()
	if !ok || got != nil {
		t.Fatalf("expected nil hit, got %v (ok=%v)", got, ok)
	}

	c.Invalidate("hit")
	if _, ok := c.Get("hit").Get(); ok {
		t.Fatal("expected miss after invalidate")
	}
}

func TestCacheInvalidationDoesNotPublishInFlightFetch(t *testing.T) {
	c := New[string](time.Minute)
	firstFetchStarted := make(chan struct{})
	releaseFirstFetch := make(chan struct{})
	var fetchCalls atomic.Int32

	fetch := func(ctx context.Context) (string, error) {
		switch fetchCalls.Add(1) {
		case 1:
			close(firstFetchStarted)
			select {
			case <-releaseFirstFetch:
				return "obsolete", nil
			case <-ctx.Done():
				return "", ctx.Err()
			}
		case 2:
			return "fresh", nil
		default:
			return "", fmt.Errorf("unexpected fetch call")
		}
	}

	type result struct {
		value string
		err   error
	}
	firstResult := make(chan result, 1)
	go func() {
		value, err := c.GetOrFetch(context.Background(), fetch)
		firstResult <- result{value: value, err: err}
	}()

	select {
	case <-firstFetchStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("first fetch did not start")
	}

	c.Invalidate()
	currentResult := make(chan result, 1)
	go func() {
		value, err := c.GetOrFetch(context.Background(), fetch)
		currentResult <- result{value: value, err: err}
	}()

	var current result
	select {
	case current = <-currentResult:
	case <-time.After(5 * time.Second):
		t.Fatal("post-invalidation fetch waited for the obsolete singleflight call")
	}
	if current.err != nil || current.value != "fresh" {
		t.Fatalf("expected fresh post-invalidation result, got %q (err=%v)", current.value, current.err)
	}

	close(releaseFirstFetch)
	var first result
	select {
	case first = <-firstResult:
	case <-time.After(5 * time.Second):
		t.Fatal("first caller did not finish")
	}
	if first.err != nil || first.value != "fresh" {
		t.Fatalf("expected first caller to observe the current generation, got %q (err=%v)", first.value, first.err)
	}
	if got := fetchCalls.Load(); got != 2 {
		t.Fatalf("expected two generation-specific fetches, got %d", got)
	}

	value, err := c.GetOrFetch(context.Background(), fetch)
	if err != nil || value != "fresh" {
		t.Fatalf("expected cache to retain fresh value, got %q (err=%v)", value, err)
	}
}

func TestCacheCanceledCallerDoesNotRetryAfterInvalidation(t *testing.T) {
	c := New[string](time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	fetchStarted := make(chan struct{})
	releaseFetch := make(chan struct{})
	var fetchCalls atomic.Int32

	resultChannel := make(chan error, 1)
	go func() {
		_, err := c.GetOrFetch(ctx, func(context.Context) (string, error) {
			if fetchCalls.Add(1) == 1 {
				close(fetchStarted)
				<-releaseFetch
				return "obsolete", nil
			}
			return "retry", nil
		})
		resultChannel <- err
	}()

	select {
	case <-fetchStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("singleton fetch did not start")
	}

	c.Invalidate()
	cancel()
	close(releaseFetch)

	select {
	case err := <-resultChannel:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected canceled caller to return context cancellation, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("canceled singleton caller did not finish")
	}
	if got := fetchCalls.Load(); got != 1 {
		t.Fatalf("expected canceled singleton caller not to retry, got %d fetches", got)
	}
}

func TestCacheConcurrentMissesUseSingleflight(t *testing.T) {
	const callerCount = 32

	c := New[int](time.Minute)
	start := make(chan struct{})
	fetchStarted := make(chan struct{})
	releaseFetch := make(chan struct{})
	var fetchStartedOnce sync.Once
	var fetchCalls atomic.Int32
	var callersReady sync.WaitGroup
	var callersDone sync.WaitGroup
	callersReady.Add(callerCount)
	callersDone.Add(callerCount)

	type result struct {
		value int
		err   error
	}
	results := make(chan result, callerCount)
	for range callerCount {
		go func() {
			defer callersDone.Done()
			callersReady.Done()
			<-start
			value, err := c.GetOrFetch(context.Background(), func(context.Context) (int, error) {
				fetchCalls.Add(1)
				fetchStartedOnce.Do(func() { close(fetchStarted) })
				<-releaseFetch
				return 42, nil
			})
			results <- result{value: value, err: err}
		}()
	}

	callersReady.Wait()
	close(start)
	select {
	case <-fetchStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("singleflight fetch did not start")
	}
	close(releaseFetch)
	callersDone.Wait()
	close(results)

	for result := range results {
		if result.err != nil || result.value != 42 {
			t.Fatalf("expected shared value 42, got %d (err=%v)", result.value, result.err)
		}
	}
	if got := fetchCalls.Load(); got != 1 {
		t.Fatalf("expected one shared fetch, got %d", got)
	}
}

func TestKeyedCacheInvalidationDoesNotPublishInFlightFetch(t *testing.T) {
	tests := []struct {
		name              string
		invalidateAll     bool
		otherEntryPresent bool
	}{
		{name: "key", otherEntryPresent: true},
		{name: "all", invalidateAll: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c := NewKeyed[string, string]()
			c.Set("other", "preserved")
			firstFetchStarted := make(chan struct{})
			releaseFirstFetch := make(chan struct{})
			var fetchCalls atomic.Int32

			fetch := func(ctx context.Context) (string, error) {
				switch fetchCalls.Add(1) {
				case 1:
					close(firstFetchStarted)
					select {
					case <-releaseFirstFetch:
						return "obsolete", nil
					case <-ctx.Done():
						return "", ctx.Err()
					}
				case 2:
					return "fresh", nil
				default:
					return "", fmt.Errorf("unexpected fetch call")
				}
			}

			type result struct {
				value string
				err   error
			}
			firstResult := make(chan result, 1)
			go func() {
				value, err := c.GetOrFetch(context.Background(), "target", nil, fetch)
				firstResult <- result{value: value, err: err}
			}()

			select {
			case <-firstFetchStarted:
			case <-time.After(5 * time.Second):
				t.Fatal("first keyed fetch did not start")
			}

			if test.invalidateAll {
				c.InvalidateAll()
			} else {
				c.Invalidate("target")
			}

			currentResult := make(chan result, 1)
			go func() {
				value, err := c.GetOrFetch(context.Background(), "target", nil, fetch)
				currentResult <- result{value: value, err: err}
			}()

			var current result
			select {
			case current = <-currentResult:
			case <-time.After(5 * time.Second):
				t.Fatal("post-invalidation keyed fetch waited for the obsolete singleflight call")
			}
			if current.err != nil || current.value != "fresh" {
				t.Fatalf("expected fresh keyed result, got %q (err=%v)", current.value, current.err)
			}

			close(releaseFirstFetch)
			var first result
			select {
			case first = <-firstResult:
			case <-time.After(5 * time.Second):
				t.Fatal("first keyed caller did not finish")
			}
			if first.err != nil || first.value != "fresh" {
				t.Fatalf("expected first keyed caller to observe the current generation, got %q (err=%v)", first.value, first.err)
			}
			if got := fetchCalls.Load(); got != 2 {
				t.Fatalf("expected two generation-specific keyed fetches, got %d", got)
			}

			value, ok := c.Get("target").Get()
			if !ok || value != "fresh" {
				t.Fatalf("expected keyed cache to retain fresh value, got %q (ok=%v)", value, ok)
			}
			_, otherPresent := c.Get("other").Get()
			if otherPresent != test.otherEntryPresent {
				t.Fatalf("unexpected unrelated entry state: got present=%v, want %v", otherPresent, test.otherEntryPresent)
			}
		})
	}
}

func TestKeyedCacheCanceledCallerDoesNotRetryAfterInvalidation(t *testing.T) {
	c := NewKeyed[string, string]()
	ctx, cancel := context.WithCancel(context.Background())
	fetchStarted := make(chan struct{})
	releaseFetch := make(chan struct{})
	var fetchCalls atomic.Int32

	resultChannel := make(chan error, 1)
	go func() {
		_, err := c.GetOrFetch(ctx, "target", nil, func(context.Context) (string, error) {
			if fetchCalls.Add(1) == 1 {
				close(fetchStarted)
				<-releaseFetch
				return "obsolete", nil
			}
			return "retry", nil
		})
		resultChannel <- err
	}()

	select {
	case <-fetchStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("keyed fetch did not start")
	}

	c.Invalidate("target")
	cancel()
	close(releaseFetch)

	select {
	case err := <-resultChannel:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected canceled caller to return context cancellation, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("canceled keyed caller did not finish")
	}
	if got := fetchCalls.Load(); got != 1 {
		t.Fatalf("expected canceled keyed caller not to retry, got %d fetches", got)
	}
}

func TestCacheStaleFallbackRequiresCurrentGeneration(t *testing.T) {
	upstreamErr := errors.New("upstream unavailable")

	t.Run("current generation", func(t *testing.T) {
		c := New[string](time.Minute)
		c.val = "stale"
		c.set = true
		c.exp = time.Now().Add(-time.Minute)

		value, err := c.GetOrFetch(context.Background(), func(context.Context) (string, error) {
			return "", upstreamErr
		})
		if value != "stale" {
			t.Fatalf("expected stale fallback, got %q", value)
		}
		var staleErr *StaleError
		if !errors.As(err, &staleErr) || !errors.Is(err, upstreamErr) {
			t.Fatalf("expected stale error wrapping upstream failure, got %v", err)
		}
	})

	t.Run("invalidated generation", func(t *testing.T) {
		c := New[string](time.Minute)
		c.val = "stale"
		c.set = true
		c.exp = time.Now().Add(-time.Minute)
		fetchStarted := make(chan struct{})
		releaseFetch := make(chan struct{})

		type result struct {
			value string
			err   error
		}
		resultChannel := make(chan result, 1)
		go func() {
			value, err := c.GetOrFetch(context.Background(), func(context.Context) (string, error) {
				close(fetchStarted)
				<-releaseFetch
				return "", upstreamErr
			})
			resultChannel <- result{value: value, err: err}
		}()

		select {
		case <-fetchStarted:
		case <-time.After(5 * time.Second):
			t.Fatal("stale fallback fetch did not start")
		}
		c.Invalidate()
		close(releaseFetch)

		var got result
		select {
		case got = <-resultChannel:
		case <-time.After(5 * time.Second):
			t.Fatal("invalidated stale fallback did not finish")
		}
		if got.value != "" || !errors.Is(got.err, upstreamErr) {
			t.Fatalf("expected upstream error without stale data, got %q (err=%v)", got.value, got.err)
		}
		var staleErr *StaleError
		if errors.As(got.err, &staleErr) {
			t.Fatalf("invalidated stale generation must not be returned: %v", got.err)
		}
	})
}
