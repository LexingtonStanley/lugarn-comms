package agentops

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/service"
)

func TestIdentityReadsAndHashesEachCall(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	identity := Identity{Member: "m", TokenFile: path}
	var got *service.Token
	fake := &service.Fake{AuthenticateFunc: func(_ context.Context, member string, token *service.Token, face model.Face) (service.Caller, error) {
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
		if _, err := identity.Authenticate(context.Background(), fake, model.FaceCLI); err != nil {
			t.Fatal(err)
		}
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
	if _, err := identity.Authenticate(context.Background(), fake, model.FaceCLI); err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatal("unreadable token must be absent")
	}
}

func TestIdentityBoundsTokenRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	identity := Identity{Member: "m", TokenFile: path}
	token, _ := model.NewToken()
	for _, size := range []int{1023, 1024, 1025, 8192} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			content := token + strings.Repeat("x", size-len(token))
			if err := os.WriteFile(path, []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			fake := &service.Fake{AuthenticateFunc: func(_ context.Context, _ string, got *service.Token, _ model.Face) (service.Caller, error) {
				if got == nil {
					t.Fatal("malformed token must reach authenticator, not nil")
				}
				want := content
				if len(want) > 1024 {
					want = want[:1024]
				}
				if string(got.Bytes) != want || got.Hash != model.HashToken(want) {
					t.Fatalf("wrong bounded token: %d bytes", len(got.Bytes))
				}
				if model.ValidateToken(string(got.Bytes)) == nil {
					t.Fatal("oversized file became a valid token")
				}
				return service.Caller{}, model.NewError(model.ErrorUnauthenticated, "malformed token").WithDetail("reason", "bad_token")
			}}
			_, err := identity.Authenticate(context.Background(), fake, model.FaceCLI)
			got := model.AsError(err)
			if got == nil || got.Code != model.ErrorUnauthenticated || got.Details["reason"] != "bad_token" {
				t.Fatalf("wrong authentication result: %v", err)
			}
		})
	}
}
