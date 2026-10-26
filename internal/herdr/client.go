package herdr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

// DefaultTimeout bounds each call that doesn't take its own timeout.
const DefaultTimeout = 15 * time.Second

// Client runs the Herdr CLI against one named server (Herdr calls it a
// session). It holds no connection; each method is one or a few `herdr`
// processes. A Client is safe for concurrent use.
type Client struct {
	bin     string
	session string
	// Timeout bounds each call except StartAgent and Wait, which take their
	// own. Zero means DefaultTimeout. A call that runs out is herdr_unavailable
	// (not_running): the server didn't answer.
	Timeout time.Duration
}

// New returns a client for the Herdr server named session, using the herdr
// executable bin (an empty bin means "herdr" on PATH). Nothing runs until a
// method is called. The session is always passed explicitly, "default"
// included, so HERDR_SESSION and friends can't change which server answers.
func New(bin, session string) *Client {
	return &Client{bin: bin, session: session}
}

// Session returns the server name the client was given.
func (c *Client) Session() string { return c.session }

func (c *Client) timeout() time.Duration {
	if c.Timeout > 0 {
		return c.Timeout
	}
	return DefaultTimeout
}

// resolve finds the herdr executable, or reports it as not installed.
func (c *Client) resolve(op string) (string, error) {
	bin := c.bin
	if bin == "" {
		bin = "herdr"
	}
	path, err := exec.LookPath(bin)
	if err != nil {
		return "", clientError(op, kindNotInstalled, err.Error())
	}
	return path, nil
}

// call runs `herdr --session <session> args…` and returns its stdout. op names
// the command for errors ("agent prompt"). A zero timeout means c.Timeout.
func (c *Client) call(ctx context.Context, timeout time.Duration, op string, args ...string) ([]byte, error) {
	if err := ValidateSession(c.session); err != nil {
		return nil, err
	}
	for _, a := range args {
		if err := checkArg(a); err != nil {
			return nil, err
		}
	}
	bin, err := c.resolve(op)
	if err != nil {
		return nil, err
	}
	if timeout <= 0 {
		timeout = c.timeout()
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(callCtx, bin, append([]string{"--session", c.session}, args...)...)
	cmd.Env = cleanEnv(os.Environ())
	cmd.WaitDelay = time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	if runErr == nil {
		return stdout.Bytes(), nil
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if callCtx.Err() != nil {
		return nil, clientError(op, kindNotRunning, fmt.Sprintf("no answer within %s", timeout))
	}
	var exitErr *exec.ExitError
	if !errors.As(runErr, &exitErr) {
		if errors.Is(runErr, exec.ErrNotFound) || errors.Is(runErr, fs.ErrNotExist) {
			return nil, clientError(op, kindNotInstalled, runErr.Error())
		}
		return nil, clientError(op, kindOther, runErr.Error())
	}
	var env errorEnvelope
	if json.Unmarshal(bytes.TrimSpace(stderr.Bytes()), &env) == nil && env.Error != nil && env.Error.Code != "" {
		return nil, fromHerdr(op, env.Error.Code, env.Error.Message)
	}
	// Exit status 2 is a CLI syntax error: plain text, not JSON.
	message := strings.TrimSpace(stderr.String())
	if message == "" {
		message = runErr.Error()
	}
	return nil, clientError(op, kindOther, message)
}

// result runs a command and decodes its JSON result into out.
func (c *Client) result(ctx context.Context, timeout time.Duration, op string, out any, args ...string) error {
	stdout, err := c.call(ctx, timeout, op, args...)
	if err != nil {
		return err
	}
	var env envelope
	if err := json.Unmarshal(stdout, &env); err != nil || len(env.Result) == 0 {
		return clientError(op, kindOther, fmt.Sprintf("unexpected output: %.200q", stdout))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(env.Result, out); err != nil {
		return clientError(op, kindOther, "unexpected result: "+err.Error())
	}
	return nil
}

// cleanEnv drops every HERDR_* variable. Windows names are case-insensitive.
func cleanEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(strings.ToUpper(name), "HERDR_") {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// checkArg refuses what can't be a single argv element: Herdr reads its
// arguments as UTF-8 (it refuses others with a syntax error) and no OS passes
// a NUL byte.
func checkArg(a string) error {
	if !utf8.ValidString(a) {
		return model.NewError(model.ErrorInvalidRequest, "a Herdr argument isn't valid UTF-8").
			WithDetail("reason", "not_utf8")
	}
	if strings.IndexByte(a, 0) >= 0 {
		return model.NewError(model.ErrorInvalidRequest, "a Herdr argument contains a NUL byte").
			WithDetail("reason", "nul_byte")
	}
	return nil
}

// envArgs renders --env KEY=VALUE pairs in key order. Keys are non-empty, have
// no '=', and aren't HERDR_*, which Herdr sets in its panes itself.
func envArgs(env map[string]string) ([]string, error) {
	keys := make([]string, 0, len(env))
	for k := range env {
		if k == "" || strings.ContainsRune(k, '=') || strings.HasPrefix(strings.ToUpper(k), "HERDR_") {
			return nil, model.NewError(model.ErrorInvalidRequest, "invalid pane environment variable name").
				WithDetails(map[string]any{"reason": "env_name", "name": k})
		}
		keys = append(keys, k)
	}
	slices.Sort(keys)
	args := make([]string, 0, 2*len(keys))
	for _, k := range keys {
		args = append(args, "--env", k+"="+env[k])
	}
	return args, nil
}
