package cli

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/weburz/burzblog/internal/client"
)

func (a *App) redirectsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "redirects",
		Short: "List the old slugs of renamed published posts and where they go",
		Long: `Renaming a post's slug keeps the old slug as a redirect to the new one. This
lists the redirects of published posts, the same list a website reads from
/api/content/redirects and finds in the export as redirects.json. Remove one in
the browser panel, under the post's slug.`,
		Example: `  burzblog redirects
  burzblog redirects --json     # {"items":[{"from":"old-slug","to":"new-slug"}]}`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, _, err := a.client()
			if err != nil {
				return err
			}
			c, cancel := ctx()
			defer cancel()
			items, err := cl.Redirects(c)
			if err != nil {
				return err
			}
			if a.asJSON {
				if items == nil {
					items = []client.Redirect{}
				}
				return a.emit(map[string]any{"items": items})
			}
			if len(items) == 0 {
				fmt.Fprintln(a.Out, "No redirects. Renaming a published post's slug adds one.")
				return nil
			}
			w := tabwriter.NewWriter(a.Out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, a.paint(dim, "FROM\tTO"))
			for _, rd := range items {
				fmt.Fprintf(w, "%s\t%s\n", clean(rd.From), clean(rd.To))
			}
			return w.Flush()
		},
	}
}
