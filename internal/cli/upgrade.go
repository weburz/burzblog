package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/weburz/burzblog/internal/release"
)

// errUpdateAvailable makes `burzblog upgrade --check` exit ExitUpdate when a
// newer release exists. The result is already printed.
var errUpdateAvailable = &exitError{"update_available", ExitUpdate, "an update is available"}

// restartCommand is what `upgrade` tells people to run afterwards.
const restartCommand = "sudo systemctl restart burzblog"

// releaseSource is where `upgrade` looks for releases; tests replace it.
type releaseSource struct {
	// LatestURL is the "latest release" API URL, DownloadBase the folder of
	// per-version download folders.
	LatestURL, DownloadBase string
	Client                  *http.Client
	GOOS, GOARCH            string
	// Executable returns the path of the running binary.
	Executable func() (string, error)
	// InContainer reports whether BurzBlog runs inside a container.
	InContainer func() bool
}

// defaultReleaseSource is the real GitHub releases and the running binary.
func (a *App) defaultReleaseSource() releaseSource {
	return releaseSource{
		LatestURL: release.LatestURL, DownloadBase: release.DownloadBase,
		Client: release.NewClient(10 * time.Minute),
		GOOS:   runtime.GOOS, GOARCH: runtime.GOARCH,
		Executable: os.Executable,
		InContainer: func() bool {
			// Docker and Podman mark their containers with a file; the
			// official image also runs BurzBlog as PID 1 on /data.
			for _, f := range []string{"/.dockerenv", "/run/.containerenv"} {
				if _, err := os.Stat(f); err == nil {
					return true
				}
			}
			return a.Getenv("BURZBLOG_DATA") == "/data" && os.Getpid() == 1
		},
	}
}

func (a *App) upgradeCmd() *cobra.Command {
	var check, force bool
	var version string
	cmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Replace this binary with the latest release",
		Long: `Downloads the latest release from GitHub, checks it against the release's
SHA256SUMS file and replaces the running binary. For installs that run as a
plain binary or a systemd service; in a container, pull the new image instead.

It does not restart anything and never runs what it downloaded. Restart the
service yourself (sudo systemctl restart burzblog). On the next start the
database backs itself up to <data>/backups and migrates; see
docs/self-hosting.md#upgrades.

--check only reports whether a newer release exists. It exits 0 when this
binary is up to date and 7 when an update is available.

A downgrade or a reinstall of the same version needs --force. A development
build (version "dev") only upgrades when --version names a release. Downloads
use https only.`,
		Example: `  burzblog upgrade --check
  burzblog upgrade
  sudo burzblog upgrade --version v1.4.0
  burzblog upgrade --check --json     # {"current":"v1.3.0","latest":"v1.4.0","updateAvailable":true}`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			src := a.release
			if src == nil {
				d := a.defaultReleaseSource()
				src = &d
			}
			return a.upgrade(cmd.Context(), *src, version, check, force)
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "only report whether an update exists (exit 7 if so)")
	cmd.Flags().StringVar(&version, "version", "", "install this release, for example v1.4.0, instead of the latest")
	cmd.Flags().BoolVar(&force, "force", false, "allow a downgrade or reinstalling the running version")
	return cmd
}

func (a *App) upgrade(parent context.Context, src releaseSource, want string, check, force bool) error {
	if src.InContainer() {
		return &exitError{"container", ExitError,
			"BurzBlog runs in a container here; pull the new image instead (docker compose pull && docker compose up -d)"}
	}
	var current release.Version
	haveCurrent := false
	if v, err := release.ParseVersion(a.Version); err == nil {
		current, haveCurrent = v, true
	} else if want == "" {
		return usagef("this is a development build (version %q), so there is no release to compare with. Name one with --version vX.Y.Z", a.Version)
	}

	if parent == nil {
		parent = context.Background()
	}
	c, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	target := want
	if want == "" {
		var err error
		if target, err = release.LatestAt(c, src.Client, src.LatestURL); err != nil {
			return fmt.Errorf("%w\nCheck your network, or name a release with --version vX.Y.Z", err)
		}
	}
	tv, err := release.ParseVersion(target)
	if err != nil {
		return usagef("--version: %s", err)
	}
	cmp := 1 // the target is newer, or the running version is unknown
	if haveCurrent {
		cmp = release.Compare(tv, current)
	}

	if check {
		available := cmp > 0
		if a.asJSON {
			if err := a.emit(struct {
				Current         string `json:"current"`
				Latest          string `json:"latest"`
				UpdateAvailable bool   `json:"updateAvailable"`
			}{a.Version, target, available}); err != nil {
				return err
			}
		} else if available {
			fmt.Fprintf(a.Out, "Update available: %s (running %s). Run `burzblog upgrade`.\n", target, a.Version)
		} else {
			fmt.Fprintf(a.Out, "%s Already up to date (%s).\n", a.paint(green, "✓"), a.Version)
		}
		if available {
			return errUpdateAvailable
		}
		return nil
	}

	switch {
	case cmp == 0 && !force:
		return a.upToDate(target)
	case cmp < 0 && want == "":
		return a.upToDate(a.Version)
	case cmp < 0 && !force:
		return usagef("%s is older than the running %s. Add --force to downgrade; the database may then be too new for it, see docs/self-hosting.md#upgrades", target, a.Version)
	}

	path, err := src.Executable()
	if err != nil {
		return fmt.Errorf("find the running binary: %w", err)
	}
	if path, err = filepath.EvalSymlinks(path); err != nil {
		return fmt.Errorf("find the running binary: %w", err)
	}
	if src.GOOS == "windows" {
		return fmt.Errorf("upgrade cannot replace a running program on Windows; download %s from the release page instead",
			release.AssetName(target, src.GOOS, src.GOARCH))
	}
	if err := a.install(parent, src, path, target); err != nil {
		return err
	}
	if a.asJSON {
		return a.emit(struct {
			From    string `json:"from"`
			To      string `json:"to"`
			Path    string `json:"path"`
			Restart string `json:"restart"`
		}{a.Version, target, path, restartCommand})
	}
	fmt.Fprintf(a.Out, "%s Installed burzblog %s at %s (was %s).\n", a.paint(green, "✓"), target, path, a.Version)
	fmt.Fprintf(a.Out, "Nothing was restarted. Restart the service to use it:\n\n  %s\n\n", restartCommand)
	fmt.Fprintln(a.Out, "On start the database backs itself up to <data>/backups and migrates to the new schema.")
	return nil
}

func (a *App) upToDate(version string) error {
	if a.asJSON {
		return a.emit(struct {
			Current         string `json:"current"`
			Latest          string `json:"latest"`
			UpdateAvailable bool   `json:"updateAvailable"`
		}{a.Version, version, false})
	}
	fmt.Fprintf(a.Out, "%s Already up to date (%s).\n", a.paint(green, "✓"), a.Version)
	return nil
}

// install downloads target for this platform, checks its SHA-256 sum and
// replaces path with it by renaming a file from the same directory, so the
// binary is never half written.
func (a *App) install(parent context.Context, src releaseSource, path, target string) error {
	dir := filepath.Dir(path)
	// Created first, so a read-only directory fails before any download.
	tmp, err := os.CreateTemp(dir, ".burzblog-upgrade-*")
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return fmt.Errorf("cannot write to %s: %w\nRun the upgrade with permission to replace %s, for example: sudo %s upgrade", dir, err, path, path)
		}
		return fmt.Errorf("create a temporary file in %s: %w", dir, err)
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()

	asset := release.AssetName(target, src.GOOS, src.GOARCH)
	c, cancel := context.WithTimeout(parent, 10*time.Minute)
	defer cancel()

	var sums strings.Builder
	if _, err := release.Fetch(c, src.Client, release.AssetURL(src.DownloadBase, target, release.ChecksumName(target)), release.MaxSumsBytes, &sums); err != nil {
		return err
	}
	want, err := release.Checksum([]byte(sums.String()), asset)
	if err != nil {
		return err
	}
	got, err := release.Fetch(c, src.Client, release.AssetURL(src.DownloadBase, target, asset), release.MaxBinaryBytes, tmp)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("the downloaded %s does not match its checksum (expected %s, got %s); nothing was installed", asset, want, got)
	}
	if err := tmp.Chmod(0o755); err != nil {
		return fmt.Errorf("make the new binary executable: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("write the new binary: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write the new binary: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}
