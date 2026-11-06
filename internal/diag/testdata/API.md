# internal/diag expected API (S4a)

Tests are in package `diag_test`.

```go
package diag

type Options struct {
	Verbose  bool                    // -v: log each call to Stderr
	Stderr   io.Writer
	StateDir string                  // debug.log lives here; created with MkdirAll when debug is on
	Getenv   func(string) string     // DROVER_LOG=debug enables <StateDir>/debug.log
	Now      func() time.Time        // clock; Exec calls it exactly twice per call (start, end)
}

func New(o Options) (*Logger, error)
func (l *Logger) Close() error // flushes/closes debug.log; safe to call twice

// Exec runs fn and logs `<base(bin)> <args> (<duration>)\n`, where args have RedactCommand applied,
// any arg containing whitespace is %q-quoted, and duration is end-start rounded to ms in
// time.Duration.String() form ("12ms"). fn's error is returned unchanged; a failed call is still logged.
// -v writes the line to Stderr; debug mode appends it (plus anything else useful) to debug.log.
func (l *Logger) Exec(bin string, args []string, fn func() error) error

// RedactCommand masks the value of each KEY=value token that is an env assignment
// (KEY matches [A-Za-z_][A-Za-z0-9_]*, token is not preceded by a non-assignment word, e.g.
// `go test -run=Foo` is untouched): `TOKEN=abc ./run` -> `TOKEN=*** ./run`.
// Single- and double-quoted values containing spaces are masked whole.
func RedactCommand(cmd string) string
```
