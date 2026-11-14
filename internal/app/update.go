package app

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/creativeprojects/go-selfupdate"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
)

// repoSlug is the GitHub repository releases are fetched from.
const repoSlug = "cabrera-evil/drover-cli"

var (
	releaseVersion = regexp.MustCompile(`^v?\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$`)
	buildSuffix    = regexp.MustCompile(`-dirty$|-\d+-g[0-9a-f]+`)
)

var (
	errNotRelease     = errors.New("updates only work for release builds")
	errNeedsYes       = errors.New("not running interactively: pass --yes to confirm the update")
	errReleaseMissing = errors.New("release not found")
	errCancelled      = errors.New("update cancelled")
	errIncomplete     = errors.New("interactive update: ended before finishing")
	// ErrReported marks an error whose message the interactive view already showed; main must not print it again.
	ErrReported = errors.New("already reported")
)

// reportedError keeps err's text and cause and adds ErrReported.
type reportedError struct{ error }

func (e reportedError) Unwrap() []error { return []error{e.error, ErrReported} }

// UpdateOptions mirrors the `update` command flags.
type UpdateOptions struct {
	Check   bool
	Version string
	Yes     bool
}

// Updater checks GitHub Releases and replaces the running binary.
type Updater struct {
	Source      selfupdate.Source
	Repo        string
	Current     string
	Exe         string
	Interactive bool // stdin and stdout are terminals, so a prompt can be asked and answered
	OutTTY      bool // stdout is a terminal
	In          io.Reader
	Out         io.Writer
	// Replace installs the release over Exe; defaults to the library's UpdateTo.
	Replace func(ctx context.Context, rel *selfupdate.Release, exe string) error
	// RunTUI runs the interactive model; defaults to a Bubble Tea program on In/Out.
	RunTUI func(ctx context.Context, m tea.Model) (tea.Model, error)
}

// checksumValidator validates against GoReleaser's <name>_<version>_checksums.txt.
type checksumValidator struct{ selfupdate.ChecksumValidator }

func (*checksumValidator) GetValidationAssetName(releaseFilename string) string {
	parts := strings.Split(releaseFilename, "_")
	if len(parts) < 3 { //nolint:mnd // name_version_os_arch.ext
		return "checksums.txt"
	}

	return strings.Join(parts[:len(parts)-2], "_") + "_checksums.txt"
}

// NewGitHubSource builds the release source, using GITHUB_TOKEN when set.
func NewGitHubSource() (*selfupdate.GitHubSource, error) {
	return selfupdate.NewGitHubSource(selfupdate.GitHubConfig{APIToken: os.Getenv("GITHUB_TOKEN")})
}

func checkReleaseBuild(version string) error {
	if !releaseVersion.MatchString(version) || buildSuffix.MatchString(version) {
		return fmt.Errorf("%w (current version: %q)", errNotRelease, version)
	}

	return nil
}

// Run executes the update flow.
func (u *Updater) Run(ctx context.Context, opts UpdateOptions) error {
	if err := checkReleaseBuild(u.Current); err != nil {
		return err
	}

	up, err := selfupdate.NewUpdater(selfupdate.Config{
		Source:    u.Source,
		Validator: &checksumValidator{},
	})
	if err != nil {
		return fmt.Errorf("configure updater: %w", err)
	}

	repo := selfupdate.ParseSlug(u.Repo)

	replace := u.Replace
	if replace == nil {
		replace = up.UpdateTo
	}

	if u.Interactive && u.OutTTY && !opts.Yes && !opts.Check {
		return u.runTUI(ctx, up, repo, opts, replace)
	}

	rel, chk, err := u.detect(ctx, up, repo, opts.Version)
	if err != nil {
		return err
	}

	current, latest := chk.Current, chk.Latest

	if opts.Check {
		_, _ = fmt.Fprintf(u.Out, "current: %s\nlatest:  %s\n", current, latest)
		if chk.Available {
			_, _ = fmt.Fprintln(u.Out, "update available")
		} else {
			_, _ = fmt.Fprintln(u.Out, "already up to date")
		}

		return nil
	}

	if !chk.Available {
		_, _ = fmt.Fprintf(u.Out, "already up to date (%s)\n", current)

		return nil
	}

	if !u.writable() {
		_, _ = fmt.Fprintf(u.Out, "%s is not writable; download %s manually from:\n%s\n", u.Exe, latest, rel.URL)

		return nil
	}

	if !opts.Yes {
		if err = u.confirm(current, latest); err != nil {
			return err
		}
	}

	if err = replace(ctx, rel, u.Exe); err != nil {
		return fmt.Errorf("update binary: %w", err)
	}

	_, _ = fmt.Fprintf(u.Out, "updated %s -> %s\n", current, latest)

	return nil
}

// detect looks up the target release and compares it with the running version.
func (u *Updater) detect(
	ctx context.Context, up *selfupdate.Updater, repo selfupdate.Repository, version string,
) (*selfupdate.Release, updateCheck, error) {
	var (
		rel   *selfupdate.Release
		found bool
		err   error
	)
	if version != "" {
		rel, found, err = up.DetectVersion(ctx, repo, version)
	} else {
		rel, found, err = up.DetectLatest(ctx, repo)
	}

	if err != nil {
		return nil, updateCheck{}, fmt.Errorf("look up release: %w", err)
	}

	if !found {
		return nil, updateCheck{}, fmt.Errorf("%w for this platform (version %q)", errReleaseMissing, version)
	}

	current := strings.TrimPrefix(u.Current, "v")
	// An explicit --version may also downgrade; otherwise only newer releases count.
	available := rel.GreaterThan(current) || (version != "" && !rel.Equal(current))

	return rel, updateCheck{Current: current, Latest: rel.Version(), Available: available}, nil
}

// runTUI drives the interactive flow; the TUI reads its keys from In and renders to Out.
func (u *Updater) runTUI(
	ctx context.Context,
	up *selfupdate.Updater,
	repo selfupdate.Repository,
	opts UpdateOptions,
	replace func(context.Context, *selfupdate.Release, string) error,
) error {
	var rel *selfupdate.Release

	detect := func(ctx context.Context) (updateCheck, error) {
		r, chk, err := u.detect(ctx, up, repo, opts.Version)
		if err != nil {
			return chk, err
		}

		rel = r
		if chk.Available && !u.writable() {
			chk.ManualURL, chk.Exe = r.URL, u.Exe
		}

		return chk, nil
	}
	install := func(ctx context.Context) error {
		if err := replace(ctx, rel, u.Exe); err != nil {
			return fmt.Errorf("update binary: %w", err)
		}

		return nil
	}

	run := u.RunTUI
	if run == nil {
		run = func(ctx context.Context, m tea.Model) (tea.Model, error) {
			p := tea.NewProgram(m, tea.WithInput(u.In), tea.WithOutput(u.Out), tea.WithContext(ctx),
				tea.WithFilter(swallowQuitWhileUpdating))

			return p.Run() //nolint:wrapcheck // wrapped by caller
		}
	}

	final, err := run(ctx, newUpdateModel(ctx, detect, install))
	if err != nil {
		return fmt.Errorf("interactive update: %w", err)
	}

	m, ok := final.(updateModel)
	if !ok || m.state != stateDone {
		return errIncomplete
	}

	if err = m.Err(); err != nil {
		return reportedError{err}
	}

	return nil
}

func (u *Updater) confirm(current, latest string) error {
	if !u.Interactive {
		return errNeedsYes
	}

	_, _ = fmt.Fprintf(u.Out, "Update %s -> %s? [y/N] ", current, latest)

	answer, _ := bufio.NewReader(u.In).ReadString('\n')
	if a := strings.ToLower(strings.TrimSpace(answer)); a != "y" && a != "yes" {
		return errCancelled
	}

	return nil
}

// writable reports whether a replacement file can be created next to the executable.
func (u *Updater) writable() bool {
	f, err := os.CreateTemp(filepath.Dir(u.Exe), ".update-check-*")
	if err != nil {
		return false
	}

	_ = f.Close()
	_ = os.Remove(f.Name())

	return true
}

// newUpdater wires the streams the update runs on; the TUI is used only when both are real terminals.
func newUpdater(src selfupdate.Source, version, exe string, in io.Reader, out io.Writer) *Updater {
	return &Updater{
		Source:      src,
		Repo:        repoSlug,
		Current:     version,
		Exe:         exe,
		Interactive: isTerminal(in) && isTerminal(out), // a prompt needs a visible question and a typed answer
		OutTTY:      isTerminal(out),
		In:          in,
		Out:         out,
	}
}

func isTerminal(s any) bool {
	f, ok := s.(*os.File)

	return ok && isatty.IsTerminal(f.Fd())
}

func newUpdateCmd(version string) *cobra.Command { return newUpdateCmdWith(version, nil) }

// newUpdateCmdWith builds the command; adjust, when set, may change the Updater right before it runs.
func newUpdateCmdWith(version string, adjust func(*Updater)) *cobra.Command {
	var opts UpdateOptions

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update to the latest release",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			src, err := NewGitHubSource()
			if err != nil {
				return fmt.Errorf("github source: %w", err)
			}

			exe, err := os.Executable()
			if err != nil {
				return fmt.Errorf("locate executable: %w", err)
			}

			// os.Executable may be a symlink (argv-derived on darwin); replace the real file, not the link.
			if resolved, rerr := filepath.EvalSymlinks(exe); rerr == nil {
				exe = resolved
			}

			u := newUpdater(src, version, exe, cmd.InOrStdin(), cmd.OutOrStdout())

			if adjust != nil {
				adjust(u)
			}

			return u.Run(cmd.Context(), opts)
		},
	}
	cmd.Flags().BoolVar(&opts.Check, "check", false, "only report whether an update is available")
	cmd.Flags().StringVar(&opts.Version, "version", "", "install this release tag (e.g. v1.2.3)")
	cmd.Flags().BoolVarP(&opts.Yes, "yes", "y", false, "skip the confirmation prompt")

	return cmd
}
