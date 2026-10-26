package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/LexingtonStanley/lugarn-comms/internal/broker"
	"github.com/LexingtonStanley/lugarn-comms/internal/hostprobe"
	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/statedir"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
)

func integrationOptions() (broker.IntegrationOptions, error) {
	binary, e := os.Executable()
	if e != nil {
		return broker.IntegrationOptions{}, e
	}
	home, e := os.UserHomeDir()
	if e != nil {
		return broker.IntegrationOptions{}, e
	}
	if override := os.Getenv("LUGARN_COMMS_POINTER_HOME"); override != "" {
		home, e = filepath.Abs(override)
		if e != nil {
			return broker.IntegrationOptions{}, e
		}
	}
	models := map[model.Harness]string{}
	for _, h := range (model.Harness("")).Values() {
		if value := os.Getenv("LUGARN_COMMS_MODEL_" + strings.ToUpper(string(h))); value != "" {
			models[h] = value
		}
	}
	return broker.IntegrationOptions{Binary: binary, HerdrBinary: os.Getenv("LUGARN_COMMS_HERDR_BINARY"), HomeDir: home, Models: models}, nil
}
func runTeam(ctx context.Context, args []string, stderr io.Writer) int {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	flags.SetOutput(stderr)
	team := flags.String("team", "", "team id")
	if e := flags.Parse(args); e != nil {
		if e == flag.ErrHelp {
			return 0
		}
		return 2
	}
	fail := func(e error) int { err := model.AsError(e); fmt.Fprintln(stderr, err); return model.ExitCode(err.Code) }
	if *team == "" || flags.NArg() != 0 {
		return fail(model.NewError(model.ErrorInvalidRequest, "run requires --team <id>"))
	}
	if e := model.ValidateOpaqueID(*team); e != nil {
		return fail(e)
	}
	dir, e := statedir.Resolve()
	if e != nil {
		return fail(e)
	}
	dir, e = filepath.Abs(dir)
	if e != nil {
		return fail(e)
	}
	if e = statedir.Ensure(dir); e != nil {
		return fail(e)
	}
	db, e := store.Open(ctx, filepath.Join(dir, "comms.db"))
	if e != nil {
		return fail(e)
	}
	defer db.Close()
	pathInfo, e := hostprobe.LoginPath(ctx, dir, false)
	if e != nil {
		return fail(e)
	}
	_ = os.Setenv("PATH", pathInfo.Merged())
	b := broker.New(db, broker.Options{StateDir: dir, LoginPath: &pathInfo})
	opts, e := integrationOptions()
	if e != nil {
		return fail(e)
	}
	integration, e := broker.NewIntegration(b, opts)
	if e != nil {
		return fail(e)
	}
	if e = broker.EnableSubreaper(); e != nil {
		return fail(e)
	}
	var pane *string
	if value := os.Getenv("HERDR_PANE_ID"); value != "" {
		pane = &value
	}
	if e = integration.Runner(*team, broker.RunnerOptions{Pane: pane}).Run(ctx); e != nil {
		return fail(e)
	}
	return 0
}
