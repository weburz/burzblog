package cli

import (
	"context"
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/weburz/burzblog/internal/store"
)

// auditLine is one audit entry as `burzblog audit --json` prints it, the same
// shape as an item of GET /api/audit.
type auditLine struct {
	ID          int64     `json:"id"`
	At          time.Time `json:"at"`
	ActorUserID int64     `json:"actorId,omitempty"`
	Actor       string    `json:"actor"`
	TokenID     int64     `json:"tokenId,omitempty"`
	Action      string    `json:"action"`
	Target      string    `json:"target"`
	Remote      string    `json:"remote"`
}

func (a *App) auditCmd() *cobra.Command {
	var data string
	var limit, offset int
	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Print the audit log, newest first (run on the host)",
		Long: `Prints who signed in, changed a token, password or role, published, archived or
deleted a post, or deployed, from the server's database. Nothing can edit or
remove an entry. Admins can read the same list in the browser panel.`,
		Example: `  burzblog audit --data /var/lib/burzblog
  burzblog audit --limit 200 --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if limit < 1 || offset < 0 {
				return usagef("the limit must be 1 or more and the offset 0 or more")
			}
			st, err := store.Open(context.Background(), a.dataDir(data))
			if err != nil {
				return err
			}
			defer st.Close()
			entries, total, err := st.AuditLog(context.Background(), limit, offset)
			if err != nil {
				return err
			}
			if a.asJSON {
				lines := make([]auditLine, 0, len(entries))
				for _, e := range entries {
					lines = append(lines, auditLine{ID: e.ID, At: e.At, ActorUserID: e.ActorUserID, Actor: e.ActorName,
						TokenID: e.TokenID, Action: e.Action, Target: e.Target, Remote: e.Remote})
				}
				return a.emit(map[string]any{"items": lines, "total": total})
			}
			if len(entries) == 0 {
				fmt.Fprintln(a.Out, "The audit log is empty.")
				return nil
			}
			w := tabwriter.NewWriter(a.Out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, a.paint(dim, "TIME\tWHO\tACTION\tTARGET\tFROM"))
			for _, e := range entries {
				who := clean(e.ActorName)
				if e.TokenID != 0 {
					who += fmt.Sprintf(" (token %d)", e.TokenID)
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", e.At.Local().Format("2006-01-02 15:04:05"), who,
					clean(e.Action), clean(e.Target), clean(e.Remote))
			}
			if err := w.Flush(); err != nil {
				return err
			}
			if shown := offset + len(entries); shown < total {
				fmt.Fprintf(a.Out, "%s\n", a.paint(dim, fmt.Sprintf("Showing %d of %d. Use --offset %d for older entries.", shown, total, shown)))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&data, "data", "", "data directory (default $BURZBLOG_DATA, $STATE_DIRECTORY or ./burzblog-data)")
	cmd.Flags().IntVar(&limit, "limit", 50, "how many entries to print")
	cmd.Flags().IntVar(&offset, "offset", 0, "how many of the newest entries to skip")
	return cmd
}
