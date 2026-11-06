// Package mux abstracts the terminal multiplexer drover drives; herdr is the only adapter.
package mux

import "context"

// Workspace is a live multiplexer workspace.
type Workspace struct {
	ID        string
	Label     string
	PaneCount int
}

// Created is the result of creating a workspace.
type Created struct{ WorkspaceID, RootPaneID string }

// Direction is the side a pane is split towards.
type Direction string

// Split directions.
const (
	Right Direction = "right"
	Down  Direction = "down"
)

// Multiplexer is everything drover needs from a terminal multiplexer.
type Multiplexer interface {
	ListWorkspaces(ctx context.Context) ([]Workspace, error)
	CreateWorkspace(ctx context.Context, cwd, label string) (Created, error)
	SplitPane(ctx context.Context, paneID string, dir Direction, cwd string) (string, error)
	RenamePane(ctx context.Context, paneID, label string) error
	RunInPane(ctx context.Context, paneID, cmd string) error
	ReadPane(ctx context.Context, paneID string) (string, error)
	FocusWorkspace(ctx context.Context, id string) error
	CloseWorkspace(ctx context.Context, id string) error
}
