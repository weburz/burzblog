package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/weburz/burzblog/internal/auth"
	"github.com/weburz/burzblog/internal/server"
	"github.com/weburz/burzblog/internal/store"
)

// dataDir picks the data directory: the flag, BURZBLOG_DATA, systemd's
// STATE_DIRECTORY, or ./burzblog-data.
func (a *App) dataDir(flag string) string {
	for _, v := range []string{flag, a.Getenv("BURZBLOG_DATA"), a.Getenv("STATE_DIRECTORY")} {
		if v != "" {
			// systemd may pass several directories separated by colons.
			return strings.Split(v, ":")[0]
		}
	}
	return "burzblog-data"
}

func (a *App) serveCmd() *cobra.Command {
	var addr, data, origin, hook string
	var cfg server.Config // holds the site settings until serving
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the BurzBlog server and browser panel",
		Long: `Runs the API and the browser panel in one process, storing everything in
one SQLite file in the data directory.

Put it behind a TLS proxy (Caddy, nginx) and set --origin to the public
URL. Settings can also come from the environment:

  BURZBLOG_ADDR, BURZBLOG_DATA, BURZBLOG_ORIGIN, BURZBLOG_DEPLOY_HOOK

Feeds and the sitemap (GET /api/content/feed.xml, feed.atom, feed.json and
sitemap.xml, also in the export archive) are made for your website and need its
public address:

  BURZBLOG_SITE_URL, BURZBLOG_SITE_TITLE, BURZBLOG_SITE_DESCRIPTION,
  BURZBLOG_SITE_LANGUAGE, BURZBLOG_POST_PATH, BURZBLOG_TAG_PATH`,
		Example: `  burzblog serve
  burzblog serve --addr 127.0.0.1:8080 --data /var/lib/burzblog --origin https://blog.example.com`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			addr = firstOf(addr, a.Getenv("BURZBLOG_ADDR"), "127.0.0.1:8080")
			origin = firstOf(origin, a.Getenv("BURZBLOG_ORIGIN"), "http://"+addr)
			hook = firstOf(hook, a.Getenv("BURZBLOG_DEPLOY_HOOK"))
			cfg.SiteURL = firstOf(cfg.SiteURL, a.Getenv("BURZBLOG_SITE_URL"))
			cfg.SiteTitle = firstOf(cfg.SiteTitle, a.Getenv("BURZBLOG_SITE_TITLE"))
			cfg.SiteDescription = firstOf(cfg.SiteDescription, a.Getenv("BURZBLOG_SITE_DESCRIPTION"))
			cfg.SiteLanguage = firstOf(cfg.SiteLanguage, a.Getenv("BURZBLOG_SITE_LANGUAGE"))
			cfg.PostPath = firstOf(cfg.PostPath, a.Getenv("BURZBLOG_POST_PATH"))
			cfg.TagPath = firstOf(cfg.TagPath, a.Getenv("BURZBLOG_TAG_PATH"))
			dir := a.dataDir(data)

			log := slog.New(slog.NewTextHandler(a.Err, &slog.HandlerOptions{Level: slog.LevelInfo}))
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			st, err := store.Open(ctx, dir)
			if err != nil {
				return err
			}
			defer st.Close()
			svc, err := auth.New(st)
			if err != nil {
				return err
			}
			cfg.Origin, cfg.DeployHook, cfg.Panel, cfg.Version, cfg.Logger = origin, hook, a.Panel, a.Version, log
			srv, err := server.New(cfg, st, svc)
			if err != nil {
				return err
			}
			if err := announceSetup(ctx, log, svc, origin, dir); err != nil {
				return err
			}

			srv.StartScheduler(ctx)

			ln, err := net.Listen("tcp", addr)
			if err != nil {
				return fmt.Errorf("listen on %s: %w", addr, err)
			}
			hs := &http.Server{
				Handler:           srv.Handler(),
				ReadHeaderTimeout: 10 * time.Second,
				ReadTimeout:       60 * time.Second,
				WriteTimeout:      120 * time.Second,
				IdleTimeout:       120 * time.Second,
				MaxHeaderBytes:    64 << 10,
				ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
			}
			log.Info("BurzBlog is running", "listen", ln.Addr().String(), "open", origin, "data", dir, "version", a.Version)
			errc := make(chan error, 1)
			go func() { errc <- hs.Serve(ln) }()
			select {
			case err := <-errc:
				if !errors.Is(err, http.ErrServerClosed) {
					return err
				}
			case <-ctx.Done():
				log.Info("shutting down")
			}
			stop() // ends the scheduler even when Serve failed first
			shutdown, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			if err := hs.Shutdown(shutdown); err != nil {
				log.Warn("shutdown", "err", err)
			}
			srv.Close(shutdown)
			return nil
		},
	}
	cmd.Flags().StringVar(&addr, "addr", "", "address to listen on (default 127.0.0.1:8080)")
	cmd.Flags().StringVar(&data, "data", "", "directory for the database (default $BURZBLOG_DATA, $STATE_DIRECTORY or ./burzblog-data)")
	cmd.Flags().StringVar(&origin, "origin", "", "public URL people open, e.g. https://blog.example.com (default http://<addr>)")
	cmd.Flags().StringVar(&hook, "deploy-hook", "", "URL to POST after published posts change; prefer BURZBLOG_DEPLOY_HOOK, it is a secret")
	cmd.Flags().StringVar(&cfg.SiteURL, "site-url", "", "public address of your website, e.g. https://example.com; needed for feeds and the sitemap")
	cmd.Flags().StringVar(&cfg.SiteTitle, "site-title", "", "website name in feeds (default the site's host)")
	cmd.Flags().StringVar(&cfg.SiteDescription, "site-description", "", "website description in feeds (default the title)")
	cmd.Flags().StringVar(&cfg.SiteLanguage, "site-language", "", "language of the website, e.g. en or pt-BR (default en)")
	cmd.Flags().StringVar(&cfg.PostPath, "post-path", "", "where a post lives on your website (default "+server.DefaultPostPath+")")
	cmd.Flags().StringVar(&cfg.TagPath, "tag-path", "", "where a tag page lives on your website, e.g. /tags/{tag}; adds them to the sitemap")
	return cmd
}

// announceSetup does nothing when accounts exist. Otherwise it issues the
// one-time first-run token and logs the setup link, the only place the token
// is ever written. The link keeps the token in the fragment so it never
// reaches a proxy, an access log or a Referer header.
func announceSetup(ctx context.Context, log *slog.Logger, svc *auth.Service, origin, dir string) error {
	token, err := svc.IssueSetupToken(ctx, auth.SetupTTL)
	if errors.Is(err, auth.ErrSetupDone) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("prepare first-run setup: %w", err)
	}
	log.Warn(fmt.Sprintf("no accounts yet; open %s/setup#token=%s to create the first one "+
		"(valid for %d minutes; restart to get a new link), or run: burzblog user add <name> --data %s",
		strings.TrimRight(origin, "/"), token, int(auth.SetupTTL.Minutes()), dir))
	return nil
}

// healthCmd lets container health checks probe the server without curl.
func (a *App) healthCmd() *cobra.Command {
	url := "http://127.0.0.1:8080/healthz"
	cmd := &cobra.Command{
		Use:    "healthcheck",
		Short:  "Exit 0 if the local server answers /healthz",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c := &http.Client{Timeout: 4 * time.Second}
			resp, err := c.Get(url)
			if err != nil {
				return err
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("health check answered HTTP %d", resp.StatusCode)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&url, "url", url, "health endpoint to probe")
	return cmd
}

func firstOf(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func (a *App) userCmd() *cobra.Command {
	var data string
	cmd := &cobra.Command{
		Use:   "user",
		Short: "Manage accounts on this server (run on the host)",
	}
	cmd.PersistentFlags().StringVar(&data, "data", "", "data directory (default $BURZBLOG_DATA, $STATE_DIRECTORY or ./burzblog-data)")

	open := func() (*store.Store, error) {
		return store.Open(context.Background(), a.dataDir(data))
	}
	newPassword := func(name string) (string, error) {
		pw, err := a.secret("New password for " + name + ": ")
		if err != nil {
			return "", errors.New("enter a password")
		}
		if err := auth.CheckPassword(pw); err != nil {
			return "", err
		}
		if a.terminal() {
			again, err := a.secret("Repeat it: ")
			if err != nil || again != pw {
				return "", errors.New("the passwords do not match")
			}
		}
		return auth.HashPassword(pw)
	}

	add := &cobra.Command{
		Use:   "add <username>",
		Short: "Create an account",
		Long: `Creates an account and asks for its password, at least 12 characters. Without a
terminal the password is read from standard input.`,
		Example: "  burzblog user add ada --data /var/lib/burzblog",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := store.NormalizeUsername(args[0])
			if err := auth.CheckUsername(name); err != nil {
				return err
			}
			st, err := open()
			if err != nil {
				return err
			}
			defer st.Close()
			hash, err := newPassword(name)
			if err != nil {
				return err
			}
			if _, err := st.CreateUser(context.Background(), name, hash); errors.Is(err, store.ErrTaken) {
				return fmt.Errorf("%s already exists; use `burzblog user passwd %s` to change the password", name, name)
			} else if err != nil {
				return err
			}
			fmt.Fprintf(a.Out, "%s Created %s. They can sign in to the panel or run `burzblog login`.\n", a.paint(green, "✓"), a.paint(bold, clean(name)))
			return nil
		},
	}
	passwd := &cobra.Command{
		Use:     "passwd <username>",
		Short:   "Set a new password; ends browser sessions and terminal logins",
		Example: "  burzblog user passwd ada --data /var/lib/burzblog",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := open()
			if err != nil {
				return err
			}
			defer st.Close()
			u, err := st.UserByName(context.Background(), args[0])
			if errors.Is(err, store.ErrNotFound) {
				return fmt.Errorf("there is no account called %s", args[0])
			} else if err != nil {
				return err
			}
			hash, err := newPassword(u.Username)
			if err != nil {
				return err
			}
			if err := st.SetPassword(context.Background(), u.ID, hash, ""); err != nil {
				return err
			}
			fmt.Fprintf(a.Out, "%s Changed the password for %s.\n", a.paint(green, "✓"), clean(u.Username))
			return nil
		},
	}
	list := &cobra.Command{
		Use:   "list",
		Short: "List accounts",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := open()
			if err != nil {
				return err
			}
			defer st.Close()
			users, err := st.Users(context.Background())
			if err != nil {
				return err
			}
			if len(users) == 0 {
				fmt.Fprintln(a.Out, "No accounts yet. Create one with: burzblog user add <name>")
			}
			for _, u := range users {
				fmt.Fprintf(a.Out, "%s  %s\n", clean(u.Username), a.paint(dim, "since "+u.CreatedAt.Local().Format("2 Jan 2006")))
			}
			return nil
		},
	}
	cmd.AddCommand(add, passwd, list)
	return cmd
}
