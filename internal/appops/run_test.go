package appops

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/service"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
)

func TestEnvelope(t *testing.T) {
	for _, tt := range []struct {
		name, op, input string
		code            int
		errorCode       model.ErrorCode
	}{
		{"missing protocol", "status", `{"op":"status","request_id":"echo"}`, 2, model.ErrorInvalidRequest},
		{"op mismatch", "status", `{"protocol":1,"op":"members","request_id":"echo"}`, 2, model.ErrorInvalidRequest},
		{"unknown field", "status", `{"protocol":1,"op":"status","request_id":"echo","surprise":true}`, 2, model.ErrorInvalidRequest},
		{"unsupported", "status", `{"protocol":2,"op":"status","request_id":"echo"}`, 4, model.ErrorUnsupportedVersion},
		{"missing required", "members", `{"protocol":1,"op":"members","request_id":"echo"}`, 2, model.ErrorInvalidRequest},
		{"wrong protocol type", "status", `{"protocol":"1","op":"status","request_id":"echo"}`, 2, model.ErrorInvalidRequest},
		{"null protocol", "status", `{"protocol":null,"op":"status","request_id":"echo"}`, 2, model.ErrorInvalidRequest},
		{"trailing JSON", "status", `{"protocol":1,"op":"status"} {}`, 2, model.ErrorInvalidRequest},
		{"negotiation", "hello", `{"protocol":999,"op":"hello","request_id":"echo","protocols":[2,1],"app":{"version":"test"}}`, 0, ""},
		{"zero hello", "hello", `{"protocol":0,"op":"hello","request_id":"echo","protocols":[1],"app":{"version":"test"}}`, 0, ""},
		{"no overlap", "hello", `{"protocol":1,"op":"hello","request_id":"echo","protocols":[2],"app":{"version":"test"}}`, 4, model.ErrorUnsupportedVersion},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			called := false
			fake := &service.Fake{HelloFunc: func(_ context.Context, req wire.HelloRequest) (wire.HelloResult, error) {
				called = true
				return wire.HelloResult{}, nil
			}}
			got := Run(context.Background(), tt.op, strings.NewReader(tt.input), &out, fake)
			if got != tt.code {
				t.Fatalf("exit %d want %d: %s", got, tt.code, &out)
			}
			var response wire.Response
			if err := model.DecodeStrict(out.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(tt.input, `"echo"`) && (response.RequestID == nil || *response.RequestID != "echo") {
				t.Fatalf("missing echo: %s", &out)
			}
			if tt.code != 0 {
				if response.OK || response.Error == nil || response.Error.Code != tt.errorCode {
					t.Fatal(out.String())
				}
				if tt.errorCode == model.ErrorUnsupportedVersion {
					b, _ := json.Marshal(response.Error.Details["supported"])
					if string(b) != "[1]" {
						t.Fatal(out.String())
					}
				}
				if called {
					t.Fatal("called service on invalid envelope")
				}
			} else {
				if !response.OK || !called {
					t.Fatal(out.String())
				}
				hello := response.Result.(*wire.HelloResult)
				if hello.Broker.Protocol != 1 {
					t.Fatal(out.String())
				}
			}
		})
	}
}

func TestEveryErrorExit(t *testing.T) {
	for _, code := range (model.ErrorCode("")).Values() {
		t.Run(string(code), func(t *testing.T) {
			fake := &service.Fake{StatusFunc: func(context.Context, wire.StatusRequest) (wire.StatusResult, error) {
				return wire.StatusResult{}, model.NewError(code, "test")
			}}
			var out bytes.Buffer
			got := Run(context.Background(), "status", strings.NewReader(`{"protocol":1,"op":"status","request_id":"x"}`), &out, fake)
			if got != model.ExitCode(code) {
				t.Fatalf("exit %d: %s", got, &out)
			}
			var response wire.Response
			if err := model.DecodeStrict(out.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if response.Error.Code != code || response.OK {
				t.Fatal(out.String())
			}
		})
	}
}

func TestRequestSizeLimit(t *testing.T) {
	var out bytes.Buffer
	input := `{"protocol":1,"op":"status","pad":"` + strings.Repeat("x", MaxRequestBytes) + `"}`
	if Run(context.Background(), "status", strings.NewReader(input), &out, &service.Fake{}) != 2 {
		t.Fatal(out.String())
	}
}

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, errors.New("broken") }

func TestWatchReplayLiveHeartbeatEOF(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	inputR, inputW := io.Pipe()
	defer inputR.Close()
	defer inputW.Close()
	outputR, outputW := io.Pipe()
	defer outputR.Close()
	defer outputW.Close()
	serviceDone := make(chan struct{})
	fake := &service.Fake{WatchFunc: func(ctx context.Context, req wire.WatchRequest, emit func(wire.WatchLine) error) error {
		defer close(serviceDone)
		if req.After == nil || *req.After != "host.0" {
			return errors.New("lost cursor")
		}
		for _, line := range []wire.WatchLine{
			wire.Event{Type: model.EventMessageCreated, Cursor: "host.1", Data: &wire.MessageCreatedEventData{}},
			wire.WatchLiveLine{Type: "watch.live", Protocol: 1, Cursor: "host.1"},
			wire.Event{Type: model.EventMessageCreated, Cursor: "host.2", Data: &wire.MessageCreatedEventData{}},
		} {
			if err := emit(line); err != nil {
				return err
			}
		}
		<-ctx.Done()
		return ctx.Err()
	}}
	done := make(chan int, 1)
	go func() {
		done <- RunWithOptions(ctx, "watch", inputR, outputW, fake, Options{HeartbeatInterval: 10 * time.Millisecond})
	}()
	if _, err := io.WriteString(inputW, `{"protocol":1,"op":"watch","after":"host.0"}`); err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(outputR)
	for _, typ := range []string{"message.created", "watch.live", "message.created", "heartbeat"} {
		if !scanner.Scan() {
			t.Fatalf("no %s: %v", typ, scanner.Err())
		}
		var v map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
		if v["type"] != typ {
			t.Fatalf("want %s got %s", typ, scanner.Bytes())
		}
		if typ == "heartbeat" && v["cursor"] != "host.2" {
			t.Fatal(v)
		}
	}
	inputW.Close()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatal(code)
		}
	case <-ctx.Done():
		t.Fatal("watch failed to exit on EOF")
	}
	select {
	case <-serviceDone:
	case <-ctx.Done():
		t.Fatal("service not cancelled")
	}
}
func TestWatchBrokenOutput(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r, w := io.Pipe()
	defer r.Close()
	defer w.Close()
	go func() { _, _ = io.WriteString(w, `{"protocol":1,"op":"watch"}`) }()
	fake := &service.Fake{WatchFunc: func(ctx context.Context, _ wire.WatchRequest, emit func(wire.WatchLine) error) error {
		return emit(wire.WatchLiveLine{Type: "watch.live", Protocol: 1})
	}}
	if code := Run(ctx, "watch", r, brokenWriter{}, fake); code != 0 {
		t.Fatal(code)
	}
}
func TestWatchErrors(t *testing.T) {
	for _, code := range (model.ErrorCode("")).Values() {
		t.Run(string(code), func(t *testing.T) {
			r, w := io.Pipe()
			defer r.Close()
			defer w.Close()
			go func() { _, _ = io.WriteString(w, `{"protocol":1,"op":"watch"}`) }()
			fake := &service.Fake{WatchFunc: func(context.Context, wire.WatchRequest, func(wire.WatchLine) error) error {
				return model.NewError(code, "test")
			}}
			var out bytes.Buffer
			if got := Run(context.Background(), "watch", r, &out, fake); got != model.ExitCode(code) {
				t.Fatalf("exit %d: %s", got, &out)
			}
			if strings.Count(out.String(), "\n") != 1 {
				t.Fatal(out.String())
			}
			line, err := wire.DecodeWatchLine(out.Bytes())
			if err != nil {
				t.Fatal(err)
			}
			if line.(*wire.WatchErrorLine).Error.Code != code {
				t.Fatal(line)
			}
		})
	}
	for _, input := range []string{`{"op":"watch"}`, `{"protocol":1,"op":"watch","unknown":1}`, `{"protocol":2,"op":"watch"}`, `bad`} {
		var out bytes.Buffer
		if code := Run(context.Background(), "watch", strings.NewReader(input), &out, &service.Fake{}); code == 0 {
			t.Fatal("expected error")
		}
		var line wire.WatchErrorLine
		if err := model.DecodeStrict(out.Bytes(), &line); err != nil {
			t.Fatal(err)
		}
		if line.Type != "error" {
			t.Fatal(out.String())
		}
	}
}

func TestWatchTrailingInputAndCancellation(t *testing.T) {
	for _, input := range []string{`{"protocol":1,"op":"watch"} {}`, `{"protocol":1,"op":"watch"} garbage`} {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		fake := &service.Fake{WatchFunc: func(ctx context.Context, _ wire.WatchRequest, _ func(wire.WatchLine) error) error {
			<-ctx.Done()
			return ctx.Err()
		}}
		var out bytes.Buffer
		if got := Run(ctx, "watch", strings.NewReader(input), &out, fake); got != 2 {
			t.Fatalf("exit %d: %s", got, &out)
		}
		cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	r, w := io.Pipe()
	defer r.Close()
	defer w.Close()
	started := make(chan struct{})
	fake := &service.Fake{WatchFunc: func(ctx context.Context, _ wire.WatchRequest, _ func(wire.WatchLine) error) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}}
	done := make(chan int, 1)
	go func() { done <- Run(ctx, "watch", r, io.Discard, fake) }()
	_, _ = io.WriteString(w, `{"protocol":1,"op":"watch"}`)
	<-started
	cancel()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatal(code)
		}
	case <-time.After(time.Second):
		t.Fatal("context cancellation did not stop watch")
	}
}
