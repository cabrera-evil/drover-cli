package probe

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/cabrera-evil/drover-cli/internal/config"
	"github.com/cabrera-evil/drover-cli/internal/errs"
)

const (
	kindHTTP  = "http"
	kindTCP   = "tcp"
	kindLog   = "log"
	kindDelay = "delay"
	kindNone  = "none"

	httpClientTimeout = 5 * time.Second
	loopbackHost      = "127.0.0.1"
)

// Checker reports whether a process is ready.
type Checker interface {
	Check(ctx context.Context) (bool, error)
}

// Clock abstracts time so waits can be tested without sleeping.
type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

// RealClock is the Clock backed by the time package.
type RealClock struct{}

// Now returns the current time.
func (RealClock) Now() time.Time { return time.Now() }

// After waits for d.
func (RealClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// Deps are the collaborators a Checker may need.
type Deps struct {
	Table  PortTable     // tcp
	Lines  <-chan string // log: drained non-blockingly on each Check
	Clock  Clock         // delay; RealClock when nil
	Status int           // http expected status; 0 means any 2xx
	Client *http.Client  // optional; the default has a bounded timeout
}

// funcChecker adapts a function to Checker.
type funcChecker func(ctx context.Context) (bool, error)

func (f funcChecker) Check(ctx context.Context) (bool, error) { return f(ctx) }

// NewChecker builds a checker for the readiness config r of a process listening on port.
func NewChecker(r config.Ready, port int, d Deps) (Checker, error) { //nolint:ireturn // the interface is the API
	var (
		c   funcChecker
		err error
	)
	switch r.Kind {
	case kindHTTP:
		c, err = newHTTP(r.Value, port, d)
	case kindTCP:
		c, err = newTCP(r.Value, d)
	case kindLog:
		c, err = newLog(r.Value, d)
	case kindDelay:
		c, err = newDelay(r.Value, d)
	case kindNone:
		c = func(context.Context) (bool, error) { return true, nil }
	default:
		err = fmt.Errorf("unknown ready kind %q", r.Kind)
	}
	if err != nil {
		return nil, err
	}

	return c, nil
}

func newHTTP(value string, port int, d Deps) (funcChecker, error) {
	target := value
	if strings.HasPrefix(value, "/") {
		target = "http://" + net.JoinHostPort(loopbackHost, strconv.Itoa(port)) + value
	}
	u, err := url.Parse(target)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("invalid ready http target %q", value)
	}
	client := d.Client
	if client == nil {
		client = &http.Client{Timeout: httpClientTimeout}
	}

	return funcChecker(func(ctx context.Context) (bool, error) {
		req, rerr := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), http.NoBody)
		if rerr != nil {
			return false, rerr
		}
		resp, derr := client.Do(req)
		if derr != nil {
			return false, nil // an unreachable server is simply not ready yet
		}
		defer func() { _ = resp.Body.Close() }()

		if d.Status != 0 {
			return resp.StatusCode == d.Status, nil
		}

		return resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices, nil
	}), nil
}

func newTCP(value string, d Deps) (funcChecker, error) {
	_, p, err := net.SplitHostPort(value)
	if err != nil {
		return nil, fmt.Errorf("invalid ready tcp address %q: %w", value, err)
	}
	port, err := strconv.Atoi(p)
	if err != nil || port < 1 || port > 65535 {
		return nil, fmt.Errorf("invalid ready tcp port in %q", value)
	}
	if d.Table == nil {
		return nil, errors.New("tcp readiness needs a port table")
	}

	return funcChecker(func(context.Context) (bool, error) { return d.Table.Listening(port) }), nil
}

func newLog(value string, d Deps) (funcChecker, error) {
	re, err := regexp.Compile(value)
	if err != nil {
		return nil, fmt.Errorf("invalid ready log pattern: %w", err)
	}
	matched := false

	return funcChecker(func(context.Context) (bool, error) {
		for !matched {
			select {
			case line, ok := <-d.Lines:
				if !ok {
					return matched, nil
				}
				matched = re.MatchString(line)
			default:
				return matched, nil
			}
		}

		return true, nil
	}), nil
}

func newDelay(value string, d Deps) (funcChecker, error) {
	dur, err := time.ParseDuration(value)
	if err != nil {
		return nil, fmt.Errorf("invalid ready delay: %w", err)
	}
	clk := d.Clock
	if clk == nil {
		clk = RealClock{}
	}
	start := clk.Now()

	return funcChecker(func(context.Context) (bool, error) { return clk.Now().Sub(start) >= dur, nil }), nil
}

// Result is the outcome of Wait.
type Result struct {
	Ready   bool
	Elapsed time.Duration // clock time from start to ready, or until the timeout expired
}

// Wait polls c every interval until it is ready, the timeout elapses or ctx is done. A Check error counts
// as not ready, and each Check is bounded by the remaining timeout.
func Wait(ctx context.Context, c Checker, timeout, interval time.Duration, clk Clock) Result {
	start := clk.Now()
	for {
		elapsed := clk.Now().Sub(start)
		if elapsed >= timeout {
			return Result{Elapsed: elapsed}
		}
		cctx, cancel := context.WithTimeout(ctx, timeout-elapsed)
		ok, err := c.Check(cctx)
		cancel()
		elapsed = clk.Now().Sub(start)
		if err == nil && ok {
			return Result{Ready: true, Elapsed: elapsed}
		}
		if elapsed >= timeout || ctx.Err() != nil {
			return Result{Elapsed: elapsed}
		}
		select {
		case <-clk.After(interval):
		case <-ctx.Done():
			return Result{Elapsed: clk.Now().Sub(start)}
		}
	}
}

// NotReadyError is the typed error for a process that missed its readiness timeout.
func NotReadyError(proc string, timeout time.Duration) *errs.Error {
	return &errs.Error{
		Code:    errs.CodeReadyTimeout,
		Message: fmt.Sprintf("%s was not ready after %s", proc, timeout),
		Fix:     "inspect its output with: drover logs " + proc,
		Exit:    errs.ExitReadiness,
	}
}
