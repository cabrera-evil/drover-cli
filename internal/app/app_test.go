//nolint:testpackage // exercises unexported wiring
package app

import (
	"bytes"
	"strings"
	"testing"
)

func TestCompletionZshHasCompdef(t *testing.T) {
	var out bytes.Buffer
	root := NewRootCmd("v1.0.0")
	root.SetOut(&out)
	root.SetArgs([]string{"completion", "zsh"})
	if err := root.Execute(); err != nil || !strings.Contains(out.String(), "compdef") {
		t.Errorf("err=%v", err)
	}
}
