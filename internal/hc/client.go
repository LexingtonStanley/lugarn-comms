package hc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

var sessionPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func CleanEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, v := range env {
		k, _, _ := strings.Cut(v, "=")
		if !strings.HasPrefix(strings.ToUpper(k), "HC_") {
			out = append(out, v)
		}
	}
	return out
}
func Resolve(binary string) (string, error) {
	if binary != "" {
		return exec.LookPath(binary)
	}
	if p, e := exec.LookPath("herdr-core"); e == nil {
		return p, nil
	}
	home, e := os.UserHomeDir()
	if e != nil {
		return "", e
	}
	return exec.LookPath(filepath.Join(home, ".local", "bin", "herdr-core"))
}

type response struct {
	ID     uint64          `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *Error          `json:"error"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}
type Notification struct {
	Method string
	Params json.RawMessage
}

// Client owns exactly one connect process. Cancellation of a call never retries
// an ambiguous mutation. Subscription reconnects use separate, read-only clients.
type Client struct {
	Hello   Hello
	cmd     *exec.Cmd
	input   io.WriteCloser
	write   sync.Mutex
	mu      sync.Mutex
	next    uint64
	pending map[uint64]chan response
	notes   chan Notification
	done    chan struct{}
	once    sync.Once
	err     error
}

func Connect(ctx context.Context, binary, session string) (*Client, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if !sessionPattern.MatchString(session) {
		return nil, errors.New("invalid hc session")
	}
	path, e := Resolve(binary)
	if e != nil {
		return nil, e
	}
	cmd := exec.Command(path, "connect", "--session", session)
	cmd.Env = CleanEnv(os.Environ())
	in, e := cmd.StdinPipe()
	if e != nil {
		return nil, e
	}
	out, e := cmd.StdoutPipe()
	if e != nil {
		in.Close()
		return nil, e
	}
	c := &Client{cmd: cmd, input: in, pending: map[uint64]chan response{}, notes: make(chan Notification, 256), done: make(chan struct{})}
	if e = cmd.Start(); e != nil {
		in.Close()
		return nil, e
	}
	go c.read(out)
	if e = c.Call(ctx, "hello", map[string]any{"generation": 1}, &c.Hello); e != nil {
		c.Close()
		return nil, e
	}
	if c.Hello.Generation != 1 {
		c.Close()
		return nil, &GenerationError{Hello: c.Hello}
	}
	if c.Hello.Core.ID == "" {
		c.Close()
		return nil, errors.New("hc hello has no core id")
	}
	return c, nil
}
func (c *Client) finish(e error) {
	c.once.Do(func() { c.mu.Lock(); c.err = e; c.mu.Unlock(); close(c.done) })
}
func (c *Client) read(out io.Reader) {
	defer close(c.notes)
	scan := bufio.NewScanner(out)
	scan.Buffer(make([]byte, 4096), 16<<20)
	for scan.Scan() {
		var r response
		if e := json.Unmarshal(scan.Bytes(), &r); e != nil {
			c.finish(e)
			break
		}
		if r.ID != 0 {
			c.mu.Lock()
			ch := c.pending[r.ID]
			c.mu.Unlock()
			if ch != nil {
				ch <- r
			}
			continue
		}
		select {
		case c.notes <- Notification{r.Method, r.Params}:
		default:
			c.finish(errors.New("hc notification overflow"))
		}
		select {
		case <-c.done:
			break
		default:
			continue
		}
		break
	}
	e := scan.Err()
	if e == nil {
		e = io.EOF
	}
	c.finish(e)
	c.input.Close()
	_ = c.cmd.Process.Kill()
	_ = c.cmd.Wait()
}
func (c *Client) Close() error { c.finish(io.EOF); _ = c.input.Close(); return c.cmd.Process.Kill() }

// Call bounds every request on the client side, as the 0.9 client does, so a
// wedged core can't hang a caller whose context has no deadline. A send may be
// held by hc for up to 60 s while the person types a draft; waits pass their own
// bound (see Wait).
func callTimeout(method string) time.Duration {
	if method == "terminal.send" {
		return 90 * time.Second
	}
	return 30 * time.Second
}
func (c *Client) Call(ctx context.Context, method string, params, result any) error {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, callTimeout(method))
		defer cancel()
	}
	c.mu.Lock()
	c.next++
	id := c.next
	ch := make(chan response, 1)
	c.pending[id] = ch
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.pending, id); c.mu.Unlock() }()
	raw, e := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	if e != nil {
		return e
	}
	c.write.Lock()
	_, e = c.input.Write(append(raw, '\n'))
	c.write.Unlock()
	if e != nil {
		return e
	}
	select {
	case r := <-ch:
		if r.Error != nil {
			return r.Error
		}
		return json.Unmarshal(r.Result, result)
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		c.mu.Lock()
		e = c.err
		c.mu.Unlock()
		return e
	}
}
func (c *Client) List(ctx context.Context, labels map[string]string) ([]Terminal, error) {
	if labels == nil {
		labels = map[string]string{}
	}
	var out struct {
		Terminals []Terminal `json:"terminals"`
	}
	e := c.Call(ctx, "terminal.list", map[string]any{"labels": labels, "all": true}, &out)
	local := make([]Terminal, 0, len(out.Terminals))
	for _, t := range out.Terminals {
		if !strings.Contains(t.Address, "/") {
			local = append(local, t)
		}
	}
	return local, e
}
func (c *Client) Get(ctx context.Context, address string) (Terminal, error) {
	var t Terminal
	e := c.Call(ctx, "terminal.get", map[string]any{"terminal": address}, &t)
	return t, e
}
func (c *Client) Run(ctx context.Context, opts RunOptions) (Terminal, error) {
	if opts.Env == nil {
		opts.Env = map[string]string{}
	}
	if opts.Labels == nil {
		opts.Labels = map[string]string{}
	}
	opts.NoParent = true
	opts.NoNotify = true
	opts.Keep = false
	var t Terminal
	e := c.Call(ctx, "terminal.run", opts, &t)
	return t, e
}
func (c *Client) Send(ctx context.Context, address, text string) (SendResult, error) {
	var out SendResult
	e := c.Call(ctx, "terminal.send", map[string]any{"terminal": address, "text": text, "force": false, "wait": false}, &out)
	return out, e
}
func (c *Client) Kill(ctx context.Context, address string) (Terminal, error) {
	var out Terminal
	e := c.Call(ctx, "terminal.kill", map[string]any{"terminal": address}, &out)
	return out, e
}
func (c *Client) Remove(ctx context.Context, address string) error {
	var out struct {
		Removed string `json:"removed"`
	}
	return c.Call(ctx, "terminal.rm", map[string]any{"terminal": address}, &out)
}
func (c *Client) Read(ctx context.Context, address string, lines int) (string, error) {
	var out struct {
		Text string `json:"text"`
	}
	e := c.Call(ctx, "terminal.read", map[string]any{"terminal": address, "lines": lines}, &out)
	return out.Text, e
}
func (c *Client) Wait(ctx context.Context, address string, timeout time.Duration) (WaitResult, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout+10*time.Second)
	defer cancel()
	var out WaitResult
	e := c.Call(ctx, "terminal.wait", map[string]any{"terminal": address, "timeout": timeout.Seconds()}, &out)
	return out, e
}
