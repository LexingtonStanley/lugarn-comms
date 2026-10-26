package harness

import (
	"path/filepath"
	"testing"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

const testLaunchID = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
const testSessionID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"

func testLaunch(t *testing.T, h model.Harness) Launch {
	t.Helper()
	d := t.TempDir()
	l := Launch{Harness: h, Profile: model.ProfileReadOnly, WorkDir: d, StateDir: filepath.Join(d, "state"), LaunchID: testLaunchID, MemberID: "member_1", Binary: filepath.Join(d, "lugarn-comms")}
	if h == model.HarnessClaude {
		l.SessionID = testSessionID
	}
	return l
}

func TestValidate(t *testing.T) {
	for _, h := range (model.Harness("")).Values() {
		l := testLaunch(t, h)
		if err := l.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	for name, mutate := range map[string]func(*Launch){
		"launch traversal":  func(l *Launch) { l.LaunchID = "../other" },
		"member flags":      func(l *Launch) { l.MemberID = "--member x" },
		"relative workdir":  func(l *Launch) { l.WorkDir = "relative" },
		"relative state":    func(l *Launch) { l.StateDir = "relative" },
		"relative binary":   func(l *Launch) { l.Binary = "lugarn-comms" },
		"newline path":      func(l *Launch) { l.Binary += "\n" },
		"session injection": func(l *Launch) { l.SessionID = "--resume" },
		"missing Claude id": func(l *Launch) { l.SessionID = "" },
		"profile":           func(l *Launch) { l.Profile = "other" },
		"harness":           func(l *Launch) { l.Harness = "other" },
		"identity override": func(l *Launch) { l.ExtraEnv = map[string]string{"lugarn_comms_member": "other"} },
		"config override":   func(l *Launch) { l.ExtraEnv = map[string]string{"OPENCODE_CONFIG_CONTENT": "{}"} },
		"env NUL":           func(l *Launch) { l.ExtraEnv = map[string]string{"X": "a\x00b"} },
		"env key":           func(l *Launch) { l.ExtraEnv = map[string]string{"X=Y": "z"} },
		"resume absent":     func(l *Launch) { l.Harness = model.HarnessCodex; l.SessionID = ""; l.Resume = true },
		"unassigned Codex":  func(l *Launch) { l.Harness = model.HarnessCodex },
	} {
		t.Run(name, func(t *testing.T) {
			l := testLaunch(t, model.HarnessClaude)
			mutate(&l)
			if l.Validate() == nil {
				t.Fatal("accepted invalid launch")
			}
		})
	}
}
