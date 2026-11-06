package mux_test

import (
	"context"
	"testing"

	"github.com/cabrera-evil/drover-cli/internal/mux"
)

// fakeMux is the tiny in-repo fake multiplexer other packages' tests can copy. It records nothing and returns zeros.
type fakeMux struct{ workspaces []mux.Workspace }

func (f *fakeMux) ListWorkspaces(context.Context) ([]mux.Workspace, error) { return f.workspaces, nil }

func (*fakeMux) CreateWorkspace(context.Context, string, string) (mux.Created, error) {
	return mux.Created{WorkspaceID: "w1", RootPaneID: "w1:p1"}, nil
}

func (*fakeMux) SplitPane(context.Context, string, mux.Direction, string) (string, error) {
	return "w1:p2", nil
}

func (*fakeMux) RenamePane(context.Context, string, string) error { return nil }

func (*fakeMux) RunInPane(context.Context, string, string) error { return nil }

func (*fakeMux) ReadPane(context.Context, string) (string, error) { return "", nil }

func (*fakeMux) FocusWorkspace(context.Context, string) error { return nil }

func (*fakeMux) CloseWorkspace(context.Context, string) error { return nil }

// AC40 (R43): the interface is small, the fake satisfies it, and so does the herdr adapter.
var (
	_ mux.Multiplexer = (*fakeMux)(nil)
	_ mux.Multiplexer = (*mux.Herdr)(nil)
)

// AC40: code written against mux.Multiplexer runs with only the fake.
func TestFakeMuxDrivesInterface(t *testing.T) {
	var m mux.Multiplexer = &fakeMux{workspaces: []mux.Workspace{{ID: "w1", Label: "api", PaneCount: 1}}}
	got, err := m.ListWorkspaces(context.Background())
	if err != nil || len(got) != 1 || got[0].Label != "api" {
		t.Fatalf("ListWorkspaces = %v, %v", got, err)
	}
}
