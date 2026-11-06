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

	"github.com/creativeprojects/go-selfupdate"
	"github.com/spf13/cobra"
)

// repoSlug is the GitHub repository releases are fetched from; change it when using this template.
const repoSlug = "cabrera-evil/go-template"

var (
	releaseVersion = regexp.MustCompile(`^v?\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$`)
	buildSuffix    = regexp.MustCompile(`-dirty$|-\d+-g[0-9a-f]+`)
)

var (
	errNotRelease     = errors.New("updates only work for release builds")
	errNeedsYes       = errors.New("not running interactively: pass --yes to confirm the update")
	errReleaseMissing = errors.New("release not found")
)

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
	Interactive bool
	In          io.Reader
	Out         io.Writer
	// Replace installs the release over Exe; defaults to the library's UpdateTo.
	Replace func(ctx context.Context, rel *selfupdate.Release, exe string) error
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

	var (
		rel   *selfupdate.Release
		found bool
	)
	if opts.Version != "" {
		rel, found, err = up.DetectVersion(ctx, repo, opts.Version)
	} else {
		rel, found, err = up.DetectLatest(ctx, repo)
	}

	if err != nil {
		return fmt.Errorf("look up release: %w", err)
	}

	if !found {
		return fmt.Errorf("%w for this platform (version %q)", errReleaseMissing, opts.Version)
	}

	current := strings.TrimPrefix(u.Current, "v")
	latest := rel.Version()
	newer := rel.GreaterThan(current)
	same := rel.Equal(current)
	// An explicit --version may also downgrade; otherwise only newer releases count.
	available := newer || (opts.Version != "" && !same)

	if opts.Check {
		_, _ = fmt.Fprintf(u.Out, "current: %s\nlatest:  %s\n", current, latest)
		if available {
			_, _ = fmt.Fprintln(u.Out, "update available")
		} else {
			_, _ = fmt.Fprintln(u.Out, "already up to date")
		}

		return nil
	}

	if !available {
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

	replace := u.Replace
	if replace == nil {
		replace = up.UpdateTo
	}

	if err = replace(ctx, rel, u.Exe); err != nil {
		return fmt.Errorf("update binary: %w", err)
	}

	_, _ = fmt.Fprintf(u.Out, "updated %s -> %s\n", current, latest)

	return nil
}

func (u *Updater) confirm(current, latest string) error {
	if !u.Interactive {
		return errNeedsYes
	}

	_, _ = fmt.Fprintf(u.Out, "Update %s -> %s? [y/N] ", current, latest)

	answer, _ := bufio.NewReader(u.In).ReadString('\n')
	if a := strings.ToLower(strings.TrimSpace(answer)); a != "y" && a != "yes" {
		return errors.New("update cancelled")
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

func newUpdateCmd(version string) *cobra.Command {
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

			st, _ := os.Stdin.Stat()
			u := &Updater{
				Source:      src,
				Repo:        repoSlug,
				Current:     version,
				Exe:         exe,
				Interactive: st != nil && st.Mode()&os.ModeCharDevice != 0,
				In:          cmd.InOrStdin(),
				Out:         cmd.OutOrStdout(),
			}

			return u.Run(cmd.Context(), opts)
		},
	}
	cmd.Flags().BoolVar(&opts.Check, "check", false, "only report whether an update is available")
	cmd.Flags().StringVar(&opts.Version, "version", "", "install this release tag (e.g. v1.2.3)")
	cmd.Flags().BoolVarP(&opts.Yes, "yes", "y", false, "skip the confirmation prompt")

	return cmd
}
