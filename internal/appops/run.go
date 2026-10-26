// Package appops implements the app's JSON and NDJSON entry point.
package appops

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/service"
	"github.com/LexingtonStanley/lugarn-comms/internal/version"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
)

// MaxRequestBytes is 64 MiB: enough for 24 members' briefs, tasks and spawn
// briefs at the maximum text size, including twelve-byte surrogate-pair JSON escapes.
const MaxRequestBytes = 64 << 20

// Options controls watch timing. Zero selects the protocol's 15-second interval.
type Options struct{ HeartbeatInterval time.Duration }

// Run reads a single request and returns the protocol's exit code.
func Run(ctx context.Context, op string, stdin io.Reader, stdout io.Writer, svc service.App) int {
	return RunWithOptions(ctx, op, stdin, stdout, svc, Options{})
}

// RunWithOptions is Run with injectable watch timing for tests.
func RunWithOptions(ctx context.Context, op string, stdin io.Reader, stdout io.Writer, svc service.App, opts Options) int {
	reader := bufio.NewReader(stdin)
	// One JSON object, read without waiting for stdin to close: the app's SSH
	// channel can't half-close it, and watch keeps it open to signal the end.
	b, err := readRequest(reader)
	if err == nil && op != "watch" {
		// Reject what already arrived after the object, without blocking for more.
		rest, _ := reader.Peek(reader.Buffered())
		if len(bytes.TrimSpace(rest)) > 0 {
			err = model.NewError(model.ErrorInvalidRequest, "data after the JSON request")
		}
	}
	var env struct {
		Protocol  *int64  `json:"protocol"`
		Op        string  `json:"op"`
		RequestID *string `json:"request_id"`
	}
	// Decode the header separately so even closed-schema failures echo correlation.
	if err == nil {
		err = json.Unmarshal(b, &env)
	}
	fail := func(err error) int {
		e := model.AsError(err)
		if op == "watch" {
			if json.NewEncoder(stdout).Encode(wire.WatchErrorLine{Type: "error", At: model.Time{Time: time.Now()}, Error: *e}) != nil {
				return 0
			}
		} else {
			if json.NewEncoder(stdout).Encode(wire.Response{Protocol: version.Protocol, Op: wire.OpName(op), RequestID: env.RequestID, Error: e}) != nil {
				return 1
			}
		}
		return model.ExitCode(e.Code)
	}
	if len(b) > MaxRequestBytes {
		return fail(model.NewError(model.ErrorInvalidRequest, "request exceeds 64 MiB"))
	}
	if err != nil {
		return fail(model.NewError(model.ErrorInvalidRequest, "cannot read JSON request"))
	}
	if env.Protocol == nil {
		return fail(model.NewError(model.ErrorInvalidRequest, "protocol is required"))
	}
	if env.Op != op {
		return fail(model.NewError(model.ErrorInvalidRequest, "op must equal argv op"))
	}
	if op != "hello" && *env.Protocol != version.Protocol {
		return fail(unsupported())
	}
	req, err := wire.DecodeRequest(op, b)
	if err != nil {
		return fail(err)
	}
	if hello, ok := req.(*wire.HelloRequest); ok {
		overlap := false
		for _, p := range hello.Protocols {
			if p == version.Protocol {
				overlap = true
			}
		}
		if !overlap {
			return fail(unsupported())
		}
	}
	if watch, ok := req.(*wire.WatchRequest); ok {
		return watchStream(ctx, *watch, reader, stdout, svc, opts)
	}
	result, err := dispatch(ctx, req, svc)
	if err != nil {
		return fail(err)
	}
	// Negotiation is transport-owned even when the service returns host metadata.
	if hello, ok := result.(wire.HelloResult); ok {
		hello.Broker.Protocol = version.Protocol
		hello.Broker.Protocols = []int64{version.Protocol}
		result = hello
	}
	if err = json.NewEncoder(stdout).Encode(wire.Response{Protocol: version.Protocol, Op: wire.OpName(op), RequestID: env.RequestID, OK: true, Result: result}); err != nil {
		return 1
	}
	return 0
}
func unsupported() *model.Error {
	return model.NewError(model.ErrorUnsupportedVersion, "unsupported protocol version").WithDetail("supported", []int{version.Protocol})
}

// A watch request is one JSON object. Scan just through its closing brace, so
// the rest of stdin can stay open without needing a newline delimiter.
func readRequest(r *bufio.Reader) ([]byte, error) {
	var b bytes.Buffer
	depth := 0
	quoted := false
	escaped := false
	started := false
	for b.Len() <= MaxRequestBytes {
		c, err := r.ReadByte()
		if err != nil {
			return b.Bytes(), err
		}
		b.WriteByte(c)
		if !started {
			if c == ' ' || c == '\n' || c == '\r' || c == '\t' {
				continue
			}
			if c != '{' {
				return b.Bytes(), model.NewError(model.ErrorInvalidRequest, "expected request object")
			}
			started = true
			depth = 1
			continue
		}
		if quoted {
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				quoted = false
			}
			continue
		}
		switch c {
		case '"':
			quoted = true
		case '{', '[':
			depth++
		case '}', ']':
			depth--
			if depth == 0 {
				return b.Bytes(), nil
			}
		}
	}
	return b.Bytes(), model.NewError(model.ErrorInvalidRequest, "request exceeds 64 MiB")
}

type watchItem struct {
	line wire.WatchLine
	ack  chan error
}

func watchStream(parent context.Context, req wire.WatchRequest, stdin io.Reader, stdout io.Writer, svc service.App, opts Options) int {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	items := make(chan watchItem)
	done := make(chan error, 1)
	inputDone := make(chan error, 1)
	// Arbitrary io.Reader cannot be interrupted. For process stdin this goroutine
	// ends on EOF; context cancellation never closes a caller-owned stream.
	go func() { inputDone <- watchInputEnd(stdin) }()
	go func() {
		done <- svc.Watch(ctx, req, func(line wire.WatchLine) error {
			item := watchItem{line: line, ack: make(chan error, 1)}
			select {
			case items <- item:
			case <-ctx.Done():
				return ctx.Err()
			}
			select {
			case err := <-item.ack:
				return err
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	interval := opts.HeartbeatInterval
	if interval <= 0 {
		interval = 15 * time.Second
	}
	timer := time.NewTimer(interval)
	defer timer.Stop()
	enc := json.NewEncoder(stdout)
	cursor := ""
	if req.After != nil {
		cursor = *req.After
	}
	write := func(line wire.WatchLine) error {
		if err := enc.Encode(line); err != nil {
			return err
		}
		if f, ok := stdout.(interface{ Flush() error }); ok {
			return f.Flush()
		}
		return nil
	}
	for {
		select {
		case <-ctx.Done():
			return 0
		case err := <-inputDone:
			if err == nil {
				return 0
			}
			return watchFailure(enc, err)
		case err := <-done:
			if err == nil || ctx.Err() != nil {
				return 0
			}
			return watchFailure(enc, err)
		case item := <-items:
			err := write(item.line)
			if err != nil {
				item.ack <- err
				return 0
			}
			switch line := item.line.(type) {
			case wire.Event:
				cursor = line.Cursor
			case *wire.Event:
				cursor = line.Cursor
			case wire.WatchLiveLine:
				cursor = line.Cursor
			case *wire.WatchLiveLine:
				cursor = line.Cursor
			}
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(interval)
			item.ack <- nil
		case <-timer.C:
			if cursor == "" {
				timer.Reset(interval)
				continue
			}
			if write(wire.HeartbeatLine{Type: "heartbeat", Cursor: cursor, At: model.Time{Time: time.Now()}}) != nil {
				return 0
			}
			timer.Reset(interval)
		}
	}
}
func watchFailure(enc *json.Encoder, err error) int {
	e := model.AsError(err)
	if enc.Encode(wire.WatchErrorLine{Type: "error", At: model.Time{Time: time.Now()}, Error: *e}) != nil {
		return 0
	}
	return model.ExitCode(e.Code)
}

// Only whitespace may follow the watch request. Any second JSON value is an
// invalid request; EOF is the normal stream termination signal.
func watchInputEnd(r io.Reader) error {
	var buf [1024]byte
	for {
		n, err := r.Read(buf[:])
		for _, c := range buf[:n] {
			if c != ' ' && c != '\n' && c != '\r' && c != '\t' {
				return model.NewError(model.ErrorInvalidRequest, "unexpected data after watch request")
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}
