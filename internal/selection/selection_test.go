package selection_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/cabrera-evil/drover-cli/internal/config"
	"github.com/cabrera-evil/drover-cli/internal/errs"
	"github.com/cabrera-evil/drover-cli/internal/selection"
)

func opt(preset string, groups ...string) selection.Options {
	return selection.Options{Groups: groups, Preset: preset}
}

func stack() *config.Config {
	return &config.Config{
		Order: []string{"web", "api", "admin", "worker", "db"},
		Processes: map[string]config.Process{
			"web":    {Tags: []string{"fe"}},
			"api":    {Tags: []string{"be", "priv"}},
			"admin":  {Tags: []string{"be", "priv", "admin"}},
			"worker": {Tags: []string{"be"}},
			"db":     {Tags: []string{"infra"}},
		},
		Groups: map[string]config.Group{
			"sv":       {TagsAny: []string{"be"}},
			"strict":   {TagsAll: []string{"be", "priv"}},
			"explicit": {Processes: []string{"db", "web"}},
			"fe":       {TagsAny: []string{"fe"}},
		},
		Presets: map[string]config.Preset{
			"all":        {TagsAny: []string{"*"}},
			"priv-admin": {TagsAny: []string{"admin", "fe"}},
			"both":       {TagsAll: []string{"priv", "admin"}},
		},
	}
}

// AC15, AC16 (R20): tags_all, tags_any, explicit list, `*` preset, and group x preset intersection.
func TestSelect(t *testing.T) {
	tests := []struct {
		name string
		opts selection.Options
		want []string
	}{
		{"AC15 tags_any", selection.Options{Groups: []string{"sv"}}, []string{"api", "admin", "worker"}},
		{"AC15 tags_all", selection.Options{Groups: []string{"strict"}}, []string{"api", "admin"}},
		{"AC15 explicit list", selection.Options{Groups: []string{"explicit"}}, []string{"web", "db"}},
		{"AC15 star preset", opt("all", "sv"), []string{"api", "admin", "worker"}},
		{"AC15 all groups", selection.Options{}, []string{"web", "api", "admin", "worker", "db"}},
		{"AC15 groups union", opt("", "fe", "sv"), []string{"web", "api", "admin", "worker"}},
		{"AC16 group x preset intersection", opt("priv-admin", "sv"), []string{"admin"}},
		{"AC16 preset tags_all", opt("both", "strict"), []string{"admin"}},
		{"AC16 empty intersection", opt("both", "explicit"), []string{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := selection.Select(stack(), tc.opts)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, tc.want) && (len(got) != 0 || len(tc.want) != 0) {
				t.Errorf("got %v want %v", got, tc.want)
			}
		})
	}
}

// R20: unknown group or preset => usage error naming the valid options.
func TestSelectUnknown(t *testing.T) {
	tests := []struct {
		name string
		opts selection.Options
		bad  string
		ok   []string
	}{
		{"group", selection.Options{Groups: []string{"nope"}}, "nope", []string{"sv", "strict", "explicit", "fe"}},
		{"preset", selection.Options{Preset: "nope"}, "nope", []string{"all", "priv-admin", "both"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := selection.Select(stack(), tc.opts)
			var e *errs.Error
			if !errors.As(err, &e) {
				t.Fatalf("want *errs.Error, got %v", err)
			}
			if e.Code != errs.CodeUsage || errs.ExitCode(err) != errs.ExitUsage {
				t.Errorf("code %s exit %d", e.Code, errs.ExitCode(err))
			}
			for _, s := range append([]string{tc.bad}, tc.ok...) {
				if !strings.Contains(err.Error(), s) {
					t.Errorf("error %q must mention %q", err, s)
				}
			}
		})
	}
}
