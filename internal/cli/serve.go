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
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/weburz/burzblog/internal/auth"
	"github.com/weburz/burzblog/internal/media"
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

// maxUploadMBLimit caps --max-upload-mb: uploads are held in memory.
const maxUploadMBLimit = 256

func (a *App) serveCmd() *cobra.Command {
	var addr, data, origin, hook string
	var checkUpdates bool
	var maxUploadMB int
	var cfg server.Config // holds the site settings until serving
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the BurzBlog server and browser panel",
		Long: `Runs the API and the browser panel in one process, storing everything in
one SQLite file in the data directory.

Put it behind a TLS proxy (Caddy, nginx) and set --origin to the public
URL. Settings can also come from the environment:

  BURZBLOG_ADDR, BURZBLOG_DATA, BURZBLOG_ORIGIN, BURZBLOG_DEPLOY_HOOK

BurzBlog contacts nobody unless you ask. --check-updates (or
BURZBLOG_CHECK_UPDATES=true) lets it look for a newer release once a day with a
GET to api.github.com; admins see the result in Settings.

Uploaded images and PDFs are kept in the media folder of the data directory,
at most --max-upload-mb each (BURZBLOG_MAX_UPLOAD_MB, default 10).

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
			if !cmd.Flags().Changed("check-updates") {
				switch v := a.Getenv("BURZBLOG_CHECK_UPDATES"); v {
				case "":
				case "true", "1":
					checkUpdates = true
				case "false", "0":
				default:
					return fmt.Errorf("BURZBLOG_CHECK_UPDATES must be true or false, got %q", v)
				}
			}
			cfg.CheckUpdates = checkUpdates
			dir := a.dataDir(data)
			if maxUploadMB == 0 {
				if v := a.Getenv("BURZBLOG_MAX_UPLOAD_MB"); v != "" {
					n, err := strconv.Atoi(v)
					if err != nil {
						return fmt.Errorf("BURZBLOG_MAX_UPLOAD_MB must be a whole number of MiB, got %q", v)
					}
					maxUploadMB = n
				}
			}
			if maxUploadMB < 0 || maxUploadMB > maxUploadMBLimit {
				return fmt.Errorf("the upload limit must be 1 to %d MiB, got %d", maxUploadMBLimit, maxUploadMB)
			}
			if maxUploadMB > 0 {
				cfg.MaxUpload = int64(maxUploadMB) << 20
			}

			log := slog.New(slog.NewTextHandler(a.Err, &slog.HandlerOptions{Level: slog.LevelInfo}))
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			// Holds the shared lock, so a restore cannot run under the server.
			st, err := store.OpenShared(ctx, dir)
			if err != nil {
				return err
			}
			defer st.Close()
			svc, err := auth.New(st)
			if err != nil {
				return err
			}
			if cfg.Media, err = media.NewDisk(filepath.Join(dir, media.DirName)); err != nil {
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
			srv.StartUpdateChecks(ctx)

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
	cmd.Flags().IntVar(&maxUploadMB, "max-upload-mb", 0, "largest upload in MiB (default 10)")
	cmd.Flags().StringVar(&origin, "origin", "", "public URL people open, e.g. https://blog.example.com (default http://<addr>)")
	cmd.Flags().StringVar(&hook, "deploy-hook", "", "URL to POST after published posts change; prefer BURZBLOG_DEPLOY_HOOK, it is a secret")
	cmd.Flags().BoolVar(&checkUpdates, "check-updates", false, "look for a newer release on GitHub once a day (default off; sends nothing but a GET with the version); or BURZBLOG_CHECK_UPDATES=true")
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

// hostAudit records a change made from the command line on the server.
func hostAudit(st *store.Store, action, target string) error {
	err := st.AddAudit(context.Background(), store.Audit{ActorName: "host", Action: action, Target: target, Remote: "local"})
	if err != nil {
		return fmt.Errorf("the change was made but could not be recorded in the audit log: %w", err)
	}
	return nil
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
		return store.OpenShared(context.Background(), a.dataDir(data))
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

	var role string
	add := &cobra.Command{
		Use:   "add <username>",
		Short: "Create an account (the first is an admin, later ones authors)",
		Long: `Creates an account and asks for its password, at least 12 characters. Without a
terminal the password is read from standard input.

An author writes and publishes posts. An admin can also delete posts, manage
every token, deploy, download backups and read the audit log. Change a role
later with: burzblog user role <username> admin|author

The first account is always an admin, so a new server is never left without
one. Later accounts are authors unless given --role admin.`,
		Example: `  burzblog user add ada --data /var/lib/burzblog
  burzblog user add grace --role admin --data /var/lib/burzblog`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := store.NormalizeUsername(args[0])
			if err := auth.CheckUsername(name); err != nil {
				return err
			}
			if role != "" && !store.ValidRole(role) {
				return usagef("the role must be %s or %s, not %q", store.RoleAuthor, store.RoleAdmin, role)
			}
			st, err := open()
			if err != nil {
				return err
			}
			defer st.Close()
			exists, err := st.HasUsers(context.Background())
			if err != nil {
				return err
			}
			switch {
			case !exists && role == store.RoleAuthor:
				return usagef("the first account must be an admin; leave out --role, or use --role admin")
			case !exists:
				role = store.RoleAdmin
			case role == "":
				role = store.RoleAuthor
			}
			hash, err := newPassword(name)
			if err != nil {
				return err
			}
			u, err := st.CreateUser(context.Background(), name, hash, role)
			if errors.Is(err, store.ErrTaken) {
				return fmt.Errorf("%s already exists; use `burzblog user passwd %s` to change the password", name, name)
			} else if err != nil {
				return err
			}
			if err := hostAudit(st, "account.create", u.Username+" as "+u.Role); err != nil {
				return err
			}
			fmt.Fprintf(a.Out, "%s Created %s as %s. They can sign in to the panel or run `burzblog login`.\n", a.paint(green, "✓"), a.paint(bold, clean(name)), u.Role)
			return nil
		},
	}
	add.Flags().StringVar(&role, "role", "", "role of the new account: author or admin (default: admin for the first account, author after)")
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
			if err := hostAudit(st, "password.change", u.Username); err != nil {
				return err
			}
			fmt.Fprintf(a.Out, "%s Changed the password for %s.\n", a.paint(green, "✓"), clean(u.Username))
			return nil
		},
	}
	reset2FA := &cobra.Command{
		Use:   "2fa-reset <username>",
		Short: "Turn off two-factor sign-in for a locked-out account; ends browser sessions",
		Long: `Turns off two-factor sign-in and deletes the recovery codes, for someone who lost
their authenticator app and recovery codes. Their browser sessions end and they
can sign in with the password alone, then set two-factor up again in Settings.
Terminal logins (tokens) keep working.`,
		Example: "  burzblog user 2fa-reset ada --data /var/lib/burzblog",
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
			if err := st.DisableTOTP(context.Background(), u.ID, ""); errors.Is(err, store.ErrNotFound) {
				return fmt.Errorf("%s does not use two-factor sign-in", clean(u.Username))
			} else if err != nil {
				return err
			}
			if err := hostAudit(st, "2fa.disable", u.Username); err != nil {
				return err
			}
			fmt.Fprintf(a.Out, "%s Turned off two-factor sign-in for %s and ended their browser sessions.\n", a.paint(green, "✓"), clean(u.Username))
			return nil
		},
	}
	setRole := &cobra.Command{
		Use:   "role <username> <admin|author>",
		Short: "Make an account an admin or an author",
		Long: `Changes what an account may do. The change applies to its browser sessions and
tokens at once. The last admin cannot be made an author, so someone can always
manage the server.`,
		Example: "  burzblog user role ada admin --data /var/lib/burzblog",
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !store.ValidRole(args[1]) {
				return usagef("the role must be %s or %s, not %q", store.RoleAdmin, store.RoleAuthor, args[1])
			}
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
			previous, err := st.SetRole(context.Background(), u.ID, args[1])
			if errors.Is(err, store.ErrLastAdmin) {
				return fmt.Errorf("%s is the last admin; make another account an admin first (burzblog user role <username> admin)", clean(u.Username))
			} else if err != nil {
				return err
			}
			if previous == args[1] {
				fmt.Fprintf(a.Out, "%s is already %s; nothing changed.\n", clean(u.Username), previous)
				return nil
			}
			if err := hostAudit(st, "role.change", u.Username+": "+previous+" to "+args[1]); err != nil {
				return err
			}
			fmt.Fprintf(a.Out, "%s %s is now %s (was %s).\n", a.paint(green, "✓"), clean(u.Username), args[1], previous)
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
				fmt.Fprintf(a.Out, "%s  %s  %s\n", clean(u.Username), u.Role, a.paint(dim, "since "+u.CreatedAt.Local().Format("2 Jan 2006")))
			}
			return nil
		},
	}
	cmd.AddCommand(add, passwd, reset2FA, setRole, list)
	return cmd
}
