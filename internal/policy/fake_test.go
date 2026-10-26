package policy

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

// fakeHost answers Host from maps. Nothing touches the real machine.
type fakeHost struct {
	os       model.HostOS
	folders  map[string]bool
	repos    map[string]string // path → repo top level
	issues   map[model.Harness]model.HarnessIssue
	paths    map[string]bool
	branches map[string]bool // root + "|" + branch
}

func newHost(os model.HostOS) *fakeHost {
	return &fakeHost{os: os, folders: map[string]bool{}, repos: map[string]string{},
		issues: map[model.Harness]model.HarnessIssue{}, paths: map[string]bool{}, branches: map[string]bool{}}
}

func (h *fakeHost) OS() model.HostOS                                { return h.os }
func (h *fakeHost) FolderExists(p string) bool                      { return h.folders[p] }
func (h *fakeHost) HarnessIssue(x model.Harness) model.HarnessIssue { return h.issues[x] }
func (h *fakeHost) PathExists(p string) bool                        { return h.paths[p] || h.folders[p] }
func (h *fakeHost) BranchExists(root, b string) bool                { return h.branches[root+"|"+b] }
func (h *fakeHost) RepoRoot(p string) (string, bool) {
	r, ok := h.repos[p]
	return r, ok
}

// repo marks folder as an existing directory inside the repository root.
func (h *fakeHost) repo(folder, root string) *fakeHost {
	h.folders[folder] = true
	h.repos[folder] = root
	return h
}

func (h *fakeHost) dir(folder string) *fakeHost {
	h.folders[folder] = true
	return h
}

type memberOpt func(*model.Member)

func agent(id, name string, role model.Role, harness model.Harness, opts ...memberOpt) model.Member {
	m := model.Member{
		ID: id, Team: "t1", Name: name, Kind: model.MemberKindAgent, Role: ptr(role), Origin: model.MemberOriginSpec,
		Harness: ptr(harness), Profile: ptr(model.ProfileWorkspace), Mode: ptr(model.ModeHeadless),
		Folder: ptr("/home/lex/code/flare"), Workdir: ptr("/home/lex/code/flare"), State: model.MemberStateRunning,
	}
	for _, o := range opts {
		o(&m)
	}
	return m
}

func humanMember(id string) model.Member {
	return model.Member{ID: id, Team: "t1", Name: HumanName, Kind: model.MemberKindHuman,
		Origin: model.MemberOriginImplicit, State: model.MemberStateRunning}
}

func state(s model.MemberState) memberOpt { return func(m *model.Member) { m.State = s } }
func profile(p model.Profile) memberOpt   { return func(m *model.Member) { m.Profile = ptr(p) } }
func mode(x model.Mode) memberOpt         { return func(m *model.Member) { m.Mode = ptr(x) } }
func workdir(p string) memberOpt          { return func(m *model.Member) { m.Workdir = ptr(p) } }
func removed() memberOpt {
	return func(m *model.Member) {
		m.RemovedAt = &model.Time{}
		m.RemovedReason = ptr(model.RemovedReasonRemovedFromSpec)
	}
}
func spawnedBy(by model.Member) memberOpt {
	return func(m *model.Member) {
		m.Origin = model.MemberOriginSpawn
		m.SpawnedBy = &model.MemberRef{ID: by.ID, Name: by.Name}
	}
}

func requireCode(t *testing.T, err *model.Error, code model.ErrorCode) {
	t.Helper()
	if err == nil {
		t.Fatalf("got no error; want %s", code)
	}
	if err.Code != code {
		t.Fatalf("got %s (%s); want %s", err.Code, err.Message, code)
	}
}

func requireNoError(t *testing.T, err *model.Error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error %s: %s %v", err.Code, err.Message, err.Details)
	}
}

// fieldErrorPaths returns details.errors' paths.
func fieldErrorPaths(t *testing.T, err *model.Error) []string {
	t.Helper()
	errs, ok := err.Details["errors"].([]FieldError)
	if !ok {
		t.Fatalf("details.errors missing in %v", err.Details)
	}
	paths := make([]string, len(errs))
	for i, e := range errs {
		paths[i] = e.Path
	}
	return paths
}

// jsonOf marshals v the way the store writes events.data, then decodes it
// into a generic value for comparison.
func jsonOf(t *testing.T, v any) map[string]any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func equal(t *testing.T, what string, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s:\n got  %#v\n want %#v", what, got, want)
	}
}
