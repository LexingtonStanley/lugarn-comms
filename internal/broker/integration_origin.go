package broker

import (
	"bufio"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/statedir"
)

type sessionOrigin struct {
	Workdir string
	Started bool
}

func (b *Broker) originPath(member, session string) string {
	return filepath.Join(b.opts.StateDir, "sessions", member, model.HashToken(session)+".json")
}
func (b *Broker) rememberOrigin(m model.Member, session string) error {
	if m.Harness == nil || (*m.Harness != model.HarnessCodex && *m.Harness != model.HarnessClaude) || session == "" || m.Workdir == nil {
		return nil
	}
	path := b.originPath(m.ID, session)
	if _, e := os.Lstat(path); e == nil {
		return nil
	} else if !os.IsNotExist(e) {
		return e
	}
	workdir, e := filepath.EvalSymlinks(*m.Workdir)
	if e != nil {
		return e
	}
	raw, e := json.Marshal(sessionOrigin{Workdir: workdir, Started: true})
	if e != nil {
		return e
	}
	if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	return statedir.WriteSecret(path, raw)
}

// codexRolloutCWD opens only a matching regular rollout, and parses only its
// first session_meta line. No config, auth store or other CODEX_HOME file is read.
func codexRolloutCWD(session string) (string, error) {
	if _, e := uuid.Parse(session); e != nil {
		return "", e
	}
	home := os.Getenv("CODEX_HOME")
	if home == "" {
		user, e := os.UserHomeDir()
		if e != nil {
			return "", e
		}
		home = filepath.Join(user, ".codex")
	}
	var cwd string
	err := filepath.WalkDir(filepath.Join(home, "sessions"), func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasPrefix(d.Name(), "rollout-") || !strings.HasSuffix(d.Name(), "-"+session+".jsonl") {
			return nil
		}
		info, e := os.Lstat(path)
		if e != nil {
			return e
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		file, e := os.Open(path)
		if e != nil {
			return e
		}
		defer file.Close()
		line, e := bufio.NewReader(io.LimitReader(file, 1024*1024)).ReadBytes('\n')
		if e != nil && e != io.EOF {
			return e
		}
		var meta struct {
			Type    string `json:"type"`
			Payload struct {
				ID  string `json:"id"`
				CWD string `json:"cwd"`
			} `json:"payload"`
		}
		if json.Unmarshal(line, &meta) != nil || meta.Type != "session_meta" || meta.Payload.ID != session || !filepath.IsAbs(meta.Payload.CWD) {
			return errors.New("invalid Codex session_meta")
		}
		cwd = meta.Payload.CWD
		return filepath.SkipAll
	})
	if err != nil {
		return "", err
	}
	if cwd == "" {
		return "", os.ErrNotExist
	}
	return cwd, nil
}
func (b *Broker) assertCodexWorkdir(m model.Member) error {
	if m.Harness == nil || *m.Harness != model.HarnessCodex {
		return nil
	}
	fail := func(cause string) error {
		return model.NewError(model.ErrorLaunchFailed, "Codex pane workdir precondition failed").WithDetail("cause", cause)
	}
	if m.Workdir == nil {
		return fail("workdir_missing")
	}
	actual, e := filepath.EvalSymlinks(*m.Workdir)
	if e != nil {
		return fail("workdir_missing")
	}
	if stat, e := os.Stat(actual); e != nil || !stat.IsDir() {
		return fail("workdir_missing")
	}
	if m.Session == nil {
		return fail("rollout_missing")
	}
	recorded, e := codexRolloutCWD(*m.Session)
	if e != nil {
		return fail("rollout_missing")
	}
	origin, e := filepath.EvalSymlinks(recorded)
	if e != nil {
		return fail("workdir_missing")
	}
	if !sameNativePath(actual, origin) {
		return fail("workdir_changed")
	}
	return nil
}

func (b *Broker) sessionStarted(member, session string) bool {
	raw, e := os.ReadFile(b.originPath(member, session))
	if e != nil {
		return false
	}
	var origin sessionOrigin
	return json.Unmarshal(raw, &origin) == nil && origin.Started
}
