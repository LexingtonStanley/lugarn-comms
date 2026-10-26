package main

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/statedir"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

var binaryPath, probeSearch string

func TestMain(m *testing.M) {
	dir, e := os.MkdirTemp("", "lugarn-broker-e2e-")
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
	name := "lugarn-comms"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binaryPath = filepath.Join(dir, name)
	cmd := exec.Command("go", "build", "-o", binaryPath, ".")
	if out, e := cmd.CombinedOutput(); e != nil {
		fmt.Fprintf(os.Stderr, "build e2e binary: %v\n%s", e, out)
		os.RemoveAll(dir)
		os.Exit(1)
	}
	// A native fake Herdr executable shadows any real Herdr even when git's
	// directory is on PATH. It rejects probe argv without opening any store.
	herdrName := "herdr"
	if runtime.GOOS == "windows" {
		herdrName += ".exe"
	}
	data, e := os.ReadFile(binaryPath)
	if e == nil {
		e = os.WriteFile(filepath.Join(dir, herdrName), data, 0700)
	}
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.RemoveAll(dir)
		os.Exit(1)
	}
	probeSearch = dir
	if git, e := exec.LookPath("git"); e == nil {
		probeSearch += string(os.PathListSeparator) + filepath.Dir(git)
	}
	result := m.Run()
	os.RemoveAll(dir)
	os.Exit(result)
}
func eptr[T any](v T) *T { return &v }
func emust(t *testing.T, e error) {
	t.Helper()
	if e != nil {
		t.Fatal(e)
	}
}
func decode[T any](t *testing.T, b []byte) T {
	t.Helper()
	var v T
	emust(t, json.Unmarshal(b, &v))
	return v
}
func childEnv(home, search string) []string {
	var env []string
	for _, s := range os.Environ() {
		key, _, _ := strings.Cut(s, "=")
		if strings.EqualFold(key, "PATH") || strings.HasPrefix(strings.ToUpper(key), "LUGARN_COMMS_") {
			continue
		}
		env = append(env, s)
	}
	return append(env, "PATH="+search, "LUGARN_COMMS_HOME="+home)
}

type processResult struct {
	out, diag []byte
	code      int
	err       error
}

func process(cmd *exec.Cmd, input []byte) processResult {
	var out, diag bytes.Buffer
	cmd.Stdin = bytes.NewReader(input)
	cmd.Stdout = &out
	cmd.Stderr = &diag
	e := cmd.Run()
	code := 0
	if e != nil {
		var exit *exec.ExitError
		if errors.As(e, &exit) {
			code = exit.ExitCode()
		} else {
			code = -1
		}
	}
	return processResult{out.Bytes(), diag.Bytes(), code, e}
}
func checked(t *testing.T, r processResult, want int) []byte {
	t.Helper()
	if r.code != want {
		t.Fatalf("exit %d want %d (%v)\nstdout: %s\nstderr: %s", r.code, want, r.err, r.out, r.diag)
	}
	return r.out
}

type e2e struct {
	home, search string
	spec         model.TeamSpec
	db           *store.Store
	tokens       map[string]string
	launches     map[string]string
	ctx          context.Context
}

func (f *e2e) cmd(args ...string) *exec.Cmd {
	c := exec.CommandContext(f.ctx, binaryPath, args...)
	c.Env = childEnv(f.home, f.search)
	return c
}
func (f *e2e) app(t *testing.T, op string, args map[string]any) wire.Response {
	t.Helper()
	if args == nil {
		args = map[string]any{}
	}
	args["protocol"] = 1
	args["op"] = op
	b, e := json.Marshal(args)
	emust(t, e)
	return decode[wire.Response](t, checked(t, process(f.cmd("app", op), b), 0))
}
func (f *e2e) agent(name string, args ...string) *exec.Cmd {
	m := ""
	for _, v := range f.spec.Members {
		if v.Name == name {
			m = v.ID
		}
	}
	return f.cmd(append([]string{"--member", m, "--token-file", f.tokens[name]}, args...)...)
}
func fixture(t *testing.T, hop int) *e2e {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	t.Cleanup(cancel)
	f := &e2e{home: t.TempDir(), search: probeSearch, ctx: ctx, tokens: map[string]string{}, launches: map[string]string{}}
	t.Setenv("LUGARN_COMMS_HOME", f.home)
	hello := f.app(t, "hello", map[string]any{"protocols": []int{1}, "app": map[string]any{"version": "e2e"}}).Result.(*wire.HelloResult)
	if hello.Schemas.Store != 1 || hello.Host.StateDir != f.home || hello.Herdr.Running {
		t.Fatal(hello)
	}
	contents, e := os.ReadFile(filepath.Join("..", "..", "schema", "examples", "team-spec", "writer-reviewer.json"))
	emust(t, e)
	f.spec = decode[model.TeamSpec](t, contents)
	for i := range f.spec.Members {
		f.spec.Members[i].Folder = t.TempDir()
	}
	f.spec.Limits.HopBudget = &hop
	f.app(t, "apply-team", map[string]any{"spec": f.spec, "if_revision": 0})
	get := f.app(t, "get-team", map[string]any{"team": f.spec.ID}).Result.(*wire.GetTeamResult)
	if get.Revision != 1 || get.Spec.Members[0].Folder != f.spec.Members[0].Folder || get.Spec.Herdr == nil {
		t.Fatal(get)
	}
	f.spec = get.Spec
	f.db, e = store.Open(ctx, filepath.Join(f.home, "comms.db"))
	emust(t, e)
	t.Cleanup(func() { f.db.Close() })
	// C2 stand-in: starts a run, members, launches, leases and token files.
	now := model.Time{Time: time.Now()}
	emust(t, f.db.Write(ctx, func(tx *store.Tx) error {
		if _, e := tx.BumpTeamRun(f.spec.ID, now); e != nil {
			return e
		}
		if e := tx.UpdateTeam(f.spec.ID, store.TeamPatch{State: eptr(model.TeamStateRunning), StartedAt: eptr(&now)}, now); e != nil {
			return e
		}
		return tx.Heartbeat(f.spec.ID, model.Runner{State: model.RunnerStateUp, StartedAt: &now, HeartbeatAt: &now}, now)
	}))
	for _, m := range f.spec.Members {
		id, e := model.NewULID()
		emust(t, e)
		token, e := model.NewToken()
		emust(t, e)
		f.launches[m.Name] = id
		path := filepath.Join(f.home, "launch", id+".token")
		f.tokens[m.Name] = path
		emust(t, statedir.WriteSecret(path, []byte(token+"\n")))
		emust(t, f.db.Write(ctx, func(tx *store.Tx) error {
			if e := tx.SetMemberState(m.ID, model.MemberStateRunning, nil, eptr(&now), nil, now); e != nil {
				return e
			}
			if e := tx.InsertLaunch(store.Launch{ID: id, MemberID: m.ID, Generation: 1, Driver: model.ModeHeadless, StartedAt: now}); e != nil {
				return e
			}
			if e := tx.CompareAndSwapLease(m.ID, model.DriverNone, 0, model.Lease{Driver: model.DriverHeadless, Generation: 1, Launch: &id}, now); e != nil {
				return e
			}
			return tx.InsertToken(store.Token{LaunchID: id, MemberID: m.ID, Hash: model.HashToken(token), CreatedAt: now})
		}))
	}
	stop := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		for {
			select {
			case <-stop:
				done <- nil
				return
			case <-ctx.Done():
				done <- ctx.Err()
				return
			case <-tick.C:
				now := model.Time{Time: time.Now()}
				if e := f.db.Write(ctx, func(tx *store.Tx) error {
					return tx.Heartbeat(f.spec.ID, model.Runner{State: model.RunnerStateUp, StartedAt: &now, HeartbeatAt: &now}, now)
				}); e != nil {
					done <- e
					return
				}
			}
		}
	}()
	t.Cleanup(func() {
		close(stop)
		if e := <-done; e != nil && ctx.Err() == nil {
			t.Error(e)
		}
	})
	return f
}
func (f *e2e) deliver(t *testing.T) {
	t.Helper()
	emust(t, f.db.Write(f.ctx, func(tx *store.Tx) error {
		ds, e := tx.ListTeamDeliveries(f.spec.ID, model.DeliveryStateQueued)
		if e != nil {
			return e
		}
		now := model.Time{Time: time.Now()}
		for _, d := range ds {
			if e = tx.UpdateDelivery(d.MessageID, d.Delivery.To.ID, model.DeliveryStateQueued, store.DeliveryPatch{State: model.DeliveryStateDelivered, Via: eptr(eptr(model.DeliveryViaHeadless)), DeliveredAt: eptr(&now), UpdatedAt: now}); e != nil {
				return e
			}
		}
		return nil
	}))
}
func TestBinaryAppCLIAndMCP(t *testing.T) {
	f := fixture(t, 100)
	first := decode[wire.SendOutput](t, checked(t, process(f.agent("writer", "send", "reviewer", "please review"), nil), 0))
	f.deliver(t)
	reply := decode[wire.ReplyOutput](t, checked(t, process(f.agent("reviewer", "reply", first.ID, "reviewed"), nil), 0))
	f.deliver(t)
	checked(t, process(f.agent("writer", "reply", reply.ID, "thanks"), nil), 0)
	cmd := f.cmd("mcp", "--member", f.spec.Members[1].ID, "--token-file", f.tokens["reviewer"])
	cs, e := mcp.NewClient(&mcp.Implementation{Name: "e2e", Version: "1"}, nil).Connect(f.ctx, &mcp.CommandTransport{Command: cmd}, nil)
	emust(t, e)
	defer cs.Close()
	listed, e := cs.ListTools(f.ctx, nil)
	emust(t, e)
	if len(listed.Tools) != 6 {
		t.Fatal(listed)
	}
	for _, v := range []struct {
		name string
		args map[string]any
		def  string
	}{{"send", map[string]any{"to": "writer", "text": "MCP review"}, "SendOutput"}, {"inbox", map[string]any{}, "InboxOutput"}, {"team", map[string]any{}, "TeamOutput"}} {
		r, e := cs.CallTool(f.ctx, &mcp.CallToolParams{Name: v.name, Arguments: v.args})
		emust(t, e)
		if r.IsError {
			t.Fatal(r)
		}
		raw, e := json.Marshal(r.StructuredContent)
		emust(t, e)
		schema, e := wire.ToolSchema(v.name, true)
		emust(t, e)
		compiler := jsonschema.NewCompiler()
		emust(t, compiler.AddResource("urn:result", schema))
		compiled, e := compiler.Compile("urn:result")
		emust(t, e)
		value, e := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
		emust(t, e)
		emust(t, compiled.Validate(value))
	}
	person := f.app(t, "send", map[string]any{"team": f.spec.ID, "to": f.spec.Members[0].ID, "text": "person task"}).Result.(*wire.SendResult)
	if person.Message.From.Name != "human" {
		t.Fatal(person)
	}
	timeline := f.app(t, "timeline", map[string]any{"team": f.spec.ID}).Result.(*wire.TimelineResult)
	if len(timeline.Events) < 4 {
		t.Fatal(timeline)
	}
	members := f.app(t, "members", map[string]any{"team": f.spec.ID}).Result.(*wire.MembersResult)
	if len(members.Members) != 3 {
		t.Fatal(members)
	}
	status := f.app(t, "status", map[string]any{"team": f.spec.ID}).Result.(*wire.StatusResult)
	if status.Teams[0].Counts.Running != 2 {
		t.Fatal(status)
	}
	// Revocation is read afresh on an existing MCP session too.
	emust(t, f.db.Write(f.ctx, func(tx *store.Tx) error { return tx.RevokeToken(f.launches["reviewer"], model.Time{Time: time.Now()}) }))
	r, e := cs.CallTool(f.ctx, &mcp.CallToolParams{Name: "team"})
	emust(t, e)
	if !r.IsError {
		t.Fatal("revoked MCP token accepted")
	}
}
func TestBinaryConcurrentBudget(t *testing.T) {
	f := fixture(t, 5)
	results := make(chan processResult, 8)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results <- process(f.agent("writer", "send", "reviewer", fmt.Sprintf("message %d", i)), nil)
		}(i)
	}
	wg.Wait()
	close(results)
	queued, held := 0, 0
	for r := range results {
		v := decode[wire.SendOutput](t, checked(t, r, 0))
		switch v.State {
		case model.DeliveryStateQueued:
			queued++
		case model.DeliveryStateHeld:
			held++
			if v.Reason == nil || *v.Reason != model.DeliveryReasonBudget {
				t.Fatal(v)
			}
		default:
			t.Fatal(v)
		}
	}
	if queued != 5 || held != 3 {
		t.Fatalf("queued %d held %d", queued, held)
	}
	var waiting []string
	emust(t, f.db.Read(f.ctx, func(tx *store.Tx) error {
		ds, e := tx.ListTeamDeliveries(f.spec.ID)
		if e != nil {
			return e
		}
		for i, d := range ds {
			m, e := tx.GetMessage(d.MessageID)
			if e != nil {
				return e
			}
			if m.Hop == nil || *m.Hop != int64(i+1) {
				t.Fatalf("hop order: %#v", m)
			}
			if d.Delivery.State == model.DeliveryStateHeld {
				waiting = append(waiting, m.ID)
			}
		}
		return nil
	}))
	out := f.app(t, "continue-budget", map[string]any{"team": f.spec.ID}).Result.(*wire.ContinueBudgetResult)
	if out.Released != 3 || out.Hops.Allowance != 10 {
		t.Fatal(out)
	}
	var released []string
	emust(t, f.db.Read(f.ctx, func(tx *store.Tx) error {
		events, e := tx.ReplayEvents(0, eptr(f.spec.ID), 0)
		if e != nil {
			return e
		}
		for _, ev := range events {
			if ev.Type == model.EventDeliveryChanged {
				var d wire.DeliveryChangedEventData
				if e = json.Unmarshal(ev.Data, &d); e != nil {
					return e
				}
				released = append(released, d.Message)
			}
		}
		return nil
	}))
	if fmt.Sprint(waiting) != fmt.Sprint(released) {
		t.Fatalf("release order %v want %v", released, waiting)
	}
}
func TestBinaryPolicyAndAuth(t *testing.T) {
	f := fixture(t, 100)
	f.spec.Grid = []model.GridEdge{}
	f.app(t, "apply-team", map[string]any{"spec": f.spec, "if_revision": 1})
	raw := checked(t, process(f.agent("writer", "send", "reviewer", "denied"), nil), 20)
	denied := decode[wire.ToolError](t, raw)
	id := denied.Error.Details["message_id"].(string)
	emust(t, f.db.Read(f.ctx, func(tx *store.Tx) error {
		m, e := tx.GetMessage(id)
		if e != nil {
			return e
		}
		if m.Deliveries[0].State != model.DeliveryStateDenied {
			t.Fatal(m)
		}
		events, e := tx.ReplayEvents(0, eptr(f.spec.ID), 0)
		if e != nil {
			return e
		}
		if events[len(events)-1].Type != model.EventPolicyDenied {
			t.Fatal(events)
		}
		return nil
	}))
	checked(t, process(f.agent("writer", "send", "human", strings.Repeat("界", 8001)), nil), 2)
	for i := 0; i < 6; i++ {
		m := decode[wire.SendOutput](t, checked(t, process(f.agent("writer", "send", "human", "help"), nil), 0))
		emust(t, f.db.Read(f.ctx, func(tx *store.Tx) error {
			v, e := tx.GetMessage(m.ID)
			if e != nil {
				return e
			}
			if v.Deliveries[0].Notify == nil || *v.Deliveries[0].Notify != (i < 5) {
				t.Fatal(v)
			}
			return nil
		}))
	}
	checked(t, process(f.cmd("--member", f.spec.Members[0].ID, "team"), nil), 3)
	emust(t, f.db.Write(f.ctx, func(tx *store.Tx) error { return tx.RevokeToken(f.launches["writer"], model.Time{Time: time.Now()}) }))
	auth := decode[wire.ToolError](t, checked(t, process(f.agent("writer", "team"), nil), 3))
	if auth.Error.Code != model.ErrorUnauthenticated || auth.Error.Details["reason"] != "launch_ended" {
		t.Fatal(auth)
	}
	emust(t, f.db.Read(f.ctx, func(tx *store.Tx) error {
		es, e := tx.ReplayEvents(0, eptr(f.spec.ID), 0)
		if e != nil {
			return e
		}
		var reasons []model.AuthFailure
		for _, ev := range es {
			if ev.Type == model.EventAuthRejected {
				var d wire.AuthRejectedEventData
				if e = json.Unmarshal(ev.Data, &d); e != nil {
					return e
				}
				reasons = append(reasons, d.Failure)
			}
		}
		if fmt.Sprint(reasons) != fmt.Sprint([]model.AuthFailure{model.AuthFailureNoToken, model.AuthFailureRevoked}) {
			t.Fatal(reasons)
		}
		return nil
	}))
}
func TestBinaryWatch(t *testing.T) {
	f := fixture(t, 100)
	snapshot := f.app(t, "status", map[string]any{"team": f.spec.ID}).Result.(*wire.StatusResult)
	checked(t, process(f.agent("writer", "send", "reviewer", "replay"), nil), 0)
	cmd := f.cmd("app", "watch")
	stdin, e := cmd.StdinPipe()
	emust(t, e)
	stdout, e := cmd.StdoutPipe()
	emust(t, e)
	var diag bytes.Buffer
	cmd.Stderr = &diag
	emust(t, cmd.Start())
	defer func() { stdin.Close(); cmd.Process.Kill() }()
	lines := make(chan []byte, 16)
	go func() {
		scan := bufio.NewScanner(stdout)
		scan.Buffer(make([]byte, 4096), 1<<20)
		for scan.Scan() {
			lines <- append([]byte(nil), scan.Bytes()...)
		}
		close(lines)
	}()
	raw, e := json.Marshal(wire.WatchRequest{Protocol: 1, Op: wire.OpNameWatch, Team: eptr(f.spec.ID), After: &snapshot.Cursor})
	emust(t, e)
	_, e = stdin.Write(raw)
	emust(t, e)
	next := func() wire.WatchLine {
		t.Helper()
		select {
		case raw, ok := <-lines:
			if !ok {
				t.Fatalf("watch ended early: %s", &diag)
			}
			v, e := wire.DecodeWatchLine(raw)
			emust(t, e)
			return v
		case <-f.ctx.Done():
			t.Fatal("watch timeout")
			return nil
		}
	}
	prev, e := model.ParseCursor(snapshot.Cursor, strings.Split(snapshot.Cursor, ".")[0])
	emust(t, e)
	count := 0
	for {
		v := next()
		if _, ok := v.(*wire.WatchLiveLine); ok {
			break
		}
		ev, ok := v.(*wire.Event)
		if !ok {
			t.Fatalf("unexpected line %T", v)
		}
		seq, e := model.ParseCursor(ev.Cursor, strings.Split(snapshot.Cursor, ".")[0])
		emust(t, e)
		if seq <= prev {
			t.Fatal("watch reordered events")
		}
		prev = seq
		count++
	}
	if count != 2 {
		t.Fatalf("replay events %d", count)
	}
	checked(t, process(f.agent("writer", "send", "reviewer", "live"), nil), 0)
	v := next()
	ev, ok := v.(*wire.Event)
	if !ok || ev.Type != model.EventMessageCreated {
		t.Fatalf("live: %#v", v)
	}
	emust(t, stdin.Close())
	emust(t, cmd.Wait())
}

func TestBinaryNewerStoreErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	f := &e2e{home: t.TempDir(), search: probeSearch, ctx: ctx}
	emust(t, statedir.Ensure(f.home))
	path := filepath.Join(f.home, "comms.db")
	db, e := store.Open(ctx, path)
	emust(t, e)
	emust(t, db.Close())
	raw, e := sql.Open("sqlite", path)
	emust(t, e)
	_, e = raw.ExecContext(ctx, "INSERT INTO schema_migrations(version,name,applied_at) VALUES (999, 'future', '2026-10-26T00:00:00.000Z')")
	emust(t, e)
	emust(t, raw.Close())
	app := decode[wire.Response](t, checked(t, process(f.cmd("app", "status"), []byte(`{"protocol":1,"op":"status","request_id":"newer"}`)), 4))
	if app.Error == nil || app.Error.Code != model.ErrorUnsupportedVersion || app.RequestID == nil || *app.RequestID != "newer" {
		t.Fatal(app)
	}
	cli := decode[wire.ToolError](t, checked(t, process(f.cmd("team"), nil), 4))
	if cli.Error.Code != model.ErrorUnsupportedVersion {
		t.Fatal(cli)
	}
	cmd := f.cmd("mcp", "--member", "writer", "--token-file", filepath.Join(f.home, "missing.token"))
	session, e := mcp.NewClient(&mcp.Implementation{Name: "newer", Version: "1"}, nil).Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	emust(t, e)
	defer session.Close()
	result, e := session.CallTool(ctx, &mcp.CallToolParams{Name: "team"})
	emust(t, e)
	if !result.IsError {
		t.Fatal(result)
	}
	data, e := json.Marshal(result.StructuredContent)
	emust(t, e)
	if decode[wire.ToolError](t, data).Error.Code != model.ErrorUnsupportedVersion {
		t.Fatal(string(data))
	}
}

func TestBinaryMCPDerivesStoreFromTokenPath(t *testing.T) {
	f := fixture(t, 100)
	cmd := f.cmd("mcp", "--member", f.spec.Members[0].ID, "--token-file", f.tokens["writer"])
	var env []string
	for _, entry := range cmd.Env {
		if !strings.HasPrefix(entry, "LUGARN_COMMS_HOME=") {
			env = append(env, entry)
		}
	}
	cmd.Env = env
	session, e := mcp.NewClient(&mcp.Implementation{Name: "stripped-env", Version: "1"}, nil).Connect(f.ctx, &mcp.CommandTransport{Command: cmd}, nil)
	emust(t, e)
	defer session.Close()
	result, e := session.CallTool(f.ctx, &mcp.CallToolParams{Name: "team"})
	emust(t, e)
	if result.IsError {
		t.Fatal(result)
	}
	raw, e := json.Marshal(result.StructuredContent)
	emust(t, e)
	team := decode[wire.TeamOutput](t, raw)
	if team.Team.ID != f.spec.ID {
		t.Fatal(team)
	}
}
