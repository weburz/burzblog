// Package cli is the burzblog command: the server (`serve`, `user`) and the
// terminal writing workflow (`login`, `new`, `edit`, `push`, ...).
package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/weburz/burzblog/internal/client"
)

// App holds what commands need from the outside world, so tests can
// replace it.
type App struct {
	In        io.Reader
	Out       io.Writer
	Err       io.Writer
	ConfigDir string
	CacheDir  string
	Getenv    func(string) string
	Panel     fs.FS
	Version   string

	reader *bufio.Reader
}

// NewApp returns an App wired to the real terminal and user directories.
func NewApp(version string, panel fs.FS) *App {
	cfg, err := os.UserConfigDir()
	if err != nil {
		cfg = "."
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		cache = os.TempDir()
	}
	return &App{
		In: os.Stdin, Out: os.Stdout, Err: os.Stderr,
		ConfigDir: filepath.Join(cfg, "burzblog"), CacheDir: filepath.Join(cache, "burzblog"),
		Getenv: os.Getenv, Panel: panel, Version: version,
	}
}

// Execute runs the command line and returns the process exit code.
func (a *App) Execute(args []string) int {
	root := a.root()
	root.SetArgs(args)
	root.SetIn(a.In)
	root.SetOut(a.Out)
	root.SetErr(a.Err)
	if err := root.Execute(); err != nil {
		a.failure(err)
		return 1
	}
	return 0
}

func (a *App) root() *cobra.Command {
	root := &cobra.Command{
		Use:   "burzblog",
		Short: "Write blog posts in the browser or from your terminal",
		Long: `BurzBlog keeps your posts as Markdown in one small server.

Write in the browser panel, or from here:

  burzblog login https://blog.example.com
  burzblog new "My next post"
  burzblog edit my-next-post
  burzblog publish my-next-post`,
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       a.Version,
	}
	root.AddGroup(
		&cobra.Group{ID: "write", Title: "Writing:"},
		&cobra.Group{ID: "account", Title: "Connecting:"},
		&cobra.Group{ID: "host", Title: "Running the server:"},
	)
	for _, c := range []*cobra.Command{a.listCmd(), a.newCmd(), a.editCmd(), a.pullCmd(), a.pushCmd(),
		a.statusCmd("publish", "Publish a post so reader sites include it"),
		a.statusCmd("unpublish", "Turn a published post back into a draft"),
		a.statusCmd("archive", "Hide a post from the list without deleting it"),
		a.statusCmd("restore", "Bring an archived post back as a draft"),
		a.openCmd(), a.exportCmd(), a.backupCmd()} {
		c.GroupID = "write"
		root.AddCommand(c)
	}
	for _, c := range []*cobra.Command{a.loginCmd(), a.logoutCmd(), a.whoamiCmd()} {
		c.GroupID = "account"
		root.AddCommand(c)
	}
	root.AddCommand(a.healthCmd())
	for _, c := range []*cobra.Command{a.serveCmd(), a.userCmd()} {
		c.GroupID = "host"
		root.AddCommand(c)
	}
	return root
}

// clean makes server-supplied text safe to print on one line: every control
// character (C0, DEL, C1, which terminals act on) becomes U+FFFD.
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return unicode.ReplacementChar
		}
		return r
	}, s)
}

// cleanLines is clean for text that already spans lines, such as error
// messages: line breaks are kept.
func cleanLines(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' {
			return unicode.ReplacementChar
		}
		return r
	}, s)
}

// failure prints an error people can act on.
func (a *App) failure(err error) {
	var apiErr *client.Error
	if errors.As(err, &apiErr) {
		fmt.Fprintln(a.Err, a.paint(red, "error:"), cleanLines(apiErr.Message))
		if apiErr.Detail != "" {
			fmt.Fprintln(a.Err, "      ", cleanLines(apiErr.Detail))
		}
		if apiErr.Status == 401 && apiErr.Code == "authentication" {
			fmt.Fprintln(a.Err, "       Run `burzblog login <server>` to sign in again.")
		}
		return
	}
	fmt.Fprintln(a.Err, a.paint(red, "error:"), cleanLines(err.Error()))
}

// Config is what `burzblog login` remembers.
type Config struct {
	Server   string `json:"server"`
	Token    string `json:"token"`
	TokenID  int64  `json:"tokenId"`
	Username string `json:"username"`
}

func (a *App) configPath() string {
	return filepath.Join(a.ConfigDir, "config.json")
}

func (a *App) loadConfig() (*Config, error) {
	var c Config
	raw, err := os.ReadFile(a.configPath())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read %s: %w", a.configPath(), err)
	}
	if err == nil {
		if err := json.Unmarshal(raw, &c); err != nil {
			return nil, fmt.Errorf("%s is not valid JSON; delete it and run `burzblog login` again", a.configPath())
		}
	}
	// Environment wins, so reader builds can run without a config file.
	if v := a.Getenv("BURZBLOG_SERVER"); v != "" {
		c.Server = v
	}
	if v := a.Getenv("BURZBLOG_TOKEN"); v != "" {
		c.Token = v
	}
	return &c, nil
}

func (a *App) saveConfig(c *Config) error {
	if err := os.MkdirAll(a.ConfigDir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", a.ConfigDir, err)
	}
	raw, _ := json.MarshalIndent(c, "", "  ")
	return writeFileAtomic(a.configPath(), append(raw, '\n'), 0o600)
}

// client returns an API client for the signed-in server.
func (a *App) client() (*client.Client, *Config, error) {
	c, err := a.loadConfig()
	if err != nil {
		return nil, nil, err
	}
	if c.Server == "" || c.Token == "" {
		return nil, nil, errors.New("not signed in. Run `burzblog login https://your-blog.example.com` first")
	}
	return client.New(c.Server, c.Token, "burzblog-cli/"+a.Version), c, nil
}

func ctx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 2*time.Minute)
}

// writeFileAtomic replaces path with data without leaving a half-written
// file behind if something fails.
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// Terminal helpers.

func (a *App) terminal() bool {
	f, ok := a.In.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

func (a *App) outTerminal() bool {
	f, ok := a.Out.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

func (a *App) colour() bool {
	return a.outTerminal() && a.Getenv("NO_COLOR") == ""
}

const (
	red    = "31"
	green  = "32"
	yellow = "33"
	dim    = "2"
	bold   = "1"
)

func (a *App) paint(code, s string) string {
	if !a.colour() {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func (a *App) line() (string, error) {
	if a.reader == nil {
		a.reader = bufio.NewReader(a.In)
	}
	s, err := a.reader.ReadString('\n')
	if err != nil && (err != io.EOF || s == "") {
		return "", errors.New("no input")
	}
	return strings.TrimRight(s, "\r\n"), nil
}

func (a *App) prompt(label string) (string, error) {
	fmt.Fprint(a.Err, label)
	return a.line()
}

func (a *App) secret(label string) (string, error) {
	fmt.Fprint(a.Err, label)
	if f, ok := a.In.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		b, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(a.Err)
		return string(b), err
	}
	return a.line()
}

// confirm asks a yes/no question; without a terminal it returns def.
func (a *App) confirm(question string, def bool) bool {
	if !a.terminal() {
		return def
	}
	hint := " [Y/n] "
	if !def {
		hint = " [y/N] "
	}
	answer, err := a.prompt(question + hint)
	if err != nil {
		return def
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return true
	case "n", "no":
		return false
	}
	return def
}

func ago(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
	return t.Local().Format("2 Jan 2006")
}
