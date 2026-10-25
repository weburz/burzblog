package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
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
	var status, search, sort, tag string
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List posts, most recently changed first unless --sort says otherwise",
		Example: `  burzblog list
  burzblog list --status published
  burzblog list --status scheduled
  burzblog list --search golang
  burzblog list --sort title
  burzblog list --tag golang
  burzblog list --status draft --json     # {"items":[{"slug","title","status","version",...}],"counts":{...}}`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, _, err := a.client()
			if err != nil {
				return err
			}
			c, cancel := ctx()
			defer cancel()
			l, err := cl.Posts(c, status, search, sort, tag)
			if err != nil {
				return err
			}
			if a.asJSON {
				if l.Items == nil {
					l.Items = []client.Post{}
				}
				if l.Counts == nil {
					l.Counts = map[string]int{}
				}
				return a.emit(l)
			}
			if len(l.Items) == 0 {
				if search != "" || status != "" || tag != "" {
					fmt.Fprintln(a.Out, "No posts match.")
				} else {
					fmt.Fprintln(a.Out, "No posts yet. Start one with: burzblog new \"My first post\"")
				}
				return nil
			}
			w := tabwriter.NewWriter(a.Out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, a.paint(dim, "STATUS\tSLUG\tTITLE\tTAGS\tWORDS\tCHANGED"))
			for _, p := range l.Items {
				label := a.status(p.Status)
				if p.PublishAt != nil && p.Status == "draft" {
					label = a.paint(yellow, "scheduled "+p.PublishAt.Local().Format("2 Jan 15:04"))
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%s\n", label, clean(p.Slug), truncate(clean(p.Title), 48),
					truncate(clean(strings.Join(p.Tags, ",")), 30), p.Words,
					ago(p.UpdatedAt)+" by "+clean(p.UpdatedBy))
			}
			w.Flush()
			fmt.Fprintf(a.Out, "\n%s\n", a.paint(dim, fmt.Sprintf("%s (%d scheduled), %d published, %d archived",
				count(l.Counts["draft"], "draft", "drafts"), l.Counts["scheduled"], l.Counts["published"], l.Counts["archived"])))
			return nil
		},
	}
	cmd.Flags().StringVarP(&status, "status", "s", "", "only draft, published, scheduled or archived posts")
	cmd.Flags().StringVarP(&search, "search", "q", "", "match title, slug or description")
	cmd.Flags().StringVar(&sort, "sort", "", "order by updated (default), published, title or created")
	cmd.Flags().StringVarP(&tag, "tag", "t", "", "only posts with this tag")
	return cmd
}

func (a *App) status(s string) string {
	switch s {
	case "published":
		return a.paint(green, "published")
	case "draft":
		return a.paint(yellow, "draft    ")
	}
	return a.paint(dim, clean(s))
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// postResult is what --json prints after a command changed a post: the
// post's fields without its body. The API does not count words for a single
// post, so there is no words field; list has one.
type postResult struct {
	Slug        string     `json:"slug"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Status      string     `json:"status"`
	PublishedOn *time.Time `json:"publishedOn"`
	PublishAt   *time.Time `json:"publishAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
	UpdatedBy   string     `json:"updatedBy"`
	Version     int64      `json:"version"`
	Tags        []string   `json:"tags"`
	Author      string     `json:"author"`
}

func summary(p *client.Post) postResult {
	tags := p.Tags
	if tags == nil {
		tags = []string{}
	}
	return postResult{p.Slug, p.Title, p.Description, p.Status, p.PublishedOn, p.PublishAt, p.UpdatedAt, p.UpdatedBy, p.Version,
		tags, p.Author}
}

// readInput reads a file, or standard input for "-", up to the largest
// document the server accepts.
func (a *App) readInput(path string) ([]byte, error) {
	if path != "-" {
		return os.ReadFile(path)
	}
	raw, err := io.ReadAll(io.LimitReader(a.In, post.MaxBody+post.MaxExtra+1))
	if err != nil {
		return nil, fmt.Errorf("read standard input: %w", err)
	}
	if len(raw) > post.MaxBody+post.MaxExtra {
		return nil, &exitError{"validation", ExitValidation, "the input on standard input is larger than a post may be"}
	}
	return raw, nil
}

func (a *App) newCmd() *cobra.Command {
	var slug, bodyFile string
	var noEdit bool
	cmd := &cobra.Command{
		Use:   "new <title>",
		Short: "Start a new draft and open it in your editor",
		Long: `Creates a draft and opens it in your editor.

The editor opens only when standard input is a terminal and neither --no-edit,
--body-file nor --json is given; otherwise the draft is created and its slug is
printed. --body-file takes the post's Markdown body (no frontmatter) from a file
or, with -, from standard input.`,
		Example: `  burzblog new "Understanding Go interfaces"
  burzblog new "Release notes" --slug release-notes-2026 --no-edit
  burzblog new "Release notes" --body-file notes.md
  cat notes.md | burzblog new "Release notes" --body-file - --json`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var body string
			if bodyFile != "" {
				raw, err := a.readInput(bodyFile)
				if err != nil {
					return err
				}
				if len(raw) > post.MaxBody {
					return &exitError{"validation", ExitValidation, fmt.Sprintf("the body is larger than the %d MiB a post may have", post.MaxBody>>20)}
				}
				body = string(raw)
			}
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
			if body != "" {
				doc := post.Encode(&post.Post{Title: p.Title, Status: post.Draft, Body: body, Author: p.Author})
				saved, err := cl.SaveMarkdown(c, p.Slug, doc, p.Version)
				if err != nil {
					return reword(err, fmt.Sprintf("%v\n       The draft %s was created, but the body was not saved. Save it with `burzblog push - --slug %s --base-version %d`.",
						err, p.Slug, p.Slug, p.Version))
				}
				p = saved
			}
			if a.asJSON {
				return a.emit(summary(p))
			}
			fmt.Fprintf(a.Out, "%s Created draft %s.\n", a.paint(green, "✓"), a.paint(bold, clean(p.Slug)))
			if noEdit || bodyFile != "" {
				return nil
			}
			if !a.terminal() {
				fmt.Fprintf(a.Err, "Not opening an editor because standard input is not a terminal. Edit it with: burzblog edit %s\n", clean(p.Slug))
				return nil
			}
			return a.edit(cl, cfg, p.Slug)
		},
	}
	cmd.Flags().StringVar(&slug, "slug", "", "URL name for the post (default: made from the title)")
	cmd.Flags().BoolVar(&noEdit, "no-edit", false, "create the draft without opening an editor")
	cmd.Flags().StringVar(&bodyFile, "body-file", "", "Markdown body for the draft from a file, or - for standard input (implies --no-edit)")
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
			// Never fall back to vi when nobody can drive it.
			if !a.terminal() && a.Getenv("VISUAL") == "" && a.Getenv("EDITOR") == "" {
				return usagef("edit needs a terminal or $EDITOR. In scripts, use `burzblog pull <slug>` and `burzblog push` instead")
			}
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
				fmt.Fprintf(a.Out, "%s Saved %s (version %d, %s).\n", a.paint(green, "✓"), a.paint(bold, clean(saved.Slug)), saved.Version, clean(saved.Status))
				if saved.Status == "draft" {
					fmt.Fprintf(a.Out, "  Publish it with: burzblog publish %s\n", clean(saved.Slug))
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
				who = clean(apiErr.Current.UpdatedBy)
			}
			return conflictf("%s saved %s while you were editing, so nothing was overwritten.\n"+
				"       Your text is kept at %s\n"+
				"       Compare it with `burzblog pull %s -o -`, then either:\n"+
				"         burzblog push %s --force   (replace their version with yours)\n"+
				"         burzblog edit %s           (keep editing your copy)",
				who, slug, docPath, slug, docPath, slug)
		}
		// %v, not %w: the hint below must survive the API error formatting.
		return reword(err, fmt.Sprintf("%v\n       Your text is kept at %s; run `burzblog edit %s` to try again", err, docPath, slug))
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

// baseHashPath holds the SHA-256 of each file as pulled or pushed, under the
// same keys, so `burzblog status` can tell whether the file was edited since.
func (a *App) baseHashPath() string {
	return filepath.Join(a.CacheDir, "base-content.json")
}

func (a *App) baseHashes() map[string]string {
	m := map[string]string{}
	if raw, err := os.ReadFile(a.baseHashPath()); err == nil {
		_ = json.Unmarshal(raw, &m)
	}
	return m
}

func contentHash(doc []byte) string {
	sum := sha256.Sum256(doc)
	return hex.EncodeToString(sum[:])
}

// rememberBase records the version a file came from and what it held then.
func (a *App) rememberBase(server, file string, version int64, doc []byte) {
	key := baseKey(server, file)
	m := a.bases()
	m[key] = version
	hashes := a.baseHashes()
	hashes[key] = contentHash(doc)
	if os.MkdirAll(a.CacheDir, 0o700) == nil {
		raw, _ := json.Marshal(m)
		_ = writeFileAtomic(a.basePath(), raw, 0o600)
		raw, _ = json.Marshal(hashes)
		_ = writeFileAtomic(a.baseHashPath(), raw, 0o600)
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
  burzblog pull my-post -o drafts/my-post.md
  burzblog pull my-post --json        # writes ./my-post.md, prints {"slug","version","path"}
  burzblog pull my-post -o - --json   # prints {"slug","version","markdown"}`,
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
			if out == "-" && a.asJSON {
				return a.emit(struct {
					Slug     string `json:"slug"`
					Version  int64  `json:"version"`
					Markdown string `json:"markdown"`
				}{slug, version, doc})
			}
			if out == "-" {
				// Piped output stays byte for byte; a terminal must not
				// act on control characters inside the post.
				if a.outTerminal() {
					doc = cleanLines(strings.ReplaceAll(doc, "\t", "    "))
				}
				_, err := io.WriteString(a.Out, doc)
				return err
			}
			if out == "" {
				out = slug + ".md"
			}
			if filepath.Base(out) != slug+".md" {
				return usagef("name the file %s.md so `burzblog push` knows which post it is", slug)
			}
			if _, err := os.Lstat(out); err == nil && !force {
				return fmt.Errorf("%s already exists; pass --force to replace it", out)
			}
			if err := writeFileAtomic(out, []byte(doc), 0o644); err != nil {
				return err
			}
			a.rememberBase(cfg.Server, out, version, []byte(doc))
			if a.asJSON {
				return a.emit(struct {
					Slug    string `json:"slug"`
					Version int64  `json:"version"`
					Path    string `json:"path"`
				}{slug, version, out})
			}
			fmt.Fprintf(a.Out, "%s Wrote %s (version %d). Send it back with: burzblog push %s\n", a.paint(green, "✓"), out, version, out)
			return nil
		},
	}
	cmd.Flags().StringVarP(&out, "output", "o", "", "file to write, named <slug>.md, or - for standard output")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "replace an existing file")
	return cmd
}

func (a *App) pushCmd() *cobra.Command {
	var force bool
	var slugFlag string
	var baseVersion int64
	cmd := &cobra.Command{
		Use:   "push <file.md|->",
		Short: "Save a local <slug>.md back to the server (creates the post if it is new)",
		Long: `Sends a Markdown file with frontmatter back to the server. The file name
is the post's slug.

If the post changed on the server since you pulled it, push refuses; use
--force to replace the server's version with yours.

With - the document is read from standard input and --slug names the post.
There is no pulled file to remember the version from, so pass the version you
started from with --base-version (burzblog list --json and burzblog pull --json
show it); without it, push refuses to touch a post that already exists.`,
		Example: `  burzblog push my-post.md
  burzblog push drafts/new-idea.md
  burzblog push my-post.md --json     # {"slug","status","version","created",...}
  burzblog pull my-post -o - | sed s/foo/bar/ | burzblog push - --slug my-post --base-version 3`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := args[0]
			slug := strings.TrimSuffix(filepath.Base(path), ".md")
			switch {
			case path == "-":
				slug = slugFlag
				if post.ValidateSlug(slug) != nil {
					return usagef("pushing from standard input needs --slug <slug>, using lowercase letters, digits and hyphens")
				}
			case slugFlag != "":
				return usagef("--slug only applies when reading standard input (-); the file name is the slug")
			case !strings.HasSuffix(path, ".md") || post.ValidateSlug(slug) != nil:
				return usagef("%s: name the file <slug>.md using lowercase letters, digits and hyphens", path)
			}
			raw, err := a.readInput(path)
			if err != nil {
				return err
			}
			cl, cfg, err := a.client()
			if err != nil {
				return err
			}
			c, cancel := ctx()
			defer cancel()
			var version int64
			var known bool
			switch {
			case baseVersion > 0:
				version, known = baseVersion, true
			case path != "-":
				version, known = a.bases()[baseKey(cfg.Server, path)]
			}
			if !known || force {
				current, err := cl.Post(c, slug)
				switch {
				case client.IsCode(err, "not_found"):
					version = 0
				case err != nil:
					return err
				case force:
					version = current.Version
				case path != "-":
					return conflictf("%s already exists on the server and this file was not pulled from it.\n"+
						"       Compare with `burzblog diff %s`, or pass --force to replace it", slug, path)
				default:
					return conflictf("%s already exists on the server and this file was not pulled from it.\n"+
						"       Pull it first (`burzblog pull %s -o other.md`) to compare, or pass --force to replace it", slug, slug)
				}
			}
			saved, err := cl.SaveMarkdown(c, slug, string(raw), version)
			if client.IsCode(err, "conflict") {
				compare := fmt.Sprintf("burzblog pull %s -o -", slug)
				if path != "-" {
					compare = "burzblog diff " + path
				}
				return conflictf("%s changed on the server since you pulled it, so nothing was overwritten.\n"+
					"       Compare with `%s`, then push again with --force to replace it", slug, compare)
			}
			if err != nil {
				return err
			}
			if path != "-" {
				a.rememberBase(cfg.Server, path, saved.Version, raw)
			}
			if a.asJSON {
				return a.emit(struct {
					postResult
					Created bool `json:"created"`
				}{summary(saved), version == 0})
			}
			verb := "Saved"
			if version == 0 {
				verb = "Created"
			}
			fmt.Fprintf(a.Out, "%s %s %s (version %d, %s).\n", a.paint(green, "✓"), verb, a.paint(bold, slug), saved.Version, clean(saved.Status))
			return nil
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "replace the server's version even if it changed")
	cmd.Flags().StringVar(&slugFlag, "slug", "", "post to save when reading standard input (-)")
	cmd.Flags().Int64Var(&baseVersion, "base-version", 0, "the version you started from; the save is refused if the post has moved on")
	return cmd
}

func (a *App) statusCmd(action, short string) *cobra.Command {
	return &cobra.Command{
		Use:   action + " <slug>",
		Short: short,
		Example: "  burzblog " + action + " my-post\n" +
			"  burzblog " + action + " my-post --json     # prints the post: {\"slug\",\"status\",\"version\",...}",
		Args: cobra.ExactArgs(1),
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
			if a.asJSON {
				return a.emit(summary(p))
			}
			if action == "unschedule" {
				fmt.Fprintf(a.Out, "%s %s is no longer scheduled; it stays a draft.\n", a.paint(green, "✓"), a.paint(bold, clean(p.Slug)))
				return nil
			}
			fmt.Fprintf(a.Out, "%s %s is now %s.\n", a.paint(green, "✓"), a.paint(bold, clean(p.Slug)), clean(p.Status))
			if action == "publish" || action == "unpublish" {
				fmt.Fprintln(a.Out, "  Reader sites with a deploy hook rebuild in a few seconds.")
			}
			return nil
		},
	}
}

// whenLayouts are the local-time forms `burzblog schedule` accepts besides
// RFC 3339.
var whenLayouts = []string{"2006-01-02 15:04", "2006-01-02T15:04"}

// parseWhen reads an RFC 3339 time, or a date and time in loc.
func parseWhen(s string, loc *time.Location) (time.Time, error) {
	s = strings.TrimSpace(s)
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	for _, layout := range whenLayouts {
		if t, err := time.ParseInLocation(layout, s, loc); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot read %q as a time; use 2026-11-03T09:00:00Z or \"2026-11-03 09:00\" (local time)", s)
}

func (a *App) scheduleCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "schedule <slug> <when>",
		Short: "Publish a draft automatically at a future time",
		Long: `Sets the time a draft publishes itself. <when> is RFC 3339 (2026-11-03T09:00:00Z)
or a local date and time in quotes ("2026-11-03 09:00"). It must be in the
future, at most two years away. Reader sites with a deploy hook rebuild when
it publishes; use "burzblog unschedule" to cancel.`,
		Example: `  burzblog schedule my-post "2026-11-03 09:00"
  burzblog schedule my-post 2026-11-03T09:00:00+05:30
  burzblog schedule my-post "2026-11-03 09:00" --json     # prints the post: {"slug","status","publishAt",...}`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			at, err := parseWhen(args[1], time.Local)
			if err != nil {
				return err
			}
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
			p, err := cl.Schedule(c, args[0], at, current.Version)
			if err != nil {
				return err
			}
			if a.asJSON {
				return a.emit(summary(p))
			}
			fmt.Fprintf(a.Out, "%s %s is scheduled for %s.\n", a.paint(green, "✓"), a.paint(bold, clean(p.Slug)),
				p.PublishAt.Local().Format("Mon 2 Jan 2006 15:04 MST"))
			return nil
		},
	}
}

func (a *App) openCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "open [slug]",
		Short: "Open the browser panel, or one post in it",
		Example: `  burzblog open
  burzblog open my-post`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := a.loadConfig()
			if err != nil {
				return err
			}
			if cfg.Server == "" {
				return errNotSignedIn
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

In CI, set BURZBLOG_SERVER and BURZBLOG_TOKEN (a build token from
Settings) instead of running burzblog login.`,
		Example: `  burzblog export --out website/content/blogs --clean
  BURZBLOG_SERVER=https://blog.example.com BURZBLOG_TOKEN=bzb_... burzblog export --out content`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if out == "" {
				return usagef("choose where to write the posts with --out <dir>")
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
			posts := len(files)
			if _, ok := files[redirectsFile]; ok {
				posts--
			}
			fmt.Fprintf(a.Out, "%s Exported %s to %s", a.paint(green, "✓"), count(posts, "published post", "published posts"), out)
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

// redirectsFile is the export's list of old slugs, next to the posts.
const redirectsFile = "redirects.json"

// readExport reads the export archive fully before anything is written, and
// accepts only flat <slug>.md regular files and redirects.json.
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
		if hdr.Typeflag != tar.TypeReg || (hdr.Name != redirectsFile && (!strings.HasSuffix(hdr.Name, ".md") || post.ValidateSlug(slug) != nil)) {
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

func (a *App) backupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backup [dir]",
		Short: "Download every post, drafts and archived included, as Markdown files",
		Long: `Writes every post to <dir>/<status>/<slug>.md, plus a README.txt, where status
is draft, published or archived. The directory defaults to
burzblog-backup-<date> in the current folder.

The backup holds current posts only. The database file is the complete backup,
including history; see docs/self-hosting.md. Restore a file with burzblog push.`,
		Example: `  burzblog backup
  burzblog backup ~/backups/blog
  burzblog backup ~/backups/blog --json     # {"dir":"~/backups/blog","posts":12}`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "burzblog-backup-" + time.Now().UTC().Format("2006-01-02")
			if len(args) == 1 {
				dir = args[0]
			}
			cl, _, err := a.client()
			if err != nil {
				return err
			}
			c, cancel := ctx()
			defer cancel()
			body, err := cl.Backup(c)
			if err != nil {
				return err
			}
			defer body.Close()
			files, err := readBackup(body)
			if err != nil {
				return err
			}
			for name, data := range files {
				path := filepath.Join(dir, filepath.FromSlash(name))
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					return err
				}
				if err := writeFileAtomic(path, data, 0o644); err != nil {
					return err
				}
			}
			if a.asJSON {
				return a.emit(struct {
					Dir   string `json:"dir"`
					Posts int    `json:"posts"`
				}{dir, len(files) - 1})
			}
			fmt.Fprintf(a.Out, "%s Backed up %s to %s.\n", a.paint(green, "✓"), count(len(files)-1, "post", "posts"), dir)
			return nil
		},
	}
	return cmd
}

// readBackup reads the backup archive fully before anything is written, and
// accepts only README.txt and regular <status>/<slug>.md files.
func readBackup(r io.Reader) (map[string][]byte, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("the backup is not a gzip archive: %w", err)
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
			return nil, fmt.Errorf("read backup: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg || !validBackupName(hdr.Name) {
			return nil, fmt.Errorf("the backup contains an unexpected entry %q", hdr.Name)
		}
		var buf bytes.Buffer
		n, err := io.Copy(&buf, io.LimitReader(tr, post.MaxBody+post.MaxExtra+1))
		if err != nil {
			return nil, fmt.Errorf("read backup: %w", err)
		}
		total += int(n)
		if n > post.MaxBody+post.MaxExtra || total > 512<<20 {
			return nil, errors.New("the backup is larger than expected")
		}
		files[hdr.Name] = buf.Bytes()
	}
	if _, ok := files["README.txt"]; !ok {
		return nil, errors.New("the backup has no README.txt, so it is not a BurzBlog backup")
	}
	return files, nil
}

func validBackupName(name string) bool {
	if name == "README.txt" {
		return true
	}
	dir, file, ok := strings.Cut(name, "/")
	return ok && post.ValidStatus(post.Status(dir)) && strings.HasSuffix(file, ".md") &&
		post.ValidateSlug(strings.TrimSuffix(file, ".md")) == nil
}
