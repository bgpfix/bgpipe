// NB: excluded from -race because Pipe.Stop() has an inherent race between
// go p.sendEvent(EVENT_STOP) and close(p.evch). The recover() in sendEvent
// makes this safe at runtime, but the race detector flags it.
//
//go:build !race

package core

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bgpfix/bgpfix/pipe"
)

// --- dummy stage for testing ---

type dummyStage struct {
	*StageBase
	prepareFn func() error
	runFn     func() error
	stopFn    func() error
}

func (d *dummyStage) Attach() error {
	if d.Options.IsProducer {
		d.P.Options.AddInput(d.Dir)
	}
	return nil
}

func (d *dummyStage) Prepare() error {
	if d.prepareFn != nil {
		return d.prepareFn()
	}
	return nil
}

func (d *dummyStage) Run() error {
	if d.runFn != nil {
		return d.runFn()
	}
	<-d.Ctx.Done()
	return context.Cause(d.Ctx)
}

func (d *dummyStage) Stop() error {
	if d.stopFn != nil {
		return d.stopFn()
	}
	return nil
}

func newDummyStage(base *StageBase) Stage {
	d := &dummyStage{StageBase: base}
	base.Options.IsProducer = true
	return d
}

// newTestBgpipe creates a minimal Bgpipe with a dummy stage for testing.
// Adds a fake "stdout" and "stdin" to the repo so AttachStages won't panic.
func newTestBgpipe(repo map[string]NewStage) *Bgpipe {
	// ensure stdout/stdin are in repo to prevent auto-add panics
	if repo["stdout"] == nil {
		repo["stdout"] = func(base *StageBase) Stage {
			base.Options.IsStdout = true
			base.Options.IsConsumer = true
			base.Options.Bidir = true
			return &dummyStage{StageBase: base}
		}
	}
	if repo["stdin"] == nil {
		repo["stdin"] = func(base *StageBase) Stage {
			base.Options.IsStdin = true
			base.Options.IsProducer = true
			base.Options.Bidir = true
			return &dummyStage{StageBase: base}
		}
	}
	b := NewBgpipe("test", repo)
	b.Pipe.Options.Logger = nil
	b.Pipe.Options.Caps = false
	b.Pipe.Options.StopTimeout = 50 * time.Millisecond
	return b
}

// drainPipe drains both outputs of a pipe in background
func drainPipe(p *pipe.Pipe) {
	go func() { //nolint:all
		for range p.L.Out {
		}
	}()
	go func() { //nolint:all
		for range p.R.Out {
		}
	}()
}

// --- Stage lifecycle tests ---

func TestStage_FullLifecycle(t *testing.T) {
	var mu sync.Mutex
	var order []string
	add := func(s string) {
		mu.Lock()
		order = append(order, s)
		mu.Unlock()
	}
	repo := map[string]NewStage{
		"test": func(base *StageBase) Stage {
			d := &dummyStage{StageBase: base}
			base.Options.IsProducer = true
			d.prepareFn = func() error {
				add("prepare")
				return nil
			}
			d.runFn = func() error {
				add("run")
				<-d.Ctx.Done()
				return context.Cause(d.Ctx)
			}
			d.stopFn = func() error {
				add("stop")
				return nil
			}
			return d
		},
	}
	b := newTestBgpipe(repo)
	s, err := b.AddStage(1, "test")
	if err != nil {
		t.Fatal(err)
	}
	s.Options.StopTimeout = 100 * time.Millisecond

	if err := b.AttachStages(); err != nil {
		t.Fatal(err)
	}
	b.Pipe.Options.OnStart(b.onStart)

	b.Pipe.Start()
	drainPipe(b.Pipe)

	// wait for stage to enter Run
	time.Sleep(100 * time.Millisecond)

	b.Pipe.Stop()
	b.Cancel(ErrPipeFinished)
	for _, s := range b.Stages {
		s.runStop(nil)
	}

	// wait for any goroutines to finish writing
	time.Sleep(50 * time.Millisecond)

	// verify order
	mu.Lock()
	defer mu.Unlock()
	if len(order) < 2 {
		t.Fatalf("expected at least [prepare run], got %v", order)
	}
	if order[0] != "prepare" {
		t.Fatalf("expected prepare first, got %v", order)
	}
	if order[1] != "run" {
		t.Fatalf("expected run second, got %v", order)
	}
}

func TestStage_PrepareError(t *testing.T) {
	prepareErr := errors.New("prepare failed")
	repo := map[string]NewStage{
		"test": func(base *StageBase) Stage {
			d := &dummyStage{StageBase: base}
			base.Options.IsProducer = true
			d.prepareFn = func() error {
				return prepareErr
			}
			return d
		},
	}
	b := newTestBgpipe(repo)
	if _, err := b.AddStage(1, "test"); err != nil {
		t.Fatal(err)
	}
	if err := b.AttachStages(); err != nil {
		t.Fatal(err)
	}
	b.Pipe.Options.OnStart(b.onStart)

	b.Pipe.Start()
	drainPipe(b.Pipe)

	// wait for global context to be cancelled due to prepare error
	select {
	case <-b.Ctx.Done():
		cause := context.Cause(b.Ctx)
		if cause == nil || !errors.Is(cause, prepareErr) {
			t.Fatalf("expected prepareErr in context cause, got %v", cause)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for prepare error to cancel global context")
	}

	b.Pipe.Stop()
}

func TestStage_RunError(t *testing.T) {
	runErr := errors.New("run failed")
	repo := map[string]NewStage{
		"test": func(base *StageBase) Stage {
			d := &dummyStage{StageBase: base}
			base.Options.IsProducer = true
			d.runFn = func() error {
				return runErr
			}
			return d
		},
	}
	b := newTestBgpipe(repo)
	if _, err := b.AddStage(1, "test"); err != nil {
		t.Fatal(err)
	}
	if err := b.AttachStages(); err != nil {
		t.Fatal(err)
	}
	b.Pipe.Options.OnStart(b.onStart)

	b.Pipe.Start()
	drainPipe(b.Pipe)

	// wait for global context to be cancelled due to run error
	select {
	case <-b.Ctx.Done():
		cause := context.Cause(b.Ctx)
		if cause == nil || !errors.Is(cause, runErr) {
			t.Fatalf("expected runErr in context cause, got %v", cause)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for run error to cancel global context")
	}

	b.Pipe.Stop()
}

func TestStage_RunContextCanceled(t *testing.T) {
	repo := map[string]NewStage{
		"test": func(base *StageBase) Stage {
			d := &dummyStage{StageBase: base}
			base.Options.IsProducer = true
			d.runFn = func() error {
				<-d.Ctx.Done()
				return ErrStageStopped
			}
			return d
		},
	}
	b := newTestBgpipe(repo)
	if _, err := b.AddStage(1, "test"); err != nil {
		t.Fatal(err)
	}
	if err := b.AttachStages(); err != nil {
		t.Fatal(err)
	}
	b.Pipe.Options.OnStart(b.onStart)

	b.Pipe.Start()
	drainPipe(b.Pipe)

	// wait for stage to enter Run
	time.Sleep(50 * time.Millisecond)

	// stop everything
	b.Pipe.Stop()
	b.Cancel(ErrPipeFinished)
	for _, s := range b.Stages {
		s.runStop(nil)
	}

	// ErrStageStopped should NOT cause a fatal error on the global context
	cause := context.Cause(b.Ctx)
	if cause != ErrPipeFinished {
		t.Fatalf("expected ErrPipeFinished, got %v", cause)
	}
}

func TestStage_StopTimeout(t *testing.T) {
	repo := map[string]NewStage{
		"test": func(base *StageBase) Stage {
			d := &dummyStage{StageBase: base}
			base.Options.IsProducer = true
			d.runFn = func() error {
				<-d.Ctx.Done() // blocks until force-cancelled
				return context.Cause(d.Ctx)
			}
			d.stopFn = func() error {
				return nil // Stop returns but Run doesn't stop
			}
			return d
		},
	}
	b := newTestBgpipe(repo)
	s, err := b.AddStage(1, "test")
	if err != nil {
		t.Fatal(err)
	}
	s.Options.StopTimeout = 100 * time.Millisecond

	if err := b.AttachStages(); err != nil {
		t.Fatal(err)
	}
	b.Pipe.Options.OnStart(b.onStart)

	b.Pipe.Start()
	drainPipe(b.Pipe)

	// wait for stage to enter Run
	time.Sleep(50 * time.Millisecond)

	// runStop should force-cancel after StopTimeout
	start := time.Now()
	s.runStop(nil)
	elapsed := time.Since(start)

	if elapsed < 80*time.Millisecond {
		t.Fatalf("runStop returned too quickly (%v), expected ~100ms timeout", elapsed)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("runStop took too long (%v), expected ~100ms timeout", elapsed)
	}

	b.Pipe.Stop()
}

func TestStage_DoubleStartStop(t *testing.T) {
	var runCount atomic.Int32
	repo := map[string]NewStage{
		"test": func(base *StageBase) Stage {
			d := &dummyStage{StageBase: base}
			base.Options.IsProducer = true
			d.runFn = func() error {
				runCount.Add(1)
				<-d.Ctx.Done()
				return context.Cause(d.Ctx)
			}
			return d
		},
	}
	b := newTestBgpipe(repo)
	s, err := b.AddStage(1, "test")
	if err != nil {
		t.Fatal(err)
	}
	s.Options.StopTimeout = 100 * time.Millisecond

	if err := b.AttachStages(); err != nil {
		t.Fatal(err)
	}
	b.Pipe.Options.OnStart(b.onStart)

	b.Pipe.Start()
	drainPipe(b.Pipe)

	time.Sleep(50 * time.Millisecond)

	// calling runStart again should be a no-op
	s.runStart(nil)

	// calling runStop twice should not panic
	b.Pipe.Stop()
	b.Cancel(ErrPipeFinished)
	s.runStop(nil)
	s.runStop(nil)

	if c := runCount.Load(); c != 1 {
		t.Fatalf("expected Run called 1 time, got %d", c)
	}
}

// --- Direction resolution tests ---

func TestStage_DirectionDefaults(t *testing.T) {
	// middle stage = -R, last producer = -L
	repo := map[string]NewStage{"test": newDummyStage}
	b := newTestBgpipe(repo)
	b.AddStage(1, "test")
	b.AddStage(2, "test")
	b.AddStage(3, "test")
	if err := b.AttachStages(); err != nil {
		t.Fatal(err)
	}

	// first stage is a producer -> default -R (because not last)
	s1 := b.Stages[1]
	if !s1.IsRight || s1.IsLeft {
		t.Fatalf("first producer: expected -R, got IsRight=%v IsLeft=%v", s1.IsRight, s1.IsLeft)
	}

	// middle stage (index 2)
	s2 := b.Stages[2]
	if !s2.IsRight || s2.IsLeft {
		t.Fatalf("middle stage: expected -R, got IsRight=%v IsLeft=%v", s2.IsRight, s2.IsLeft)
	}

	// last stage is a producer -> should be -L
	s3 := b.Stages[3]
	if s3.IsRight || !s3.IsLeft {
		t.Fatalf("last producer: expected -L, got IsRight=%v IsLeft=%v", s3.IsRight, s3.IsLeft)
	}
}

func TestStage_DirectionExplicit(t *testing.T) {
	// -L explicit
	repo := map[string]NewStage{"test": newDummyStage}
	b := newTestBgpipe(repo)
	s, _ := b.AddStage(1, "test")
	s.parseArgs([]string{"--left"})
	s2, _ := b.AddStage(2, "test")
	s2.parseArgs([]string{})
	if err := b.AttachStages(); err != nil {
		t.Fatal(err)
	}
	if !s.IsLeft || s.IsRight {
		t.Fatalf("expected -L, got IsLeft=%v IsRight=%v", s.IsLeft, s.IsRight)
	}

	// -LR without Bidir should fail
	repo2 := map[string]NewStage{"test": newDummyStage}
	b2 := newTestBgpipe(repo2)
	s3, _ := b2.AddStage(1, "test")
	s3.parseArgs([]string{"--left", "--right"})
	s4, _ := b2.AddStage(2, "test")
	s4.parseArgs([]string{})
	err := b2.AttachStages()
	if err == nil || !errors.Is(err, ErrLR) {
		t.Fatalf("expected ErrLR for -LR without Bidir, got %v", err)
	}

	// -LR with Bidir should succeed
	repo3 := map[string]NewStage{"test": func(base *StageBase) Stage {
		d := &dummyStage{StageBase: base}
		base.Options.IsProducer = true
		base.Options.Bidir = true
		return d
	}}
	b3 := newTestBgpipe(repo3)
	s5, _ := b3.AddStage(1, "test")
	s5.parseArgs([]string{"--left", "--right"})
	s6, _ := b3.AddStage(2, "test")
	s6.parseArgs([]string{})
	if err := b3.AttachStages(); err != nil {
		t.Fatalf("expected success for -LR with Bidir, got %v", err)
	}
	if !s5.IsBidir {
		t.Fatal("expected IsBidir=true")
	}
}

// --- Coordination tests ---

func TestBgpipe_WaitGroupClosesInputs(t *testing.T) {
	repo := map[string]NewStage{"test": newDummyStage}
	b := newTestBgpipe(repo)
	s, _ := b.AddStage(1, "test")
	s.Options.StopTimeout = 100 * time.Millisecond
	s2, _ := b.AddStage(2, "test")
	s2.Options.StopTimeout = 100 * time.Millisecond

	if err := b.AttachStages(); err != nil {
		t.Fatal(err)
	}
	b.Pipe.Options.OnStart(b.onStart)
	b.Pipe.Start()
	drainPipe(b.Pipe)

	// wait for stages to start
	time.Sleep(100 * time.Millisecond)

	// stop all stages (which decrements wg), should cause pipe inputs to close
	b.Cancel(ErrPipeFinished)
	for _, st := range b.Stages {
		st.runStop(nil)
	}

	done := make(chan struct{})
	go func() {
		b.Pipe.Wait()
		close(done)
	}()

	select {
	case <-done:
		// success - pipe stopped because wg_* decrements triggered input closure
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for pipe to stop after stages finished")
	}
}

func TestStage_WaitForEvent(t *testing.T) {
	var runStarted atomic.Bool
	stageIdx := 0 // track which stage instance this is
	repo := map[string]NewStage{
		"test": func(base *StageBase) Stage {
			stageIdx++
			idx := stageIdx
			d := &dummyStage{StageBase: base}
			base.Options.IsProducer = true
			if idx == 1 {
				d.runFn = func() error {
					runStarted.Store(true)
					<-d.Ctx.Done()
					return context.Cause(d.Ctx)
				}
			}
			return d
		},
	}
	b := newTestBgpipe(repo)
	s, _ := b.AddStage(1, "test")
	s.parseArgs([]string{"--wait=custom"})
	s.Options.StopTimeout = 100 * time.Millisecond

	// add a second stage so pipe has something (first stage waits for event)
	s2, _ := b.AddStage(2, "test")
	s2.Options.StopTimeout = 100 * time.Millisecond

	if err := b.AttachStages(); err != nil {
		t.Fatal(err)
	}
	b.Pipe.Options.OnStart(b.onStart)
	b.Pipe.Start()
	drainPipe(b.Pipe)

	// wait a bit - stage 1 should NOT have started Run yet
	time.Sleep(100 * time.Millisecond)
	if runStarted.Load() {
		t.Fatal("stage should not have started Run before wait event fires")
	}

	// fire the event that stage 1 is waiting for
	b.Pipe.Event("custom/START")
	time.Sleep(100 * time.Millisecond)

	if !runStarted.Load() {
		t.Fatal("stage should have started Run after wait event fires")
	}

	b.Pipe.Stop()
	b.Cancel(ErrPipeFinished)
	for _, st := range b.Stages {
		st.runStop(nil)
	}
}

func TestStage_StopOnEvent(t *testing.T) {
	var stopped atomic.Bool
	repo := map[string]NewStage{
		"test": func(base *StageBase) Stage {
			d := &dummyStage{StageBase: base}
			base.Options.IsProducer = true
			d.runFn = func() error {
				<-d.Ctx.Done()
				return context.Cause(d.Ctx)
			}
			d.stopFn = func() error {
				stopped.Store(true)
				return nil
			}
			return d
		},
	}
	b := newTestBgpipe(repo)
	s, _ := b.AddStage(1, "test")
	s.parseArgs([]string{"--stop=custom"})
	s.Options.StopTimeout = 200 * time.Millisecond

	s2, _ := b.AddStage(2, "test")
	s2.Options.StopTimeout = 100 * time.Millisecond

	if err := b.AttachStages(); err != nil {
		t.Fatal(err)
	}
	b.Pipe.Options.OnStart(b.onStart)
	b.Pipe.Start()
	drainPipe(b.Pipe)

	// wait for stages to start
	time.Sleep(100 * time.Millisecond)

	// fire the stop event
	b.Pipe.Event("custom/STOP")

	// wait for the stop event handler to call runStop
	time.Sleep(300 * time.Millisecond)

	if !stopped.Load() {
		t.Fatal("stage Stop should have been called after the stop event")
	}

	b.Pipe.Stop()
	b.Cancel(ErrPipeFinished)
	for _, st := range b.Stages {
		st.runStop(nil)
	}
}
