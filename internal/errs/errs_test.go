package errs_test

import (
	"errors"
	"fmt"
	"regexp"
	"testing"

	"github.com/cabrera-evil/drover-cli/internal/errs"
)

// AC12 (R14): golden rendering `file:line key.path: reason (DRV-...)`.
func TestErrorRendering(t *testing.T) {
	tests := []struct {
		name string
		err  *errs.Error
		want string
	}{
		{
			name: "file line key",
			err: &errs.Error{
				Code: "DRV-CONFIG-INVALID-VALUE", File: "drover.yaml", Line: 12,
				Key: "processes.api.ready", Message: "only one ready checker is allowed",
			},
			want: "drover.yaml:12 processes.api.ready: only one ready checker is allowed (DRV-CONFIG-INVALID-VALUE)",
		},
		{
			name: "no line",
			err: &errs.Error{
				Code:    "DRV-CONFIG-UNSUPPORTED-VERSION",
				File:    "/x/drover.yaml",
				Key:     "version",
				Message: "unsupported version 2",
			},
			want: "/x/drover.yaml version: unsupported version 2 (DRV-CONFIG-UNSUPPORTED-VERSION)",
		},
		{
			name: "no file or key",
			err:  &errs.Error{Code: "DRV-SOMETHING-FAILED", Message: "something failed"},
			want: "something failed (DRV-SOMETHING-FAILED)",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.err.Error(); got != tc.want {
				t.Errorf("got %q want %q", got, tc.want)
			}
		})
	}
}

// R73: exit code 3 for invalid config; ExitCode survives wrapping.
func TestExitCode(t *testing.T) {
	cfg := &errs.Error{Code: "DRV-CONFIG-UNKNOWN-KEY", Message: "bad", Exit: errs.ExitConfig}
	if errs.ExitConfig != 3 {
		t.Fatalf("ExitConfig = %d, want 3", errs.ExitConfig)
	}
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, 0},
		{"plain error", errors.New("boom"), 1},
		{"config error", cfg, 3},
		{"wrapped config error", fmt.Errorf("ctx: %w", cfg), 3},
		{"drv error without exit", &errs.Error{Code: "DRV-TEST-CODE", Message: "x"}, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := errs.ExitCode(tc.err); got != tc.want {
				t.Errorf("got %d want %d", got, tc.want)
			}
		})
	}
}

// R73: stable DRV- slug code shape (DRV-AREA-NAME).
func TestCodeShape(t *testing.T) {
	re := regexp.MustCompile(`^DRV(-[A-Z]+)+$`)
	for _, c := range []string{
		errs.CodeUnknownKey, errs.CodeUnsupportedVersion, errs.CodeInvalidValue, errs.CodeNotFound,
	} {
		if !re.MatchString(c) {
			t.Errorf("code %q does not match %s", c, re)
		}
	}
	seen := map[string]bool{}
	for _, c := range []string{
		errs.CodeUnknownKey, errs.CodeUnsupportedVersion, errs.CodeInvalidValue, errs.CodeNotFound,
	} {
		if seen[c] {
			t.Errorf("duplicate code %s", c)
		}
		seen[c] = true
	}
}
