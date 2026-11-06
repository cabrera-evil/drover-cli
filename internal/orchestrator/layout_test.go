package orchestrator_test

import (
	"slices"
	"testing"

	"github.com/cabrera-evil/drover-cli/internal/config"
	"github.com/cabrera-evil/drover-cli/internal/mux"
	"github.com/cabrera-evil/drover-cli/internal/orchestrator"
)

const (
	right = mux.Right
	down  = mux.Down
)

func op(pane int, dir mux.Direction) orchestrator.SplitOp {
	return orchestrator.SplitOp{Pane: pane, Dir: dir}
}

func leaf(group string) config.Split { return config.Split{Group: group} }

func node(dir string, children ...config.Split) config.Split {
	return config.Split{Dir: dir, Children: children}
}

// Layout (R24): op i creates pane i+1 by splitting pane op.Pane; pane 0 is the workspace root pane.
func TestPlanLayoutNamed(t *testing.T) {
	tests := []struct {
		layout string
		want   map[int][]orchestrator.SplitOp
	}{
		{"default", map[int][]orchestrator.SplitOp{
			1: {}, 2: {op(0, right)}, 3: {op(0, right), op(1, down)}, 4: {op(0, right), op(1, down), op(2, down)},
		}},
		{"", map[int][]orchestrator.SplitOp{2: {op(0, right)}, 3: {op(0, right), op(1, down)}}},
		{"columns", map[int][]orchestrator.SplitOp{
			1: {}, 2: {op(0, right)}, 3: {op(0, right), op(1, right)}, 4: {op(0, right), op(1, right), op(2, right)},
		}},
		{"rows", map[int][]orchestrator.SplitOp{
			1: {}, 2: {op(0, down)}, 3: {op(0, down), op(1, down)}, 4: {op(0, down), op(1, down), op(2, down)},
		}},
		{"main-vertical", map[int][]orchestrator.SplitOp{
			1: {}, 2: {op(0, right)}, 3: {op(0, right), op(1, down)}, 4: {op(0, right), op(1, down), op(2, down)},
		}},
	}
	for _, tc := range tests {
		for n, want := range tc.want {
			got := orchestrator.PlanLayout(config.Layout{Name: tc.layout}, n)
			if !slices.Equal(got, want) {
				t.Errorf("layout %q n=%d: got %v want %v", tc.layout, n, got, want)
			}
		}
	}
}

func TestPlanLayoutTree(t *testing.T) {
	tests := []struct {
		name string
		tree config.Split
		n    int
		want []orchestrator.SplitOp
	}{
		{"two columns", node("columns", leaf("a"), leaf("b")), 2, []orchestrator.SplitOp{op(0, right)}},
		{"two rows", node("rows", leaf("a"), leaf("b")), 2, []orchestrator.SplitOp{op(0, down)}},
		{"three columns", node("columns", leaf("a"), leaf("b"), leaf("c")), 3,
			[]orchestrator.SplitOp{op(0, right), op(1, right)}},
		{"columns with nested rows", node("columns", leaf("a"), node("rows", leaf("b"), leaf("c"))), 3,
			[]orchestrator.SplitOp{op(0, right), op(1, down)}},
		{"rows with nested columns", node("rows", leaf("a"), node("columns", leaf("b"), leaf("c"))), 3,
			[]orchestrator.SplitOp{op(0, down), op(1, right)}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := orchestrator.PlanLayout(config.Layout{Tree: &tc.tree}, tc.n)
			if !slices.Equal(got, tc.want) {
				t.Errorf("got %v want %v", got, tc.want)
			}
		})
	}
}

// A layout never plans a split for the first pane, so n panes need exactly n-1 operations.
func TestPlanLayoutSplitCount(t *testing.T) {
	for _, name := range []string{"default", "columns", "rows", "main-vertical"} {
		for n := 1; n <= 4; n++ {
			if got := len(orchestrator.PlanLayout(config.Layout{Name: name}, n)); got != n-1 {
				t.Errorf("layout %s n=%d: %d ops, want %d", name, n, got, n-1)
			}
		}
	}
}
