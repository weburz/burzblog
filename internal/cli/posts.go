package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/weburz/burzblog/internal/client"
	"github.com/weburz/burzblog/internal/post"
)

func (a *App) listCmd() *cobra.Command {
	var status, search string
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List posts, most recently changed first",
		Example: `  burzblog list
  burzblog list --status published
  burzblog list --search golang`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, _, err := a.client()
			if err != nil {
				return err
			}
			c, cancel := ctx()
			defer cancel()
			l, err := cl.Posts(c, status, search)
			if err != nil {
				return err
			}
			if len(l.Items) == 0 {
				if search != "" || status != "" {
					fmt.Fprintln(a.Out, "No posts match.")
				} else {
					fmt.Fprintln(a.Out, "No posts yet. Start one with: burzblog new \"My first post\"")
				}
				return nil
			}
			w := tabwriter.NewWriter(a.Out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, a.paint(dim, "STATUS\tSLUG\tTITLE\tWORDS\tCHANGED"))
			for _, p := range l.Items {
				fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\n", a.status(p.Status), p.Slug, truncate(p.Title, 48), p.Words,
					ago(p.UpdatedAt)+" by "+p.UpdatedBy)
			}
			w.Flush()
			fmt.Fprintf(a.Out, "\n%s\n", a.paint(dim, fmt.Sprintf("%d drafts, %d published, %d archived",
				l.Counts["draft"], l.Counts["published"], l.Counts["archived"])))
			return nil
		},
	}
	cmd.Flags().StringVarP(&status, "status", "s", "", "only draft, published or archived posts")
	cmd.Flags().StringVarP(&search, "search", "q", "", "match title, slug or description")
	return cmd
}

func (a *App) status(s string) string {
	switch s {
	case "published":
		return a.paint(green, "published")
	case "draft":
		return a.paint(yellow, "draft    ")
	}
	return a.paint(dim, s)
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func (a *App) newCmd() *cobra.Command {
	var slug string
	var noEdit bool
	cmd := &cobra.Command{
		Use:   "new <title>",
		Short: "Start a new draft and open it in your editor",
		Example: `  burzblog new "Understanding Go interfaces"
  burzblog new "Release notes" --slug release-notes-2026 --no-edit`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, cfg, err := a.client()
			if err != nil {
				return err
			}
			title := strings.Join(args, " ")
			c, cancel := ctx()
			defer cancel()
			p, err := cl.CreatePost(c, title, slug)
			if err != nil {
				return err
			}
			fmt.Fprintf(a.Out, "%s Created draft %s.\n", a.paint(green, "✓"), a.paint(bold, p.Slug))
			if noEdit {
				return nil
			}
			return a.edit(cl, cfg, p.Slug)
		},
	}
	cmd.Flags().StringVar(&slug, "slug", "", "URL name for the post (default: made from the title)")
	cmd.Flags().BoolVar(&noEdit, "no-edit", false, "create the draft without opening an editor")
	return cmd
}

func (a *App) editCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "edit <slug>",
		Short: "Open a post in your editor and save it back when you close it",
		Long: `Downloads the post as a Markdown file with frontmatter, opens it in
$VISUAL or $EDITOR, and saves it back when the editor exits.

If someone else saved the post while you were editing, nothing is
overwritten: your text stays on disk and you are told how to continue.`,
		Example: "  burzblog edit understanding-go-interfaces\n  EDITOR=\"code --wait\" burzblog edit my-post",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, cfg, err := a.client()
			if err != nil {
				return err
			}
			return a.edit(cl, cfg, args[0])
		},
	}
}

// draft is a post being edited locally, kept until it is saved.
type draft struct {
	Server  string    `json:"server"`
	Slug    string    `json:"slug"`
	Version int64     `json:"version"`
	Started time.Time `json:"started"`
}

func (a *App) draftPaths(cfg *Config, slug string) (doc, meta string) {
	host := "server"
	if u, err := url.Parse(cfg.Server); err == nil && u.Host != "" {
		host = strings.NewReplacer(":", "_", "/", "_").Replace(u.Host)
	}
	dir := filepath.Join(a.CacheDir, "drafts", host)
	return filepath.Join(dir, slug+".md"), filepath.Join(dir, slug+".json")
}

func (a *App) edit(cl *client.Client, cfg *Config, slug string) error {
	if err := post.ValidateSlug(slug); err != nil {
		return fmt.Errorf("%q is not a post slug; run `burzblog list` to see them", slug)
	}
	docPath, metaPath := a.draftPaths(cfg, slug)

	var original string
	var version int64
	resumed := false
	if raw, err := os.ReadFile(metaPath); err == nil {
		var d draft
		if json.Unmarshal(raw, &d) == nil && d.Server == cfg.Server {
			if _, err := os.Stat(docPath); err == nil {
				fmt.Fprintf(a.Err, "%s You have unsaved changes to %s from %s (%s).\n", a.paint(yellow, "!"), slug, ago(d.Started), docPath)
				if a.confirm("Keep editing them?", true) {
					version, resumed = d.Version, true
				}
			}
		}
	}
	if !resumed {
		// Each request gets its own deadline: time spent in the editor must
		// not count against the save.
		c, cancel := ctx()
		doc, v, err := cl.Markdown(c, slug)
		cancel()
		if err != nil {
			return err
		}
		original, version = doc, v
		if err := os.MkdirAll(filepath.Dir(docPath), 0o700); err != nil {
			return err
		}
		if err := writeFileAtomic(docPath, []byte(doc), 0o600); err != nil {
			return err
		}
		meta, _ := json.Marshal(draft{Server: cfg.Server, Slug: slug, Version: v, Started: time.Now()})
		if err := writeFileAtomic(metaPath, meta, 0o600); err != nil {
			return err
		}
	}

	for {
		if err := a.runEditor(docPath); err != nil {
			return fmt.Errorf("%w\n       Your text is kept at %s", err, docPath)
		}
		edited, err := os.ReadFile(docPath)
		if err != nil {
			return err
		}
		if !resumed && string(edited) == original {
			os.Remove(docPath)
			os.Remove(metaPath)
			fmt.Fprintln(a.Out, "No changes.")
			return nil
		}
		c, cancel := ctx()
		saved, err := cl.SaveMarkdown(c, slug, string(edited), version)
		cancel()
		if err == nil {
			os.Remove(docPath)
			os.Remove(metaPath)
			if saved.Version == version {
				fmt.Fprintln(a.Out, "No changes.")
			} else {
				fmt.Fprintf(a.Out, "%s Saved %s (version %d, %s).\n", a.paint(green, "✓"), a.paint(bold, saved.Slug), saved.Version, saved.Status)
				if saved.Status == "draft" {
					fmt.Fprintf(a.Out, "  Publish it with: burzblog publish %s\n", saved.Slug)
				}
			}
			return nil
		}
		var apiErr *client.Error
		// Without a terminal nobody can answer, and reopening would loop.
		if errors.As(err, &apiErr) && apiErr.Code == "validation" && a.terminal() {
			a.failure(err)
			if a.confirm("Reopen the editor to fix it?", true) {
				continue
			}
		}
		if errors.As(err, &apiErr) && apiErr.Code == "conflict" {
			who := "someone"
			if apiErr.Current != nil {
				who = apiErr.Current.UpdatedBy
			}
			return fmt.Errorf("%s saved %s while you were editing, so nothing was overwritten.\n"+
				"       Your text is kept at %s\n"+
				"       Compare it with `burzblog pull %s -o -`, then either:\n"+
				"         burzblog push %s --force   (replace their version with yours)\n"+
				"         burzblog edit %s           (keep editing your copy)",
				who, slug, docPath, slug, docPath, slug)
		}
		// %v, not %w: the hint below must survive the API error formatting.
		return fmt.Errorf("%v\n       Your text is kept at %s; run `burzblog edit %s` to try again", err, docPath, slug)
	}
}

func (a *App) runEditor(path string) error {
	editor := a.Getenv("VISUAL")
	if editor == "" {
		editor = a.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
		if runtime.GOOS == "windows" {
			editor = "notepad"
		}
	}
	parts := strings.Fields(editor)
	cmd := exec.Command(parts[0], append(parts[1:], path)...)
	if f, ok := a.In.(*os.File); ok {
		cmd.Stdin = f
	}
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if f, ok := a.Out.(*os.File); ok {
		cmd.Stdout = f
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("the editor %q did not finish cleanly: %v", editor, err)
	}
	return nil
}

// Base versions remember which server version each pulled file came from,
// keyed by server and the file's absolute path, so push can refuse to
// overwrite newer work even if the post was edited elsewhere meanwhile.
func (a *App) basePath() string {
	return filepath.Join(a.CacheDir, "base.json")
}

func (a *App) bases() map[string]int64 {
	m := map[string]int64{}
	if raw, err := os.ReadFile(a.basePath()); err == nil {
		_ = json.Unmarshal(raw, &m)
	}
	return m
}

func baseKey(server, file string) string {
	if abs, err := filepath.Abs(file); err == nil {
		file = abs
	}
	return server + " " + file
}

func (a *App) rememberBase(server, file string, version int64) {
	m := a.bases()
	m[baseKey(server, file)] = version
	raw, _ := json.Marshal(m)
	if os.MkdirAll(a.CacheDir, 0o700) == nil {
		_ = writeFileAtomic(a.basePath(), raw, 0o600)
	}
}

func (a *App) pullCmd() *cobra.Command {
	var out string
	var force bool
	cmd := &cobra.Command{
		Use:   "pull <slug>",
		Short: "Download a post as <slug>.md to work on it with your own tools",
		Example: `  burzblog pull my-post            # writes ./my-post.md
  burzblog pull my-post -o -       # prints it
  burzblog pull my-post -o drafts/my-post.md`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			slug := args[0]
			cl, cfg, err := a.client()
			if err != nil {
				return err
			}
			c, cancel := ctx()
			defer cancel()
			doc, version, err := cl.Markdown(c, slug)
			if err != nil {
				return err
			}
			if out == "-" {
				_, err := io.WriteString(a.Out, doc)
				return err
			}
			if out == "" {
				out = slug + ".md"
			}
			if filepath.Base(out) != slug+".md" {
				return fmt.Errorf("name the file %s.md so `burzblog push` knows which post it is", slug)
			}
			if _, err := os.Lstat(out); err == nil && !force {
				return fmt.Errorf("%s already exists; pass --force to replace it", out)
			}
			if err := writeFileAtomic(out, []byte(doc), 0o644); err != nil {
				return err
			}
			a.rememberBase(cfg.Server, out, version)
			fmt.Fprintf(a.Out, "%s Wrote %s (version %d). Send it back with: burzblog push %s\n", a.paint(green, "✓"), out, version, out)
			return nil
		},
	}
	cmd.Flags().StringVarP(&out, "output", "o", "", "file to write, or - for standard output")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "replace an existing file")
	return cmd
}

func (a *App) pushCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "push <file.md>",
		Short: "Save a local <slug>.md back to the server (creates the post if it is new)",
		Long: `Sends a Markdown file with frontmatter back to the server. The file name
is the post's slug.

If the post changed on the server since you pulled it, push refuses; use
--force to replace the server's version with yours.`,
		Example: "  burzblog push my-post.md\n  burzblog push drafts/new-idea.md",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := args[0]
			slug := strings.TrimSuffix(filepath.Base(path), ".md")
			if !strings.HasSuffix(path, ".md") || post.ValidateSlug(slug) != nil {
				return fmt.Errorf("%s: name the file <slug>.md using lowercase letters, digits and hyphens", path)
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			cl, cfg, err := a.client()
			if err != nil {
				return err
			}
			c, cancel := ctx()
			defer cancel()
			version, known := a.bases()[baseKey(cfg.Server, path)]
			if !known || force {
				current, err := cl.Post(c, slug)
				switch {
				case client.IsCode(err, "not_found"):
					version = 0
				case err != nil:
					return err
				case force:
					version = current.Version
				default:
					return fmt.Errorf("%s already exists on the server and this file was not pulled from it.\n"+
						"       Pull it first (`burzblog pull %s -o other.md`) to compare, or pass --force to replace it", slug, slug)
				}
			}
			saved, err := cl.SaveMarkdown(c, slug, string(raw), version)
			if client.IsCode(err, "conflict") {
				return fmt.Errorf("%s changed on the server since you pulled it, so nothing was overwritten.\n"+
					"       Compare with `burzblog pull %s -o -`, then push again with --force to replace it", slug, slug)
			}
			if err != nil {
				return err
			}
			a.rememberBase(cfg.Server, path, saved.Version)
			verb := "Saved"
			if version == 0 {
				verb = "Created"
			}
			fmt.Fprintf(a.Out, "%s %s %s (version %d, %s).\n", a.paint(green, "✓"), verb, a.paint(bold, slug), saved.Version, saved.Status)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "replace the server's version even if it changed")
	return cmd
}

func (a *App) statusCmd(action, short string) *cobra.Command {
	return &cobra.Command{
		Use:   action + " <slug>",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, _, err := a.client()
			if err != nil {
				return err
			}
			c, cancel := ctx()
			defer cancel()
			current, err := cl.Post(c, args[0])
			if err != nil {
				return err
			}
			p, err := cl.Action(c, args[0], action, current.Version)
			if err != nil {
				return err
			}
			fmt.Fprintf(a.Out, "%s %s is now %s.\n", a.paint(green, "✓"), a.paint(bold, p.Slug), p.Status)
			if action == "publish" || action == "unpublish" {
				fmt.Fprintln(a.Out, "  Reader sites with a deploy hook rebuild in a few seconds.")
			}
			return nil
		},
	}
}

func (a *App) openCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "open [slug]",
		Short: "Open the browser panel, or one post in it",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := a.loadConfig()
			if err != nil {
				return err
			}
			if cfg.Server == "" {
				return errors.New("not signed in. Run `burzblog login https://your-blog.example.com` first")
			}
			target := cfg.Server + "/"
			if len(args) == 1 {
				target = cfg.Server + "/posts/" + url.PathEscape(args[0])
			}
			var opener *exec.Cmd
			switch runtime.GOOS {
			case "darwin":
				opener = exec.Command("open", target)
			case "windows":
				opener = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
			default:
				opener = exec.Command("xdg-open", target)
			}
			if err := opener.Start(); err != nil {
				fmt.Fprintln(a.Out, target)
			}
			return nil
		},
	}
}

func (a *App) exportCmd() *cobra.Command {
	var out string
	var clean bool
	cmd := &cobra.Command{
		Use:   "export --out <dir>",
		Short: "Download every published post as <slug>.md files (for reader site builds)",
		Long: `Writes each published post to <dir>/<slug>.md with frontmatter, ready for
Nuxt Content or another static site generator.

In CI, set BURZBLOG_SERVER and BURZBLOG_TOKEN (an export token from
Settings) instead of running burzblog login.`,
		Example: `  burzblog export --out website/content/blogs --clean
  BURZBLOG_SERVER=https://blog.example.com BURZBLOG_TOKEN=bzb_... burzblog export --out content`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if out == "" {
				return errors.New("choose where to write the posts with --out <dir>")
			}
			cl, _, err := a.client()
			if err != nil {
				return err
			}
			c, cancel := ctx()
			defer cancel()
			body, err := cl.Export(c)
			if err != nil {
				return err
			}
			defer body.Close()
			files, err := readExport(body)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(out, 0o755); err != nil {
				return err
			}
			removed := 0
			if clean {
				existing, _ := filepath.Glob(filepath.Join(out, "*.md"))
				for _, f := range existing {
					if _, keep := files[filepath.Base(f)]; !keep {
						if err := os.Remove(f); err != nil {
							return err
						}
						removed++
					}
				}
			}
			for name, data := range files {
				if err := writeFileAtomic(filepath.Join(out, name), data, 0o644); err != nil {
					return err
				}
			}
			fmt.Fprintf(a.Out, "%s Exported %d published posts to %s", a.paint(green, "✓"), len(files), out)
			if removed > 0 {
				fmt.Fprintf(a.Out, " (removed %d no longer published)", removed)
			}
			fmt.Fprintln(a.Out, ".")
			return nil
		},
	}
	cmd.Flags().StringVarP(&out, "out", "o", "", "directory to write <slug>.md files into")
	cmd.Flags().BoolVar(&clean, "clean", false, "delete other .md files in the directory (posts no longer published)")
	return cmd
}

// readExport reads the export archive fully before anything is written, and
// accepts only flat <slug>.md regular files.
func readExport(r io.Reader) (map[string][]byte, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("the export is not a gzip archive: %w", err)
	}
	tr := tar.NewReader(gz)
	files := map[string][]byte{}
	total := 0
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read export: %w", err)
		}
		slug := strings.TrimSuffix(hdr.Name, ".md")
		if hdr.Typeflag != tar.TypeReg || !strings.HasSuffix(hdr.Name, ".md") || post.ValidateSlug(slug) != nil {
			return nil, fmt.Errorf("the export contains an unexpected entry %q", hdr.Name)
		}
		var buf bytes.Buffer
		n, err := io.Copy(&buf, io.LimitReader(tr, post.MaxBody+post.MaxExtra+1))
		if err != nil {
			return nil, fmt.Errorf("read export: %w", err)
		}
		total += int(n)
		if n > post.MaxBody+post.MaxExtra || total > 512<<20 {
			return nil, errors.New("the export is larger than expected")
		}
		files[hdr.Name] = buf.Bytes()
	}
	return files, nil
}
