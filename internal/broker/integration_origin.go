package broker

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/statedir"
)

type sessionOrigin struct{ Workdir string }

func (b *Broker) originPath(member, session string) string {
	return filepath.Join(b.opts.StateDir, "sessions", member, model.HashToken(session)+".json")
}
func (b *Broker) rememberOrigin(m model.Member, session string) error {
	if m.Harness == nil || *m.Harness != model.HarnessCodex || session == "" || m.Workdir == nil {
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
	raw, e := json.Marshal(sessionOrigin{Workdir: workdir})
	if e != nil {
		return e
	}
	if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	return statedir.WriteSecret(path, raw)
}
func (b *Broker) assertCodexWorkdir(m model.Member) error {
	if m.Harness == nil || *m.Harness != model.HarnessCodex {
		return nil
	}
	fail := func() error {
		return model.NewError(model.ErrorHarnessUnavailable, "Codex pane workdir must match its recorded thread workdir").WithDetail("reason", "unresolvable")
	}
	if m.Session == nil || m.Workdir == nil {
		return fail()
	}
	raw, e := os.ReadFile(b.originPath(m.ID, *m.Session))
	if e != nil {
		return fail()
	}
	var origin sessionOrigin
	if json.Unmarshal(raw, &origin) != nil {
		return fail()
	}
	actual, e := filepath.EvalSymlinks(*m.Workdir)
	if e != nil {
		return fail()
	}
	equal := filepath.Clean(actual) == filepath.Clean(origin.Workdir)
	if runtime.GOOS == "windows" {
		equal = strings.EqualFold(filepath.Clean(actual), filepath.Clean(origin.Workdir))
	}
	if !equal {
		return fail()
	}
	return nil
}
