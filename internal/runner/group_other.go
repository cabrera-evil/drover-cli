//go:build !unix

package runner

import "os/exec"

// killGroupOnCancel is a no-op: the default Cancel kills only the child on this OS.
func killGroupOnCancel(*exec.Cmd) {}
