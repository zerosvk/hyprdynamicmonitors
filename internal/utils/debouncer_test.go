package utils_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fiffeek/hyprdynamicmonitors/internal/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDebouncer_Basic(t *testing.T) {
	debouncer := utils.NewDebouncer()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- debouncer.Run(ctx)
	}()

	var mu sync.Mutex
	callCount := 0
	var lastCallTime time.Time

	testFn := func(ctx context.Context) error {
		mu.Lock()
		defer mu.Unlock()
		callCount++
		lastCallTime = time.Now()
		t.Logf("Test function called, count: %d", callCount)
		return nil
	}

	// Schedule multiple calls quickly - should be debounced to one
	start := time.Now()
	debouncer.Do(ctx, 100*time.Millisecond, testFn)
	debouncer.Do(ctx, 100*time.Millisecond, testFn)
	debouncer.Do(ctx, 100*time.Millisecond, testFn)

	// Wait for debounced execution
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	// Should have been called exactly once
	assert.Equal(t, 1, callCount, "Function should be called exactly once due to debouncing")

	// Check timing - function should have been called after the debounce delay
	timeSinceStart := lastCallTime.Sub(start)
	assert.True(t, timeSinceStart >= 100*time.Millisecond,
		"Function should be called after debounce delay")

	// Cancel and wait for shutdown
	cancel()
	select {
	case err := <-errCh:
		require.Error(t, err)
		assert.Contains(t, err.Error(), "context canceled")
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timeout waiting for debouncer to shutdown")
	}
}

func TestDebouncer_Cancel(t *testing.T) {
	debouncer := utils.NewDebouncer()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Start the debouncer
	errCh := make(chan error, 1)
	go func() {
		errCh <- debouncer.Run(ctx)
	}()

	// Track function calls
	var callCount atomic.Int32
	testFn := func(ctx context.Context) error {
		t.Logf("Test function called, count: %d", callCount.Add(1))
		return nil
	}

	// Schedule a call, then cancel before it executes
	debouncer.Do(ctx, 200*time.Millisecond, testFn)
	time.Sleep(50 * time.Millisecond) // Wait a bit but not long enough for execution
	debouncer.Cancel()

	// Wait longer than the original delay
	time.Sleep(300 * time.Millisecond)

	// Function should not have been called
	assert.Equal(t, int32(0), callCount.Load(), "Function should not be called after cancel")

	// Cancel and wait for shutdown
	cancel()
	select {
	case err := <-errCh:
		require.Error(t, err)
		assert.Contains(t, err.Error(), "context canceled")
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timeout waiting for debouncer to shutdown")
	}
}

func TestDebouncer_RunningJobIsNotCancelledByNewDo(t *testing.T) {
	debouncer := utils.NewDebouncer()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- debouncer.Run(ctx)
	}()

	started := make(chan struct{})
	release := make(chan struct{})
	firstJobErr := make(chan error, 1)
	var secondRuns atomic.Int32

	slowJob := func(jobCtx context.Context) error {
		close(started)
		<-release
		firstJobErr <- jobCtx.Err()
		return nil
	}
	fastJob := func(context.Context) error {
		secondRuns.Add(1)
		return nil
	}

	debouncer.Do(ctx, 10*time.Millisecond, slowJob)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first job did not start")
	}

	// a new event while the first job runs, e.g. a user callback being executed
	debouncer.Do(ctx, 10*time.Millisecond, fastJob)
	assert.Eventually(t, func() bool { return secondRuns.Load() == 1 }, time.Second, 5*time.Millisecond)

	close(release)
	select {
	case err := <-firstJobErr:
		assert.NoError(t, err, "running job context must not be cancelled by a new Do")
	case <-time.After(time.Second):
		t.Fatal("first job did not finish")
	}

	cancel()
	select {
	case <-errCh:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timeout waiting for debouncer to shutdown")
	}
}

func TestDebouncer_CancelStopsRunningJob(t *testing.T) {
	debouncer := utils.NewDebouncer()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- debouncer.Run(ctx)
	}()

	started := make(chan struct{})
	cancelled := make(chan struct{})
	debouncer.Do(ctx, 10*time.Millisecond, func(jobCtx context.Context) error {
		close(started)
		<-jobCtx.Done()
		close(cancelled)
		return nil
	})
	<-started
	debouncer.Cancel()
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("Cancel did not stop the running job")
	}

	cancel()
	<-errCh
}
