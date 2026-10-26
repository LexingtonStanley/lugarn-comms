package broker

import (
	"context"

	"github.com/LexingtonStanley/lugarn-comms/internal/herdr"
	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

// Pane ids can be reused after a server restart. The team's stored workspace
// id, never a label, must still own the pane immediately before any close.
func (i *Integration) closeOwnedPane(ctx context.Context, server, workspace, pane string) error {
	client := i.opts.Herdr(server)
	p, e := client.Pane(ctx, pane)
	if herdr.IsGone(e) || herdr.IsUnavailable(e) {
		return nil
	}
	if e != nil {
		return e
	}
	if workspace == "" || p.Workspace != workspace {
		return model.NewError(model.ErrorLeaseConflict, "refusing to close a pane outside the recorded team workspace")
	}
	if p.Tab != "" {
		return client.CloseTab(ctx, p.Tab)
	}
	return client.ClosePane(ctx, pane)
}
