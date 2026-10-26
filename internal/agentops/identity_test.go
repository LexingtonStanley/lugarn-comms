package agentops

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/service"
)

func TestIdentityReadsAndHashesEachCall(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	identity := Identity{Member: "m", TokenFile: path}
	var got *service.Token
	fake := &service.Fake{AuthenticateFunc: func(_ context.Context, member string, token *service.Token, face model.Face) (service.Caller, *model.Error) {
		if member != "m" || face != model.FaceCLI {
			t.Fatalf("identity %s %s", member, face)
		}
		got = token
		return service.Caller{MemberID: member}, nil
	}}
	for _, text := range []string{"first\n", "second\r\n", "malformed", ""} {
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
		_, _ = identity.Authenticate(context.Background(), fake, model.FaceCLI)
		if text == "" {
			if got != nil {
				t.Fatal("empty token must be absent")
			}
			continue
		}
		want := text
		if text == "first\n" {
			want = "first"
		}
		if text == "second\r\n" {
			want = "second"
		}
		if got == nil || string(got.Bytes) != want || got.Hash != model.HashToken(want) {
			t.Fatalf("wrong token: %v", got)
		}
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	_, _ = identity.Authenticate(context.Background(), fake, model.FaceCLI)
	if got != nil {
		t.Fatal("unreadable token must be absent")
	}
}
