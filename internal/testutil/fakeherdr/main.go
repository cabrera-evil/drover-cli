// Command fakeherdr imitates herdr 0.9.x for the subcommands drover uses. It is test support only.
//
// It keeps workspaces and panes in the JSON file named by FAKE_HERDR_STATE and prints herdr's JSON shapes.
// FAKE_HERDR_FAIL=<args prefix> makes matching calls exit 1 with stderr; FAKE_HERDR_SLEEP_MS delays every call.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	exitFail  = 1
	exitUsage = 2
	fileMode  = 0o600
	version   = "herdr 0.9.0"

	minArgs   = 2
	paneArg   = 2
	cmdArg    = 3
	dirRight  = "right"
	dirDown   = "down"
	flagCwd   = "--cwd"
	flagLabel = "--label"
	keyType   = "type"
)

var errNotFound = errors.New("not found")

type pane struct {
	ID    string   `json:"id"`
	Label string   `json:"label"`
	Cwd   string   `json:"cwd"`
	Ran   []string `json:"ran"`
}

type workspace struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Cwd     string `json:"cwd"`
	Focused bool   `json:"focused"`
	Panes   []pane `json:"panes"`
}

type world struct {
	NextWorkspace int         `json:"next_workspace"`
	Workspaces    []workspace `json:"workspaces"`
	Calls         [][]string  `json:"calls"`
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 1 && args[0] == "--version" {
		fmt.Fprintln(stdout, version)

		return 0
	}
	path := os.Getenv("FAKE_HERDR_STATE")
	if path == "" || len(args) < minArgs {
		fmt.Fprintln(stderr, "fake herdr: FAKE_HERDR_STATE and a subcommand are required")

		return exitUsage
	}
	if ms, err := strconv.Atoi(os.Getenv("FAKE_HERDR_SLEEP_MS")); err == nil {
		time.Sleep(time.Duration(ms) * time.Millisecond)
	}
	if fail := os.Getenv("FAKE_HERDR_FAIL"); fail != "" && strings.HasPrefix(strings.Join(args, " "), fail) {
		fmt.Fprintln(stderr, "fake herdr: simulated failure")

		return exitFail
	}
	w, err := load(path)
	if err == nil {
		w.Calls = append(w.Calls, args)

		var result any
		if result, err = dispatch(w, args); err == nil {
			if err = save(path, w); err == nil {
				err = printResult(stdout, result)
			}
		}
	}
	if err != nil {
		fmt.Fprintln(stderr, "fake herdr:", err)

		return exitFail
	}

	return 0
}

func printResult(out io.Writer, result any) error {
	if text, ok := result.(string); ok {
		_, err := fmt.Fprintln(out, text)

		return err
	}
	b, err := json.Marshal(map[string]any{"id": "fake", "result": result})
	if err != nil {
		return fmt.Errorf("encode result: %w", err)
	}
	_, err = fmt.Fprintln(out, string(b))

	return err
}

func load(path string) (*world, error) {
	w := &world{NextWorkspace: 1}
	b, err := os.ReadFile(path) //nolint:gosec // test support reads the path the test chose.
	if errors.Is(err, os.ErrNotExist) {
		return w, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read state: %w", err)
	}
	if err = json.Unmarshal(b, w); err != nil {
		return nil, fmt.Errorf("parse state: %w", err)
	}

	return w, nil
}

func save(path string, w *world) error {
	b, err := json.Marshal(w)
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	//nolint:gosec // test support writes the path the test chose.
	if err = os.WriteFile(path, b, fileMode); err != nil {
		return fmt.Errorf("write state: %w", err)
	}

	return nil
}

func dispatch(w *world, args []string) (any, error) {
	switch args[0] + " " + args[1] {
	case "workspace list":
		return listWorkspaces(w), nil
	case "workspace create":
		return createWorkspace(w, args), nil
	case "workspace focus":
		return focusWorkspace(w, args)
	case "workspace close":
		return closeWorkspace(w, args)
	case "pane split":
		return splitPane(w, args)
	case "pane rename":
		return renamePane(w, args)
	case "pane run":
		return runInPane(w, args)
	case "pane read":
		return readPane(w, args)
	}

	return nil, fmt.Errorf("unknown subcommand %q", args[0]+" "+args[1])
}

func flagValue(args []string, name string) string {
	for i := range len(args) - 1 {
		if args[i] == name {
			return args[i+1]
		}
	}

	return ""
}

func okResult() map[string]any { return map[string]any{keyType: "ok"} }

func listWorkspaces(w *world) any {
	list := make([]map[string]any, 0, len(w.Workspaces))
	for _, ws := range w.Workspaces {
		list = append(list, map[string]any{
			"workspace_id": ws.ID, "label": ws.Label, "pane_count": len(ws.Panes), "focused": ws.Focused,
		})
	}

	return map[string]any{keyType: "workspace_list", "workspaces": list}
}

func createWorkspace(w *world, args []string) any {
	ws := workspace{
		ID:    "w" + strconv.Itoa(w.NextWorkspace),
		Label: flagValue(args, flagLabel),
		Cwd:   flagValue(args, flagCwd),
	}
	w.NextWorkspace++
	root := pane{ID: ws.ID + ":p1", Cwd: ws.Cwd}
	ws.Panes = []pane{root}
	w.Workspaces = append(w.Workspaces, ws)

	return map[string]any{
		keyType:     "workspace_created",
		"workspace": map[string]any{"workspace_id": ws.ID, "label": ws.Label},
		"root_pane": map[string]any{"pane_id": root.ID},
	}
}

func findWorkspace(w *world, id string) (*workspace, error) {
	for i := range w.Workspaces {
		if w.Workspaces[i].ID == id {
			return &w.Workspaces[i], nil
		}
	}

	return nil, fmt.Errorf("workspace %q: %w", id, errNotFound)
}

func findPane(w *world, id string) (*pane, error) {
	for i := range w.Workspaces {
		for j := range w.Workspaces[i].Panes {
			if w.Workspaces[i].Panes[j].ID == id {
				return &w.Workspaces[i].Panes[j], nil
			}
		}
	}

	return nil, fmt.Errorf("pane %q: %w", id, errNotFound)
}

func argAt(args []string, i int) (string, error) {
	if i >= len(args) {
		return "", fmt.Errorf("missing argument %d", i)
	}

	return args[i], nil
}

func focusWorkspace(w *world, args []string) (any, error) {
	id, err := argAt(args, paneArg)
	if err != nil {
		return nil, err
	}
	target, err := findWorkspace(w, id)
	if err != nil {
		return nil, err
	}
	for i := range w.Workspaces {
		w.Workspaces[i].Focused = false
	}
	target.Focused = true

	return okResult(), nil
}

func closeWorkspace(w *world, args []string) (any, error) {
	id, err := argAt(args, paneArg)
	if err != nil {
		return nil, err
	}
	if _, err = findWorkspace(w, id); err != nil {
		return nil, err
	}
	kept := w.Workspaces[:0]
	for _, ws := range w.Workspaces {
		if ws.ID != id {
			kept = append(kept, ws)
		}
	}
	w.Workspaces = kept

	return okResult(), nil
}

func splitPane(w *world, args []string) (any, error) {
	src, err := findPane(w, flagValue(args, "--pane"))
	if err != nil {
		return nil, err
	}
	if dir := flagValue(args, "--direction"); dir != dirRight && dir != dirDown {
		return nil, fmt.Errorf("invalid direction %q", dir)
	}
	wsID, _, _ := strings.Cut(src.ID, ":")
	ws, err := findWorkspace(w, wsID)
	if err != nil {
		return nil, err
	}
	created := pane{ID: wsID + ":p" + strconv.Itoa(len(ws.Panes)+1), Cwd: flagValue(args, flagCwd)}
	ws.Panes = append(ws.Panes, created)

	return map[string]any{keyType: "pane_split", "pane": map[string]any{"pane_id": created.ID}}, nil
}

func renamePane(w *world, args []string) (any, error) {
	id, err := argAt(args, paneArg)
	if err != nil {
		return nil, err
	}
	label, err := argAt(args, cmdArg)
	if err != nil {
		return nil, err
	}
	p, err := findPane(w, id)
	if err != nil {
		return nil, err
	}
	p.Label = label

	return okResult(), nil
}

func runInPane(w *world, args []string) (any, error) {
	id, err := argAt(args, paneArg)
	if err != nil {
		return nil, err
	}
	command, err := argAt(args, cmdArg)
	if err != nil {
		return nil, err
	}
	p, err := findPane(w, id)
	if err != nil {
		return nil, err
	}
	p.Ran = append(p.Ran, command)

	return okResult(), nil
}

// readPane prints the commands run in the pane as plain text, one per line.
func readPane(w *world, args []string) (any, error) {
	id, err := argAt(args, paneArg)
	if err != nil {
		return nil, err
	}
	p, err := findPane(w, id)
	if err != nil {
		return nil, err
	}

	return strings.Join(p.Ran, "\n"), nil
}
