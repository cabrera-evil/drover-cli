# Expected production API for slice S3 `internal/ansi` (derived from the tests)

```go
// Strip returns s without ANSI escape sequences: CSI (ESC [ ... final byte), OSC (ESC ] ... BEL or ESC \),
// and other two-byte ESC sequences. A partial/unterminated sequence at the end of s is dropped.
// Text, newlines and carriage returns are kept. Idempotent.
func Strip(s string) string
```
