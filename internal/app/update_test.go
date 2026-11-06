//nolint:testpackage // exercises unexported wiring
package app

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/creativeprojects/go-selfupdate"
)

type fakeAsset struct {
	id   int64
	name string
}

func (a fakeAsset) GetID() int64                  { return a.id }
func (a fakeAsset) GetName() string               { return a.name }
func (a fakeAsset) GetSize() int                  { return 1 }
func (a fakeAsset) GetBrowserDownloadURL() string { return "https://example.test/" + a.name }

type fakeRelease struct{ tag string }

func (r fakeRelease) GetID() int64              { return 1 }
func (r fakeRelease) GetTagName() string        { return r.tag }
func (r fakeRelease) GetDraft() bool            { return false }
func (r fakeRelease) GetPrerelease() bool       { return false }
func (r fakeRelease) GetPublishedAt() time.Time { return time.Time{} }
func (r fakeRelease) GetReleaseNotes() string   { return "" }
func (r fakeRelease) GetName() string           { return r.tag }
func (r fakeRelease) GetURL() string            { return "https://example.test/releases/" + r.tag }

func (r fakeRelease) GetAssets() []selfupdate.SourceAsset {
	ver := strings.TrimPrefix(r.tag, "v")
	ext := ".tar.gz"
	if runtime.GOOS == "windows" {
		ext = ".zip"
	}

	return []selfupdate.SourceAsset{
		fakeAsset{1, fmt.Sprintf("go-template_%s_%s_%s%s", ver, runtime.GOOS, runtime.GOARCH, ext)},
		fakeAsset{2, fmt.Sprintf("go-template_%s_checksums.txt", ver)},
	}
}

type fakeSource struct{ tags []string }

func (s fakeSource) ListReleases(context.Context, selfupdate.Repository) ([]selfupdate.SourceRelease, error) {
	rels := make([]selfupdate.SourceRelease, 0, len(s.tags))
	for _, t := range s.tags {
		rels = append(rels, fakeRelease{t})
	}

	return rels, nil
}

func (fakeSource) DownloadReleaseAsset(context.Context, *selfupdate.Release, int64) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

type harness struct {
	u        *Updater
	out      *bytes.Buffer
	replaced int
}

func newHarness(t *testing.T, current string, tags ...string) *harness {
	h := &harness{out: &bytes.Buffer{}}
	h.u = &Updater{
		Source:  fakeSource{tags},
		Repo:    "owner/repo",
		Current: current,
		Exe:     filepath.Join(t.TempDir(), "go-template"),
		In:      strings.NewReader(""),
		Out:     h.out,
		Replace: func(context.Context, *selfupdate.Release, string) error { h.replaced++; return nil },
	}

	return h
}

func TestRefusesNonReleaseVersions(t *testing.T) {
	for _, v := range []string{"dev", "v1.2.3-dirty", "v1.2.3-4-gabc1234", "abc1234", ""} {
		h := newHarness(t, v, "v1.3.0")
		err := h.u.Run(context.Background(), UpdateOptions{Yes: true})
		if err == nil || !strings.Contains(err.Error(), "release builds") {
			t.Errorf("version %q: want release-build refusal, got %v", v, err)
		}
		if h.replaced != 0 {
			t.Errorf("version %q: replace must not run", v)
		}
	}
}

func TestUpToDate(t *testing.T) {
	h := newHarness(t, "v1.3.0", "v1.3.0", "v1.2.0")
	if err := h.u.Run(context.Background(), UpdateOptions{Yes: true}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.out.String(), "already up to date") || h.replaced != 0 {
		t.Errorf("out=%q replaced=%d", h.out, h.replaced)
	}
}

func TestCheckReportsUpdateWithoutReplacing(t *testing.T) {
	h := newHarness(t, "v1.2.0", "v1.3.0", "v1.2.0")
	if err := h.u.Run(context.Background(), UpdateOptions{Check: true}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.out.String(), "update available") || h.replaced != 0 {
		t.Errorf("out=%q replaced=%d", h.out, h.replaced)
	}
}

func TestNonInteractiveWithoutYesRefuses(t *testing.T) {
	h := newHarness(t, "v1.2.0", "v1.3.0")
	err := h.u.Run(context.Background(), UpdateOptions{})
	if err == nil || !strings.Contains(err.Error(), "--yes") || h.replaced != 0 {
		t.Errorf("err=%v replaced=%d", err, h.replaced)
	}
}

func TestInteractivePromptDecline(t *testing.T) {
	h := newHarness(t, "v1.2.0", "v1.3.0")
	h.u.Interactive = true
	h.u.In = strings.NewReader("n\n")
	if err := h.u.Run(context.Background(), UpdateOptions{}); err == nil || h.replaced != 0 {
		t.Errorf("err=%v replaced=%d", err, h.replaced)
	}
	if !strings.Contains(h.out.String(), "Update 1.2.0 -> 1.3.0? [y/N]") {
		t.Errorf("prompt missing: %q", h.out)
	}
}

func TestYesProceedsToReplace(t *testing.T) {
	h := newHarness(t, "v1.2.0", "v1.3.0")
	if err := h.u.Run(context.Background(), UpdateOptions{Yes: true}); err != nil {
		t.Fatal(err)
	}
	if h.replaced != 1 {
		t.Errorf("replaced=%d, want 1", h.replaced)
	}
}

func TestExplicitVersionSelectsThatRelease(t *testing.T) {
	h := newHarness(t, "v1.2.0", "v1.3.0", "v1.2.5")
	if err := h.u.Run(context.Background(), UpdateOptions{Yes: true, Version: "v1.2.5"}); err != nil {
		t.Fatal(err)
	}
	if h.replaced != 1 || !strings.Contains(h.out.String(), "1.2.5") {
		t.Errorf("out=%q replaced=%d", h.out, h.replaced)
	}
}

func TestNotWritablePrintsManualInstructions(t *testing.T) {
	h := newHarness(t, "v1.2.0", "v1.3.0")
	h.u.Exe = filepath.Join(t.TempDir(), "missing-dir", "go-template")
	if err := h.u.Run(context.Background(), UpdateOptions{Yes: true}); err != nil {
		t.Fatal(err)
	}
	if h.replaced != 0 || !strings.Contains(h.out.String(), "https://example.test/releases/v1.3.0") {
		t.Errorf("out=%q replaced=%d", h.out, h.replaced)
	}
}

func TestChecksumAssetName(t *testing.T) {
	got := (&checksumValidator{}).GetValidationAssetName("go-template_1.2.3_linux_amd64.tar.gz")
	if got != "go-template_1.2.3_checksums.txt" {
		t.Errorf("got %q", got)
	}
}
