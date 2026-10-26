// Package agentops shares credential loading and typed tool dispatch between agent faces.
package agentops

import (
	"bytes"
	"context"
	"io"
	"os"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/service"
)

// Identity is fixed per process, but the file is read for every operation.
type Identity struct {
	Member    string
	TokenFile string
}

// Authenticate reads a single token line and hashes the encoded token. Missing
// identities and file failures reach the authenticator as nil, so it owns the
// auth.rejected event. Reads are capped at 1 KiB; readable, malformed tokens
// (including oversized files) still reach it for bad_token.
func (i Identity) Authenticate(ctx context.Context, auth service.Authenticator, face model.Face) (service.Caller, error) {
	var token *service.Token
	if i.Member != "" && i.TokenFile != "" {
		if file, err := os.Open(i.TokenFile); err == nil {
			b, readErr := io.ReadAll(io.LimitReader(file, 1<<10))
			_ = file.Close()
			if readErr != nil {
				return auth.Authenticate(ctx, i.Member, nil, face)
			}
			if bytes.HasSuffix(b, []byte("\r\n")) {
				b = b[:len(b)-2]
			} else {
				b = bytes.TrimSuffix(b, []byte("\n"))
			}
			if len(b) > 0 {
				token = &service.Token{Bytes: b, Hash: model.HashToken(string(b))}
			}
		}
	}
	return auth.Authenticate(ctx, i.Member, token, face)
}
