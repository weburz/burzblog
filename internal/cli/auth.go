package cli

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/weburz/burzblog/internal/client"
)

func (a *App) loginCmd() *cobra.Command {
	var username string
	var passwordStdin bool
	cmd := &cobra.Command{
		Use:   "login <server>",
		Short: "Sign in to a BurzBlog server from this computer",
		Example: `  burzblog login https://blog.example.com
  burzblog login https://blog.example.com --username ada
  echo "$PASSWORD" | burzblog login https://blog.example.com -u ada --password-stdin`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			server, err := normalizeServer(args[0])
			if err != nil {
				return err
			}
			if username == "" {
				if username, err = a.prompt("Username: "); err != nil {
					return errors.New("enter a username, or pass --username")
				}
			}
			password, err := a.secret("Password: ")
			if passwordStdin && err == nil {
				password = strings.TrimSpace(password)
			}
			if err != nil || password == "" {
				return errors.New("enter your password")
			}
			host, _ := os.Hostname()
			name := "CLI"
			if host != "" {
				name = "CLI on " + host
			}
			c, cancel := ctx()
			defer cancel()
			tok, err := client.New(server, "", "burzblog-cli/"+a.Version).IssueToken(c, username, password, name)
			if err != nil {
				return err
			}
			old, _ := a.loadConfig()
			if err := a.saveConfig(&Config{Server: server, Token: tok.Secret, TokenID: tok.ID, Username: tok.Username}); err != nil {
				return err
			}
			// Signing in again replaces the old token; revoke it so it does
			// not linger in the panel's token list.
			if old != nil && old.Server == server && old.TokenID != 0 && old.TokenID != tok.ID {
				_ = client.New(server, tok.Secret, "").RevokeToken(c, old.TokenID)
			}
			fmt.Fprintf(a.Out, "%s Signed in to %s as %s.\n", a.paint(green, "✓"), server, a.paint(bold, clean(tok.Username)))
			fmt.Fprintln(a.Out, "  Try: burzblog list")
			return nil
		},
	}
	cmd.Flags().StringVarP(&username, "username", "u", "", "your BurzBlog username")
	cmd.Flags().BoolVar(&passwordStdin, "password-stdin", false, "read the password from standard input")
	return cmd
}

func normalizeServer(raw string) (string, error) {
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(strings.TrimRight(raw, "/"))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return "", fmt.Errorf("%q is not a server address like https://blog.example.com", raw)
	}
	if u.Scheme == "http" && !isLocal(u.Hostname()) {
		return "", errors.New("use https:// for a server that is not on this computer; your password would travel unencrypted")
	}
	return u.Scheme + "://" + u.Host + strings.TrimRight(u.Path, "/"), nil
}

func isLocal(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1" || strings.HasSuffix(host, ".localhost")
}

func (a *App) logoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Sign this computer out and revoke its token",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := a.loadConfig()
			if err != nil {
				return err
			}
			if cfg.Token == "" {
				fmt.Fprintln(a.Out, "Not signed in.")
				return nil
			}
			revoked := false
			if cfg.TokenID != 0 {
				c, cancel := ctx()
				defer cancel()
				revoked = client.New(cfg.Server, cfg.Token, "").RevokeToken(c, cfg.TokenID) == nil
			}
			if err := os.Remove(a.configPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			fmt.Fprintf(a.Out, "%s Signed out of %s.\n", a.paint(green, "✓"), cfg.Server)
			if !revoked {
				fmt.Fprintln(a.Out, "  The server could not be reached, so revoke this computer's token in Settings.")
			}
			return nil
		},
	}
}

func (a *App) whoamiCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Show which server and account this computer uses",
		Example: `  burzblog whoami
  burzblog whoami --json            # {"username":"ada","server":"https://blog.example.com"}
  BURZBLOG_SERVER=https://blog.example.com BURZBLOG_TOKEN=bzb_... burzblog whoami --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, cfg, err := a.client()
			if err != nil {
				return err
			}
			c, cancel := ctx()
			defer cancel()
			name, err := cl.Whoami(c)
			if err != nil {
				return err
			}
			if a.asJSON {
				return a.emit(struct {
					Username string `json:"username"`
					Server   string `json:"server"`
				}{name, cfg.Server})
			}
			fmt.Fprintf(a.Out, "%s on %s\n", a.paint(bold, clean(name)), cfg.Server)
			return nil
		},
	}
}
