package utils

import (
	"context"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
)

type DebounceFn func(context.Context) error

type debounceOp int

const (
	opDo debounceOp = iota
	opCancel
)

type debounceMsg struct {
	op     debounceOp
	delay  time.Duration
	parent context.Context
	fn     DebounceFn
}

type Debouncer struct {
	ch chan debounceMsg
}

// runningJobs tracks the cancel funcs of jobs that are currently executing
type runningJobs struct {
	mu      sync.Mutex
	nextID  uint64
	cancels map[uint64]context.CancelFunc
}

func (r *runningJobs) add(cancel context.CancelFunc) uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	r.cancels[r.nextID] = cancel
	return r.nextID
}

func (r *runningJobs) remove(id uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.cancels, id)
}

func (r *runningJobs) cancelAll() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, cancel := range r.cancels {
		cancel()
		delete(r.cancels, id)
	}
}

func NewDebouncer() *Debouncer {
	return &Debouncer{
		ch: make(chan debounceMsg, 16),
	}
}

// Do schedules fn to run after delay. If another Do arrives before the timer
// fires, the previous pending call is replaced by this one. A job that is already
// running is not interrupted, it is only cancelled by Cancel or the parent context.
// parent provides the context for the eventual job execution.
func (d *Debouncer) Do(parent context.Context, delay time.Duration, fn DebounceFn) {
	logrus.WithFields(logrus.Fields{"fun": GetFunctionName(fn), "delay": delay}).Debug(
		"Scheduling execution - sending message")
	d.ch <- debounceMsg{op: opDo, delay: delay, parent: parent, fn: fn}
	logrus.WithFields(logrus.Fields{"fun": GetFunctionName(fn), "delay": delay}).Debug("Message sent successfully")
}

// Cancel drops any pending job without running it.
func (d *Debouncer) Cancel() {
	logrus.Debug("Canceling all executions - sending cancel message")
	d.ch <- debounceMsg{op: opCancel}
	logrus.Debug("Cancel message sent successfully")
}

func (d *Debouncer) Run(ctx context.Context) error {
	eg, ctx := errgroup.WithContext(ctx)

	eg.Go(func() error {
		logrus.Debug("Running debouncer loop")
		var (
			timer   *time.Timer
			tickC   <-chan time.Time
			pending DebounceFn
			parent  context.Context
			running = &runningJobs{cancels: map[uint64]context.CancelFunc{}}
		)

		// Helper to stop and drain the timer channel safely.
		stopTimer := func() {
			if timer == nil {
				return
			}
			if !timer.Stop() {
				// If Stop returns false, the timer either fired or is firing.
				// Drain if it already fired to avoid spurious wakeups.
				select {
				case <-timer.C:
				default:
				}
			}
			timer = nil
			tickC = nil
		}

		for {
			logrus.Debug("Debouncer loop")
			select {
			case <-ctx.Done():
				logrus.Debug("Debouncer parent context cancelled")
				// Parent canceled: clean up and exit loop.
				running.cancelAll()
				stopTimer()
				return context.Cause(ctx)

			case m := <-d.ch:
				logrus.WithFields(logrus.Fields{"op": m.op}).Debug("Received debouncer message")
				switch m.op {
				case opCancel:
					logrus.Debug("Processing cancel operation")
					// Drop any pending job and stop the running ones.
					logrus.Debug("Cancelling running jobs")
					running.cancelAll()
					logrus.Debug("Stopping timer for cancel")
					stopTimer()
					pending = nil
					parent = nil
					logrus.Debug("Cancel operation completed")

				case opDo:
					logrus.WithFields(logrus.Fields{
						"delay": m.delay,
						"fun":   GetFunctionName(m.fn),
					}).Debug("Processing do operation")
					// a running job is left to finish, e.g. a user callback should not be killed
					// halfway through; jobs are expected to serialize themselves if needed
					logrus.Debug("Stopping previous timer")
					stopTimer()
					pending = m.fn
					parent = m.parent
					logrus.WithFields(logrus.Fields{"delay": m.delay}).Debug("Starting new timer")
					timer = time.NewTimer(m.delay)
					tickC = timer.C
					logrus.WithFields(logrus.Fields{
						"delay": m.delay,
						"fun":   GetFunctionName(m.fn),
					}).Debug("Do operation completed, timer started")
				}

			case <-tickC:
				logrus.Debug("Timer fired, processing tick")
				// Timer fired; take a snapshot of the job and clear pending state
				fn := pending
				pending = nil
				stopTimer()
				logrus.WithFields(logrus.Fields{
					"fn_nil":     fn == nil,
					"parent_nil": parent == nil,
				}).Debug("Timer tick processed")

				if fn == nil || parent == nil {
					logrus.Debug("Skipping execution - fn or parent is nil")
					continue
				}

				jobCtx, jobCancel := context.WithCancel(parent)
				jobID := running.add(jobCancel)

				logrus.WithFields(logrus.Fields{"fun": GetFunctionName(fn)}).Debug("Starting job execution")
				// Run the job in the errgroup; if it returns error, group_ctx cancels
				// and the loop will exit with ctx.Done().
				eg.Go(func() error {
					defer func() {
						logrus.WithFields(logrus.Fields{"fun": GetFunctionName(fn)}).Debug("Job execution completed")
						running.remove(jobID)
						jobCancel()
					}()
					select {
					case <-jobCtx.Done():
						logrus.WithFields(logrus.Fields{"fun": GetFunctionName(fn)}).Debug("Job context cancelled")
						return nil
					default:
					}
					logrus.WithFields(logrus.Fields{"fun": GetFunctionName(fn)}).Debug("Executing debounced function")
					return fn(jobCtx)
				})
			}
		}
	})

	return eg.Wait()
}
