package probe_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cabrera-evil/drover-cli/internal/config"
	"github.com/cabrera-evil/drover-cli/internal/errs"
	"github.com/cabrera-evil/drover-cli/internal/probe"
)

const kindHTTP = "http"

// fakeClock advances instantly: After moves Now forward by d and fires at once, so Wait runs in no real time.
type fakeClock struct{ now time.Time }

func newClock() *fakeClock { return &fakeClock{now: time.Unix(1_700_000_000, 0)} }

func (c *fakeClock) Now() time.Time { return c.now }

func (c *fakeClock) After(d time.Duration) <-chan time.Time {
	c.now = c.now.Add(d)
	ch := make(chan time.Time, 1)
	ch <- c.now

	return ch
}

func mustChecker(t *testing.T, r config.Ready, port int, d probe.Deps) probe.Checker { //nolint:ireturn // test helper
	t.Helper()
	c, err := probe.NewChecker(r, port, d)
	if err != nil {
		t.Fatal(err)
	}

	return c
}

func portOf(t *testing.T, addr string) int {
	t.Helper()
	_, p, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if _, scanErr := fmt.Sscanf(p, "%d", &n); scanErr != nil {
		t.Fatal(scanErr)
	}

	return n
}

func status(code int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(code) }))
}

// AC31 (R42): http checker, default accepts 2xx; a mismatching status is not ready.
func TestHTTPChecker(t *testing.T) {
	ok, bad := status(http.StatusOK), status(http.StatusServiceUnavailable)
	t.Cleanup(ok.Close)
	t.Cleanup(bad.Close)
	ctx := context.Background()
	okChecker := mustChecker(t, config.Ready{Kind: kindHTTP, Value: ok.URL}, 0, probe.Deps{})
	if up, err := okChecker.Check(ctx); err != nil || !up {
		t.Errorf("200 => %v, %v; want ready", up, err)
	}
	if up, _ := mustChecker(t, config.Ready{Kind: kindHTTP, Value: bad.URL}, 0, probe.Deps{}).Check(ctx); up {
		t.Error("503 must not be ready")
	}
	// explicit expected status
	nf := status(http.StatusNotFound)
	t.Cleanup(nf.Close)
	d := probe.Deps{Status: http.StatusNotFound}
	if up, _ := mustChecker(t, config.Ready{Kind: kindHTTP, Value: nf.URL}, 0, d).Check(ctx); !up {
		t.Error("expected status 404 matched but not ready")
	}
	d.Status = http.StatusOK
	if up, _ := mustChecker(t, config.Ready{Kind: kindHTTP, Value: nf.URL}, 0, d).Check(ctx); up {
		t.Error("expected 200, got 404, must not be ready")
	}
}

// AC31 (R42): a value that is only a path is combined with the process port on 127.0.0.1.
func TestHTTPCheckerPathAndPort(t *testing.T) {
	var gotPath atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath.Store(r.URL.Path)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	port := portOf(t, srv.Listener.Addr().String())
	c := mustChecker(t, config.Ready{Kind: kindHTTP, Value: "/health"}, port, probe.Deps{})
	if up, err := c.Check(context.Background()); err != nil || !up {
		t.Fatalf("got %v, %v; want ready", up, err)
	}
	if gotPath.Load() != "/health" {
		t.Errorf("requested path %v", gotPath.Load())
	}
}

// AC30 (R41): a stalled server never hangs the checker; it returns not-ready within the context timeout.
func TestHTTPCheckerStalled(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) })
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	start := time.Now()
	up, _ := mustChecker(t, config.Ready{Kind: kindHTTP, Value: srv.URL}, 0, probe.Deps{}).Check(ctx)
	if up {
		t.Error("stalled server must not be ready")
	}
	if el := time.Since(start); el > 2*time.Second {
		t.Errorf("Check took %v, probes must not hang", el)
	}
}

// AC31, AC29 (R40): tcp checker consults the socket table and never dials the port.
func TestTCPCheckerUsesTableNotDial(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	var conns atomic.Int32
	go func() {
		for {
			c, acceptErr := l.Accept()
			if acceptErr != nil {
				return
			}
			conns.Add(1)
			_ = c.Close()
		}
	}()
	port := portOf(t, l.Addr().String())
	tbl := &fakeTable{ports: map[int]bool{port: true}}
	val := fmt.Sprintf("localhost:%d", port)
	chk := mustChecker(t, config.Ready{Kind: "tcp", Value: val}, 0, probe.Deps{Table: tbl})
	up, err := chk.Check(context.Background())
	if err != nil || !up {
		t.Fatalf("listed port => %v, %v; want ready", up, err)
	}
	if len(tbl.asked) != 1 || tbl.asked[0] != port {
		t.Errorf("table asked %v, want [%d]", tbl.asked, port)
	}
	time.Sleep(100 * time.Millisecond)
	if n := conns.Load(); n != 0 {
		t.Fatalf("checker connected to the port %d time(s); probes must read the socket table", n)
	}
}

// AC31: tcp checker not ready when the table does not list the port.
func TestTCPCheckerDown(t *testing.T) {
	tbl := &fakeTable{ports: map[int]bool{}}
	chk := mustChecker(t, config.Ready{Kind: "tcp", Value: "localhost:5432"}, 0, probe.Deps{Table: tbl})
	up, err := chk.Check(context.Background())
	if err != nil || up {
		t.Errorf("got %v, %v; want not ready without error", up, err)
	}
}

// AC31 (R42): log checker matches a regex over an injected line stream and stays ready once matched.
func TestLogChecker(t *testing.T) {
	lines := make(chan string, 8)
	c := mustChecker(t, config.Ready{Kind: "log", Value: `listening on :\d+`}, 0, probe.Deps{Lines: lines})
	ctx := context.Background()
	lines <- "booting"
	lines <- "migrating"
	if up, _ := c.Check(ctx); up {
		t.Error("no match yet, must not be ready")
	}
	lines <- "server listening on :3000"
	if up, err := c.Check(ctx); err != nil || !up {
		t.Errorf("match => %v, %v; want ready", up, err)
	}
	if up, _ := c.Check(ctx); !up {
		t.Error("readiness must be sticky after a match")
	}
}

// AC31: an invalid log regex is rejected when the checker is built.
func TestLogCheckerBadRegex(t *testing.T) {
	_, err := probe.NewChecker(config.Ready{Kind: "log", Value: "("}, 0, probe.Deps{Lines: make(chan string)})
	if err == nil {
		t.Error("expected an error for an invalid regex")
	}
}

// AC31 (R42): delay checker is ready once the clock has passed the delay since the checker was built.
func TestDelayChecker(t *testing.T) {
	clk := newClock()
	c := mustChecker(t, config.Ready{Kind: "delay", Value: "5s"}, 0, probe.Deps{Clock: clk})
	ctx := context.Background()
	if up, _ := c.Check(ctx); up {
		t.Error("ready at t=0")
	}
	clk.now = clk.now.Add(4900 * time.Millisecond)
	if up, _ := c.Check(ctx); up {
		t.Error("ready before the delay elapsed")
	}
	clk.now = clk.now.Add(100 * time.Millisecond)
	if up, err := c.Check(ctx); err != nil || !up {
		t.Errorf("delay elapsed => %v, %v; want ready", up, err)
	}
}

// AC31 (R42): none is immediately ready.
func TestNoneChecker(t *testing.T) {
	chk := mustChecker(t, config.Ready{Kind: "none"}, 0, probe.Deps{})
	if up, err := chk.Check(context.Background()); err != nil || !up {
		t.Errorf("none => %v, %v; want ready", up, err)
	}
}

func TestNewCheckerUnknownKind(t *testing.T) {
	if _, err := probe.NewChecker(config.Ready{Kind: "carrier-pigeon"}, 0, probe.Deps{}); err == nil {
		t.Error("expected an error for an unknown ready kind")
	}
}

type funcChecker func(context.Context) (bool, error)

func (f funcChecker) Check(ctx context.Context) (bool, error) { return f(ctx) }

// AC23 (R28): Wait polls on the clock and reports ready with the elapsed time.
func TestWaitReadyAfterDelay(t *testing.T) {
	clk := newClock()
	var calls int
	chk := funcChecker(func(context.Context) (bool, error) { calls++; return calls >= 4, nil })
	res := probe.Wait(context.Background(), chk, 30*time.Second, time.Second, clk)
	if !res.Ready {
		t.Fatalf("expected ready, got %+v", res)
	}
	if calls != 4 {
		t.Errorf("checker called %d times, want 4", calls)
	}
	if res.Elapsed != 3*time.Second {
		t.Errorf("Elapsed = %v, want 3s", res.Elapsed)
	}
}

// AC24 (R29): readiness never met => not ready after the timeout, no hang.
func TestWaitTimeout(t *testing.T) {
	clk := newClock()
	chk := funcChecker(func(context.Context) (bool, error) { return false, nil })
	res := probe.Wait(context.Background(), chk, 10*time.Second, 2*time.Second, clk)
	if res.Ready {
		t.Fatal("must not be ready")
	}
	if res.Elapsed < 10*time.Second || res.Elapsed > 12*time.Second {
		t.Errorf("Elapsed = %v, want about the 10s timeout", res.Elapsed)
	}
}

// AC30 (R41): Wait bounds each check by the remaining timeout, so a checker that blocks cannot hang it.
func TestWaitBoundsBlockingChecker(t *testing.T) {
	chk := funcChecker(func(ctx context.Context) (bool, error) { <-ctx.Done(); return false, ctx.Err() })
	start := time.Now()
	res := probe.Wait(context.Background(), chk, 100*time.Millisecond, 10*time.Millisecond, probe.RealClock{})
	if res.Ready {
		t.Error("must not be ready")
	}
	if el := time.Since(start); el > 2*time.Second {
		t.Errorf("Wait took %v", el)
	}
}

// AC24 (R29): the timeout error carries a drover logs hint, exit code 5 and a slug code.
func TestNotReadyError(t *testing.T) {
	err := probe.NotReadyError("api", 30*time.Second)
	if got := errs.ExitCode(err); got != 5 {
		t.Errorf("exit code = %d, want 5", got)
	}
	if err.Code != "DRV-READY-TIMEOUT" {
		t.Errorf("code = %q", err.Code)
	}
	text := err.Error() + " " + err.Fix
	if !strings.Contains(text, "drover logs api") || !strings.Contains(text, "api") {
		t.Errorf("missing hint in %q", text)
	}
}
