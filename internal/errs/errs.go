// Package errs defines the typed errors drover returns to the user, with stable DRV- codes and exit codes.
package errs

import (
	"errors"
	"fmt"
	"strconv"
)

// Process exit codes (R71). Success is 0 and runtime failures are 1.
const (
	ExitUsage     = 2
	ExitConfig    = 3
	ExitPreflight = 4
	ExitReadiness = 5
)

// Stable error codes (R73), slug style DRV-AREA-NAME. Codes are never reused.
const (
	CodeUnknownKey         = "DRV-CONFIG-UNKNOWN-KEY"
	CodeUnsupportedVersion = "DRV-CONFIG-UNSUPPORTED-VERSION"
	CodeInvalidValue       = "DRV-CONFIG-INVALID-VALUE"
	CodeNotFound           = "DRV-CONFIG-NOT-FOUND"
	CodeUsage              = "DRV-USAGE"
	CodePortInUse          = "DRV-PORT-IN-USE"
	CodeCwdMissing         = "DRV-CWD-MISSING"
	CodeEnvFileMissing     = "DRV-ENVFILE-MISSING"
	CodeDepCycle           = "DRV-DEP-CYCLE"
	CodeDepUnknown         = "DRV-DEP-UNKNOWN"
	CodeReadyTimeout       = "DRV-READY-TIMEOUT"
	CodeHerdrFailed        = "DRV-HERDR-FAILED"
	CodeHerdrNotFound      = "DRV-HERDR-NOT-FOUND"
	CodeEnvFileInvalid     = "DRV-ENVFILE-INVALID"
	CodeRunFailed          = "DRV-RUN-FAILED"
)

// Error is a user-facing error. It must never carry env or env_file values.
type Error struct {
	Code    string
	Message string
	File    string
	Line    int
	Key     string
	Fix     string
	Exit    int // 0 means a runtime failure (exit code 1).
}

// Error renders `file:line key.path: message (DRV-AREA-NAME)`, omitting the parts that are unset.
func (e *Error) Error() string {
	loc := e.File
	if e.File != "" && e.Line > 0 {
		loc += ":" + strconv.Itoa(e.Line)
	}
	if e.Key != "" {
		if loc != "" {
			loc += " "
		}
		loc += e.Key
	}
	if loc == "" {
		return fmt.Sprintf("%s (%s)", e.Message, e.Code)
	}

	return fmt.Sprintf("%s: %s (%s)", loc, e.Message, e.Code)
}

// ExitCode returns the process exit code for this error.
func (e *Error) ExitCode() int {
	if e.Exit == 0 {
		return 1
	}

	return e.Exit
}

// ExitCode maps any error to a process exit code: 0 for nil, the typed code for *Error, 1 otherwise.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var e *Error
	if errors.As(err, &e) {
		return e.ExitCode()
	}

	return 1
}
