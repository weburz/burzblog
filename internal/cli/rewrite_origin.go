package cli

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/weburz/burzblog/internal/media"
	"github.com/weburz/burzblog/internal/post"
	"github.com/weburz/burzblog/internal/server"
	"github.com/weburz/burzblog/internal/store"
)

// Match a whole URL, so a different site's redirect or a longer filename
// containing the old address cannot be mistaken for a local upload.
var bodyURL = regexp.MustCompile("(?i)https?://[^[:space:]<>()\\[\\]\"'`]+")

func rewriteMediaURL(raw string, old, next *url.URL) string {
	link := strings.TrimRight(raw, ".,;:!")
	u, err := url.Parse(link)
	if err != nil || u.User != nil || !strings.EqualFold(u.Scheme, old.Scheme) || !strings.EqualFold(u.Host, old.Host) {
		return raw
	}
	name, ok := strings.CutPrefix(u.EscapedPath(), "/media/")
	if !ok || !media.ValidName(name) {
		return raw
	}
	u.Scheme, u.Host = next.Scheme, next.Host
	return u.String() + raw[len(link):]
}

func (a *App) rewriteOriginCmd() *cobra.Command {
	var data string
	var dry bool
	cmd := &cobra.Command{
		Use: "rewrite-origin OLD NEW", Short: "Move post body and cover upload URLs to a new BurzBlog origin",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			old, err := server.ParseOrigin(args[0])
			if err != nil || old.Fragment != "" || old.ForceQuery {
				return usagef("OLD must be an HTTP or HTTPS origin without a path, query or fragment")
			}
			next, err := server.ParseOrigin(args[1])
			if err != nil || next.Fragment != "" || next.ForceQuery {
				return usagef("NEW must be an HTTP or HTTPS origin without a path, query or fragment")
			}
			ctx := cmd.Context()
			st, err := store.OpenExisting(ctx, a.dataDir(data))
			if err != nil {
				return err
			}
			defer st.Close()
			rewrite := func(raw string) string { return rewriteMediaURL(raw, old, next) }
			changed := 0
			err = st.ForEachPost(ctx, func(p *post.Post) error {
				body := bodyURL.ReplaceAllStringFunc(p.Body, rewrite)
				cover := rewrite(p.CoverURL)
				if body == p.Body && cover == p.CoverURL {
					return nil
				}
				if !dry {
					_, err := st.UpdatePost(ctx, p.Slug, p.Version, "host", func(current *post.Post) error {
						current.Body, current.CoverURL = body, cover
						return nil
					})
					if err != nil {
						return fmt.Errorf("update %s: %w", p.Slug, err)
					}
					if err := hostAudit(st, "post.update", p.Slug); err != nil {
						return err
					}
				}
				changed++
				return nil
			})
			if err != nil {
				return err
			}
			if a.asJSON {
				return a.emit(map[string]any{"posts": changed, "dryRun": dry})
			}
			if dry {
				fmt.Fprintf(a.Out, "Would update %s.\n", count(changed, "post", "posts"))
			} else {
				fmt.Fprintf(a.Out, "Updated %s. Rebuild your website to publish the new URLs.\n", count(changed, "post", "posts"))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&data, "data", "", "BurzBlog data directory")
	cmd.Flags().BoolVar(&dry, "dry-run", false, "report how many posts would change")
	return cmd
}
