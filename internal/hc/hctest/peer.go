// Package hctest supplies a native fake line-protocol peer for portable tests.
// It never executes a terminal argv or invokes an installed core.
package hctest

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/hc"
)

type State struct {
	Hello      hc.Hello          `json:"hello"`
	Terminals  []hc.Terminal     `json:"terminals"`
	RunError   string            `json:"run_error"`
	SendError  string            `json:"send_error"`
	Events     []json.RawMessage `json:"events"`
	Disconnect bool              `json:"disconnect"`
	Calls      []Call            `json:"calls"`
}
type Call struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

func Read(path string) (State, error) {
	var s State
	raw, e := os.ReadFile(path)
	if e == nil {
		e = json.Unmarshal(raw, &s)
	}
	return s, e
}
func Write(path string, s State) error {
	raw, e := json.Marshal(s)
	if e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), "peer-*.json")
	if e != nil {
		return e
	}
	name := f.Name()
	defer os.Remove(name)
	if _, e = f.Write(raw); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	return os.Rename(name, path)
}
func Serve() int {
	path := os.Getenv("LUGARN_HC_FAKE_STATE")
	if len(os.Args) > 1 && os.Args[1] == "--help" {
		fmt.Println(os.Getenv("LUGARN_HC_FAKE_HELP"))
		return 0
	}
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println("herdr-core 0.1.21")
		return 0
	}
	if len(os.Args) != 4 || os.Args[1] != "connect" || os.Args[2] != "--session" || !strings.HasPrefix(os.Args[3], "lugarn-test-") {
		return 71
	}
	for _, v := range os.Environ() {
		if strings.HasPrefix(strings.ToUpper(v), "HC_") {
			return 72
		}
	}
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), 16<<20)
	for scanner.Scan() {
		var req struct {
			ID     uint64          `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(scanner.Bytes(), &req) != nil {
			return 73
		}
		state, e := Read(path)
		if e != nil {
			return 74
		}
		state.Calls = append(state.Calls, Call{req.Method, req.Params})
		var params struct {
			Terminal string            `json:"terminal"`
			Text     string            `json:"text"`
			Labels   map[string]string `json:"labels"`
		}
		_ = json.Unmarshal(req.Params, &params)
		var out any
		kind := ""
		index := -1
		for n, t := range state.Terminals {
			if t.Address == params.Terminal {
				index = n
			}
		}
		switch req.Method {
		case "hello":
			out = state.Hello
		case "terminal.list", "events.subscribe":
			selected := []hc.Terminal{}
			for _, t := range state.Terminals {
				match := true
				for k, v := range params.Labels {
					if t.Labels[k] != v {
						match = false
					}
				}
				if match {
					selected = append(selected, t)
				}
			}
			out = hc.Snapshot{Seq: 0, Terminals: selected}
		case "terminal.run":
			var options hc.RunOptions
			if json.Unmarshal(req.Params, &options) != nil {
				return 75
			}
			if state.RunError != "" {
				kind = state.RunError
				break
			}
			for _, t := range state.Terminals {
				if t.State == "live" && options.Name != nil && t.Name != nil && *options.Name == *t.Name {
					kind = "name_taken"
				}
			}
			if kind != "" {
				break
			}
			pid := int64(123)
			name := "pi"
			if len(options.Argv) > 0 {
				name = options.Argv[0]
			}
			t := hc.Terminal{Address: fmt.Sprintf("t%x", len(state.Terminals)+1), PID: &pid, Name: options.Name, Labels: options.Labels, State: "live", Cwd: options.Cwd, Argv: options.Argv, Foreground: &hc.Foreground{PID: pid, Name: name, Cwd: options.Cwd, Argv: options.Argv}, Status: &hc.Status{App: "pi", State: "idle"}, Reported: true}
			state.Terminals = append(state.Terminals, t)
			out = t
		case "terminal.get", "terminal.kill", "terminal.rm", "terminal.send", "terminal.read", "terminal.wait":
			if index < 0 {
				kind = "not_found"
				break
			}
			t := state.Terminals[index]
			switch req.Method {
			case "terminal.get":
				out = t
			case "terminal.kill":
				t.State = "finished"
				t.PID = nil
				t.Foreground = nil
				t.Status = nil
				state.Terminals[index] = t
				out = t
			case "terminal.rm":
				state.Terminals = append(state.Terminals[:index], state.Terminals[index+1:]...)
				out = map[string]string{"removed": t.Address}
			case "terminal.send":
				kind = state.SendError
				started := true
				state.Terminals[index].Status = &hc.Status{State: "working"}
				out = hc.SendResult{Started: &started}
			case "terminal.read":
				out = map[string]string{"text": "fake screen"}
			case "terminal.wait":
				status := "idle"
				out = hc.WaitResult{Terminal: t.Address, Ended: true, Reason: "status", Status: &status}
			}
		default:
			kind = "invalid_params"
		}
		if Write(path, state) != nil {
			return 76
		}
		reply := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		if kind != "" {
			reply["error"] = map[string]any{"code": -32000, "message": "fake failure", "data": map[string]string{"kind": kind}}
		} else {
			reply["result"] = out
		}
		if json.NewEncoder(os.Stdout).Encode(reply) != nil {
			return 77
		}
		if req.Method == "events.subscribe" {
			for _, event := range state.Events {
				fmt.Fprintln(os.Stdout, string(event))
			}
			if state.Disconnect {
				return 0
			}
			time.Sleep(time.Minute)
			return 0
		}
	}
	return 0
}
