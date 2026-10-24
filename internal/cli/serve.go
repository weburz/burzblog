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
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the BurzBlog server and browser panel",
		Long: `Runs the API and the browser panel in one process, storing everything in
one SQLite file in the data directory.

Put it behind a TLS proxy (Caddy, nginx) and set --origin to the public
URL. Settings can also come from the environment:

  BURZBLOG_ADDR, BURZBLOG_DATA, BURZBLOG_ORIGIN, BURZBLOG_DEPLOY_HOOK`,
		Example: `  burzblog serve
  burzblog serve --addr 127.0.0.1:8080 --data /var/lib/burzblog --origin https://blog.example.com`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			addr = firstOf(addr, a.Getenv("BURZBLOG_ADDR"), "127.0.0.1:8080")
			origin = firstOf(origin, a.Getenv("BURZBLOG_ORIGIN"), "http://"+addr)
			hook = firstOf(hook, a.Getenv("BURZBLOG_DEPLOY_HOOK"))
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
			srv, err := server.New(server.Config{Origin: origin, DeployHook: hook, Panel: a.Panel, Version: a.Version, Logger: log}, st, svc)
			if err != nil {
				return err
			}
			if users, err := st.Users(ctx); err == nil && len(users) == 0 {
				log.Warn("no accounts yet; create one with: burzblog user add <name> --data " + dir)
			}

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
	cmd.Flags().StringVar(&data, "data", "", "directory for the database (default $STATE_DIRECTORY or ./burzblog-data)")
	cmd.Flags().StringVar(&origin, "origin", "", "public URL people open, e.g. https://blog.example.com (default http://<addr>)")
	cmd.Flags().StringVar(&hook, "deploy-hook", "", "URL to POST after published posts change; prefer BURZBLOG_DEPLOY_HOOK, it is a secret")
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
		Use:     "add <username>",
		Short:   "Create an account",
		Example: "  burzblog user add jarmos --data /var/lib/burzblog",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := store.NormalizeUsername(args[0])
			if name == "" || len(name) > 64 || strings.ContainsAny(name, " \t\r\n") {
				return errors.New("a username is 1 to 64 characters without spaces")
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
			fmt.Fprintf(a.Out, "%s Created %s. They can sign in to the panel or run `burzblog login`.\n", a.paint(green, "✓"), a.paint(bold, name))
			return nil
		},
	}
	passwd := &cobra.Command{
		Use:   "passwd <username>",
		Short: "Set a new password and sign the account out of every browser",
		Args:  cobra.ExactArgs(1),
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
			fmt.Fprintf(a.Out, "%s Changed the password for %s.\n", a.paint(green, "✓"), u.Username)
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
				fmt.Fprintf(a.Out, "%s  %s\n", u.Username, a.paint(dim, "since "+u.CreatedAt.Local().Format("2 Jan 2006")))
			}
			return nil
		},
	}
	cmd.AddCommand(add, passwd, list)
	return cmd
}
