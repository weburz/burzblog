package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/weburz/burzblog/internal/client"
	"github.com/weburz/burzblog/internal/post"
)

// errDiffers makes `burzblog diff` exit 1 when the texts differ, like diff(1).
// Execute exits with ExitError for it without printing anything.
var errDiffers = &exitError{"differs", ExitError, "the file differs from the server's version"}

// diffContext is the number of unchanged lines shown around each change.
const diffContext = 3

// diffTarget works out the post and the local file from the arguments:
// <file.md> (the slug is the file name, as in push), <slug> (./<slug>.md) or
// <slug> <file>.
func diffTarget(args []string) (slug, file string, err error) {
	if len(args) == 2 {
		if post.ValidateSlug(args[0]) != nil {
			return "", "", usagef("%s is not a post slug. With two arguments, give the slug first and the file second", args[0])
		}
		return args[0], args[1], nil
	}
	if strings.HasSuffix(args[0], ".md") {
		slug = strings.TrimSuffix(filepath.Base(args[0]), ".md")
		if post.ValidateSlug(slug) != nil {
			return "", "", usagef("%s: name the file <slug>.md using lowercase letters, digits and hyphens, or give the slug first: burzblog diff <slug> %s", args[0], args[0])
		}
		return slug, args[0], nil
	}
	if post.ValidateSlug(args[0]) != nil {
		return "", "", usagef("%s is neither a <slug>.md file nor a post slug", args[0])
	}
	return args[0], args[0] + ".md", nil
}

// readable makes a line of a diff safe to print: tabs become spaces, a
// trailing carriage return stays visible, and control characters are replaced.
func readable(line string) string {
	line = strings.ReplaceAll(line, "\t", "    ")
	if rest, ok := strings.CutSuffix(line, "\r"); ok {
		return clean(rest) + "␍"
	}
	return clean(line)
}

func (a *App) paintDiff(kind byte, s string) string {
	switch kind {
	case '-':
		return a.paint(red, s)
	case '+':
		return a.paint(green, s)
	case '@':
		return a.paint(yellow, s)
	case 'h':
		return a.paint(bold, s)
	}
	return s
}

func (a *App) diffCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "diff <file.md|slug> [file.md]",
		Short: "Show how a local file differs from the post on the server",
		Long: `Prints a unified diff from the server's current post (the Markdown with
frontmatter that pull writes) to your file: lines starting with - are only on the
server, lines starting with + only in your file.

With a file only, the slug is the file name, as in push. With a slug only, the
file is ./<slug>.md. It also says whether the file is based on the server's
current version or an older one, using the version pull recorded.

Exit code 0 means identical, 1 means different; other failures use the codes
listed in docs/terminal.md. Text output is for reading: control characters from
the post are replaced, and --json carries the exact diff.`,
		Example: `  burzblog diff my-post.md
  burzblog diff my-post                      # ./my-post.md
  burzblog diff my-post drafts/copy.md
  burzblog diff my-post.md --json     # {"slug","baseVersion","serverVersion","stale","identical","diff"}`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			slug, file, err := diffTarget(args)
			if err != nil {
				return err
			}
			local, err := os.ReadFile(file)
			if err != nil {
				return usagef("cannot read %s: %v", file, err)
			}
			cl, cfg, err := a.client()
			if err != nil {
				return err
			}
			c, cancel := ctx()
			defer cancel()
			doc, serverVersion, err := cl.Markdown(c, slug)
			if err != nil {
				return err
			}
			base, known := a.bases()[baseKey(cfg.Server, file)]
			stale := known && serverVersion > base
			hs := diffText(doc, string(local), diffContext)
			serverName := fmt.Sprintf("server/%s.md (version %d)", slug, serverVersion)

			if a.asJSON {
				var baseVersion *int64
				if known {
					baseVersion = &base
				}
				return a.emitDiff(struct {
					Slug          string `json:"slug"`
					BaseVersion   *int64 `json:"baseVersion"`
					ServerVersion int64  `json:"serverVersion"`
					Stale         bool   `json:"stale"`
					Identical     bool   `json:"identical"`
					Diff          string `json:"diff"`
				}{slug, baseVersion, serverVersion, stale, len(hs) == 0,
					renderUnified(serverName, file, hs, nil, nil)}, len(hs) == 0)
			}

			switch {
			case !known:
				fmt.Fprintf(a.Err, "%s This file was not pulled by this computer, so its base version is unknown; the server is at version %d.\n",
					a.paint(yellow, "!"), serverVersion)
			case stale:
				fmt.Fprintf(a.Err, "%s This file is based on version %d, but the server is at version %d: the post was saved since you pulled it.\n",
					a.paint(yellow, "!"), base, serverVersion)
			default:
				fmt.Fprintf(a.Err, "This file is based on the server's current version (%d).\n", serverVersion)
			}
			if len(hs) == 0 {
				fmt.Fprintln(a.Err, "No differences.")
				return nil
			}
			fmt.Fprint(a.Out, renderUnified(clean(serverName), clean(file), hs, readable, a.paintDiff))
			return errDiffers
		},
	}
}

// emitDiff prints v and then reports a difference through the exit code.
func (a *App) emitDiff(v any, identical bool) error {
	if err := a.emit(v); err != nil {
		return err
	}
	if identical {
		return nil
	}
	return errDiffers
}

// File states reported by `burzblog status`.
const (
	stateUpToDate        = "up_to_date"
	stateModified        = "modified"
	stateChangedOnServer = "changed_on_server"
	stateBothChanged     = "both_changed"
	stateNotOnServer     = "not_on_server"
	stateNotPulled       = "not_pulled"
)

var stateLabels = map[string]string{
	stateUpToDate:        "up to date",
	stateModified:        "modified locally",
	stateChangedOnServer: "changed on server",
	stateBothChanged:     "both changed",
	stateNotOnServer:     "not on server",
	stateNotPulled:       "differs, not pulled by this computer",
}

// fileState compares a local file with the server's post. serverVersion is 0
// when the post does not exist.
func fileState(local []byte, serverDoc string, serverVersion int64, base int64, known bool, savedHash string, hashKnown bool) string {
	switch {
	case serverVersion == 0:
		return stateNotOnServer
	case string(local) == serverDoc:
		return stateUpToDate
	case !known:
		return stateNotPulled
	}
	changedOnServer := serverVersion > base
	// Without a recorded hash the file can only be judged against the
	// server's text, which is no help once the server has moved on.
	modified := true
	if hashKnown {
		modified = contentHash(local) != savedHash
	}
	switch {
	case changedOnServer && modified:
		return stateBothChanged
	case changedOnServer:
		return stateChangedOnServer
	case modified:
		return stateModified
	}
	// Same version, same text as when pulled, yet not the server's text: the
	// server rewrote it when saving (for example normalised the frontmatter).
	return stateUpToDate
}

func (a *App) fileStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status [dir]",
		Short: "Show which <slug>.md files in a folder differ from the server",
		Long: `Lists each <slug>.md file in the folder (default .) with one of: up to date,
modified locally, changed on server (the server's version is newer than the one
you pulled), both changed, or not on server. A file this computer never pulled
or pushed that differs from the server is reported as not pulled, since its
base version is unknown. Subfolders are not entered.

Use burzblog diff <file> to see the changes. The exit code is non-zero only
when something went wrong.`,
		Example: `  burzblog status
  burzblog status drafts
  burzblog status --json     # {"dir","files":[{"file","slug","state","baseVersion","serverVersion"}]}`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) == 1 {
				dir = args[0]
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				return fmt.Errorf("cannot read %s: %v", dir, err)
			}
			cl, cfg, err := a.client()
			if err != nil {
				return err
			}
			bases, hashes := a.bases(), a.baseHashes()

			type fileResult struct {
				File          string `json:"file"`
				Slug          string `json:"slug"`
				State         string `json:"state"`
				BaseVersion   *int64 `json:"baseVersion"`
				ServerVersion *int64 `json:"serverVersion"`
			}
			files := []fileResult{}
			for _, e := range entries {
				slug, isMD := strings.CutSuffix(e.Name(), ".md")
				if !isMD || !e.Type().IsRegular() || post.ValidateSlug(slug) != nil {
					continue
				}
				path := filepath.Join(dir, e.Name())
				local, err := os.ReadFile(path)
				if err != nil {
					return fmt.Errorf("cannot read %s: %v", path, err)
				}
				c, cancel := ctx()
				doc, serverVersion, err := cl.Markdown(c, slug)
				cancel()
				if err != nil && !client.IsCode(err, "not_found") {
					return err
				}
				key := baseKey(cfg.Server, path)
				base, known := bases[key]
				savedHash, hashKnown := hashes[key]
				r := fileResult{File: path, Slug: slug}
				r.State = fileState(local, doc, serverVersion, base, known, savedHash, hashKnown)
				if known {
					r.BaseVersion = &base
				}
				if serverVersion > 0 {
					r.ServerVersion = &serverVersion
				}
				files = append(files, r)
			}

			if a.asJSON {
				return a.emit(struct {
					Dir   string       `json:"dir"`
					Files []fileResult `json:"files"`
				}{dir, files})
			}
			if len(files) == 0 {
				fmt.Fprintf(a.Out, "No <slug>.md files in %s.\n", clean(dir))
				return nil
			}
			w := tabwriter.NewWriter(a.Out, 0, 0, 2, ' ', 0)
			for _, f := range files {
				label := stateLabels[f.State]
				switch f.State {
				case stateUpToDate:
					label = a.paint(green, label)
				case stateBothChanged:
					label = a.paint(red, label)
				case stateModified, stateChangedOnServer, stateNotPulled:
					label = a.paint(yellow, label)
				default:
					label = a.paint(dim, label)
				}
				if f.BaseVersion != nil && f.ServerVersion != nil && f.State != stateUpToDate {
					label += a.paint(dim, fmt.Sprintf(" (base %d, server %d)", *f.BaseVersion, *f.ServerVersion))
				}
				fmt.Fprintf(w, "%s\t%s\n", clean(f.File), label)
			}
			return w.Flush()
		},
	}
}
