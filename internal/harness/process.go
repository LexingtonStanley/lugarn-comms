package harness

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/hostprobe"
	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

// Resolver is the executable portion of hostprobe.Probe, replaceable in tests.
// Production callers normally use Start with hostprobe's native-only resolution.
type Resolver interface {
	Binary(model.Harness) (string, error)
}

// Driver allows a test binary to replace executable discovery. Its zero value
// uses hostprobe. The resolved program is executed directly, never through a shell.
type Driver struct{ Resolver Resolver }

var defaultProbe = hostprobe.New()

// Handle owns one process and all its descendants. Events contains exactly one
// buffered acknowledgement, then closes at completion. Wait does not require a
// channel consumer. PID and Events are immutable after Start returns.
type Handle struct {
	// JobName is the immutable Windows job identity, empty on POSIX. Persist it
	// alongside PID and process birth before allowing a turn to detach.
	JobName      string
	PID          int
	Events       <-chan Event
	mu           sync.Mutex
	cmd          *exec.Cmd
	group        *processGroup
	done         chan struct{}
	finished     bool
	cancelled    bool
	timedOut     bool
	controlError string
	outcome      Outcome
}

// Start writes per-launch files, resolves the native harness and starts a turn.
// The caller owns the launch's token and eventual file cleanup.
func Start(ctx context.Context, l Launch, prompt string) (*Handle, error) {
	return (Driver{}).Start(ctx, l, prompt)
}

func (d Driver) Start(ctx context.Context, l Launch, prompt string) (*Handle, error) {
	args, err := Args(l, model.ModeHeadless)
	if err != nil {
		return nil, err
	}
	if l.Harness == model.HarnessOpenCode {
		// Before WriteFiles, so a missing or unsupported version keeps its code.
		if _, err = openCodeMajor(l.Version); err != nil {
			return nil, err
		}
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	resolver := d.Resolver
	if resolver == nil {
		resolver = defaultProbe
	}
	if probe, ok := resolver.(interface {
		Harnesses(context.Context) []model.HarnessInfo
	}); ok {
		for _, info := range probe.Harnesses(ctx) {
			if info.Harness != l.Harness {
				continue
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if !info.Supported || !info.Headless {
				reason := "unsupported_version"
				if info.Issue != nil {
					reason = string(*info.Issue)
				}
				return nil, model.NewError(model.ErrorHarnessUnavailable, "harness is unavailable for headless turns").WithDetail("reason", reason)
			}
		}
	}
	binary, err := resolver.Binary(l.Harness)
	if err != nil {
		reason := "not_installed"
		if strings.Contains(err.Error(), "shim_only") {
			reason = "shim_only"
		}
		return nil, model.NewError(model.ErrorHarnessUnavailable, "cannot resolve harness executable").WithDetail("reason", reason).WithDetail("cause", err.Error())
	}
	if !filepath.IsAbs(binary) {
		binary, err = filepath.Abs(binary)
		if err != nil {
			return nil, err
		}
	}
	if runtime.GOOS == "windows" && !strings.EqualFold(filepath.Ext(binary), ".exe") {
		return nil, model.NewError(model.ErrorHarnessUnavailable, "harness must be a native executable").WithDetail("reason", "shim_only")
	}
	if err = WriteFiles(l); err != nil {
		return nil, model.NewError(model.ErrorLaunchFailed, "cannot write launch files").WithDetail("cause", err.Error())
	}
	overrides, err := Environment(l)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cmd := exec.Command(binary, args...)
	cmd.Dir = l.WorkDir
	cmd.Env = mergeEnv(os.Environ(), overrides, l.Harness == model.HarnessOpenCode)
	cmd.Stdin = strings.NewReader(prompt)
	// Bound pipe-drain time if a descendant keeps an inherited handle open.
	cmd.WaitDelay = time.Second
	group, err := newProcessGroup(cmd, l.LaunchID)
	if err != nil {
		return nil, model.NewError(model.ErrorLaunchFailed, "cannot create process group").WithDetail("cause", err.Error())
	}
	events := make(chan Event, 1)
	h := &Handle{JobName: group.jobName(), cmd: cmd, group: group, done: make(chan struct{}), Events: events}
	p := NewParser(l.Harness, l.SessionID)
	stream := &eventWriter{parser: p, emit: func(e Event) { events <- e }, abort: func() { h.stop(false, false) }}
	stderr := &tailWriter{}
	cmd.Stdout = stream
	cmd.Stderr = stderr
	// Hold the lock through assignment: an early stream failure must never signal
	// an unassigned process group. Windows' start-to-assignment window is described
	// in process_windows.go.
	h.mu.Lock()
	err = cmd.Start()
	group.started()
	if err == nil {
		h.PID = cmd.Process.Pid
		err = group.assign(cmd)
	}
	if err != nil {
		h.mu.Unlock()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
		group.close()
		return nil, model.NewError(model.ErrorLaunchFailed, "cannot start harness").WithDetail("cause", err.Error())
	}
	h.mu.Unlock()
	go func() {
		err := cmd.Wait()
		stream.flush()
		exit := cmd.ProcessState.ExitCode()
		if err != nil && exit == 0 {
			p.fail(err.Error())
		}
		h.mu.Lock()
		if err := group.kill(); err != nil && h.controlError == "" {
			h.controlError = oneLine(err.Error())
		}
		group.close()
		h.outcome = p.Finish(exit, h.cancelled, h.timedOut, string(stderr.data))
		if h.controlError != "" {
			h.outcome.Error = oneLine(h.outcome.Error + "; process cleanup: " + h.controlError)
		}
		h.finished = true
		close(events)
		close(h.done)
		h.mu.Unlock()
	}()
	go func() {
		select {
		case <-ctx.Done():
			h.stop(true, errors.Is(ctx.Err(), context.DeadlineExceeded))
		case <-h.done:
		}
	}()
	return h, nil
}

func (h *Handle) stop(cancelled, timedOut bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.finished {
		return
	}
	if cancelled {
		h.cancelled = true
		h.timedOut = h.timedOut || timedOut
	}
	if err := h.group.kill(); err != nil && h.controlError == "" {
		h.controlError = oneLine(err.Error())
	}
}

// Cancel immediately kills the process group/job. It is safe concurrently with
// itself and Wait, and has no effect after completion.
func (h *Handle) Cancel() { h.stop(true, false) }

// Wait may be called repeatedly or concurrently; it returns after process exit
// and stream drain. Its returned Usage is a private copy.
func (h *Handle) Wait() Outcome {
	<-h.done
	h.mu.Lock()
	defer h.mu.Unlock()
	o := h.outcome
	if o.Usage != nil {
		o.Usage = cloneUsage(o.Usage)
	}
	return o
}

func cloneUsage(u *model.Usage) *model.Usage {
	// Copy the pointed counters too, so callers can own the returned outcome.
	clone := *u
	copyInt := func(p *int64) *int64 {
		if p == nil {
			return nil
		}
		v := *p
		return &v
	}
	clone.InputTokens = copyInt(u.InputTokens)
	clone.OutputTokens = copyInt(u.OutputTokens)
	clone.CachedInputTokens = copyInt(u.CachedInputTokens)
	clone.ReasoningTokens = copyInt(u.ReasoningTokens)
	if u.CostUSD != nil {
		v := *u.CostUSD
		clone.CostUSD = &v
	}
	return &clone
}

func mergeEnv(base []string, overrides map[string]string, openCode bool) []string {
	values := map[string]string{}
	key := func(k string) string {
		if runtime.GOOS == "windows" {
			return strings.ToUpper(k)
		}
		return k
	}
	for _, entry := range base {
		k, v, ok := strings.Cut(entry, "=")
		if ok && (!openCode || !strings.EqualFold(k, "OPENCODE_CONFIG_CONTENT")) {
			values[key(k)] = k + "=" + v
		}
	}
	for k, v := range overrides {
		values[key(k)] = k + "=" + v
	}
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, k := range keys {
		result = append(result, values[k])
	}
	return result
}

type tailWriter struct{ data []byte }

func (w *tailWriter) Write(b []byte) (int, error) {
	w.data = append(w.data, b...)
	if len(w.data) > 16000 {
		w.data = append([]byte(nil), w.data[len(w.data)-16000:]...)
	}
	return len(b), nil
}

type eventWriter struct {
	parser  *Parser
	emit    func(Event)
	abort   func()
	pending []byte
	total   int
	discard bool
}

func (w *eventWriter) Write(b []byte) (int, error) {
	n := len(b)
	w.total += n
	if w.discard {
		return n, nil
	}
	if w.total > 64<<20 {
		return w.limit(n)
	}
	for len(b) > 0 {
		at := bytes.IndexByte(b, '\n')
		if at < 0 {
			if len(w.pending)+len(b) > 4<<20 {
				return w.limit(n)
			}
			w.pending = append(w.pending, b...)
			break
		}
		if len(w.pending)+at > 4<<20 {
			return w.limit(n)
		}
		w.pending = append(w.pending, b[:at]...)
		w.line()
		w.pending = w.pending[:0]
		b = b[at+1:]
	}
	return n, nil
}
func (w *eventWriter) line() {
	if e := w.parser.Line(w.pending); e != nil {
		w.emit(*e)
	}
}
func (w *eventWriter) flush() {
	if !w.discard && len(w.pending) > 0 {
		w.line()
	}
	w.pending = nil
}
func (w *eventWriter) limit(n int) (int, error) {
	w.discard = true
	w.pending = nil
	w.parser.fail("harness event output limit exceeded")
	w.abort()
	return n, fmt.Errorf("harness event output limit exceeded")
}
