package cli

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/weburz/burzblog/internal/client"
	"github.com/weburz/burzblog/internal/post"
)

// Import actions, as printed by --json. A dry run reports the action that
// would happen.
const (
	importCreated   = "created"
	importUpdated   = "updated"
	importUnchanged = "unchanged"
	importSkipped   = "skipped"
	importFailed    = "failed"
)

type importError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// importFile is the outcome for one Markdown file.
type importFile struct {
	File    string       `json:"file"`
	Slug    string       `json:"slug,omitempty"`
	Action  string       `json:"action"`
	Status  string       `json:"status,omitempty"`
	Version int64        `json:"version,omitempty"`
	Error   *importError `json:"error,omitempty"`
	exit    int
}

// failed marks r as failed with err.
func (r importFile) failed(err error) importFile {
	var code string
	code, r.exit = classify(err)
	r.Action, r.Error = importFailed, &importError{code, err.Error()}
	return r
}

func (a *App) importCmd() *cobra.Command {
	var recursive, asDraft, update, dryRun bool
	cmd := &cobra.Command{
		Use:   "import <file.md|dir>...",
		Short: "Bring existing <slug>.md files into BurzBlog (the inverse of export)",
		Long: `Creates a post from each Markdown file with frontmatter, in the shape
"burzblog export" writes and Nuxt Content reads. The slug is the frontmatter
slug, or the file name without .md. A published file stays published; a file
without a status becomes a draft. Frontmatter keys BurzBlog does not model are
kept.

Directories are read without descending into folders unless --recursive is
given; only .md files are imported. A post that already exists is skipped
unless --update is given, which saves the file over it as a new version (the
old one stays in History). One failing file does not stop the others; the exit
code is non-zero if any file failed.`,
		Example: `  burzblog import content/blog
  burzblog import content/blog --recursive --draft
  burzblog import a.md b.md --update
  burzblog import content/blog --dry-run
  burzblog import content/blog --json     # {"dryRun","created","updated","unchanged","skipped","failed","files":[...]}`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, _, err := a.client()
			if err != nil {
				return err
			}
			files, results := collectImports(args, recursive)
			seen := map[string]bool{}
			for _, f := range files {
				results = append(results, a.importOne(cl, f, seen, asDraft, update, dryRun))
			}
			return a.reportImport(results, dryRun)
		},
	}
	cmd.Flags().BoolVarP(&recursive, "recursive", "r", false, "also read .md files in sub-directories")
	cmd.Flags().BoolVar(&asDraft, "draft", false, "import every post as a draft, whatever its status says (with --update, this unpublishes)")
	cmd.Flags().BoolVar(&update, "update", false, "save the file over a post that already exists instead of skipping it")
	cmd.Flags().BoolVarP(&dryRun, "dry-run", "n", false, "show what would happen without changing anything")
	return cmd
}

// collectImports expands files and directories into the Markdown files to
// import, in a stable order. Paths that cannot be used are returned as failed
// results.
func collectImports(args []string, recursive bool) ([]string, []importFile) {
	var files []string
	var problems []importFile
	problem := func(path string, err error) {
		problems = append(problems, importFile{File: path}.failed(err))
	}
	for _, arg := range args {
		info, err := os.Stat(arg)
		switch {
		case err != nil:
			problem(arg, err)
		case !info.IsDir() && !strings.HasSuffix(arg, ".md"):
			problem(arg, &exitError{"validation", ExitValidation, "is not a Markdown (.md) file"})
		case !info.IsDir():
			files = append(files, arg)
		default:
			err := filepath.WalkDir(arg, func(path string, d fs.DirEntry, err error) error {
				switch {
				case err != nil:
					return err
				case d.IsDir() && path != arg && !recursive:
					return filepath.SkipDir
				case d.Type().IsRegular() && strings.HasSuffix(path, ".md"):
					files = append(files, path)
				}
				return nil
			})
			if err != nil {
				problem(arg, err)
			}
		}
	}
	sort.Strings(files)
	return files, problems
}

// importOne imports one file. It never returns an error: the outcome, failure
// included, is the result. seen holds the slugs already handled in this run,
// so a dry run reports a repeated slug as the real run would find it.
func (a *App) importOne(cl *client.Client, path string, seen map[string]bool, asDraft, update, dryRun bool) importFile {
	res := importFile{File: path}
	if info, err := os.Stat(path); err != nil {
		return res.failed(err)
	} else if info.Size() > post.MaxBody+post.MaxExtra {
		return res.failed(&exitError{"validation", ExitValidation, "is larger than a post may be"})
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return res.failed(err)
	}
	doc, err := post.Decode(string(raw))
	if err != nil {
		return res.failed(&exitError{"validation", ExitValidation, err.Error()})
	}
	slug := doc.Slug
	if slug == "" {
		slug = strings.TrimSuffix(filepath.Base(path), ".md")
	}
	res.Slug = slug
	if asDraft {
		doc.Status = post.Draft
	}
	p := &post.Post{Slug: slug}
	doc.Apply(p)
	post.Normalize(p)
	if err := post.Validate(p); err != nil {
		return res.failed(&exitError{"validation", ExitValidation, err.Error()})
	}
	res.Status = string(p.Status)

	c, cancel := ctx()
	defer cancel()
	current, err := cl.Post(c, slug)
	if err != nil && !client.IsCode(err, "not_found") {
		return res.failed(err)
	}
	exists := err == nil || (dryRun && seen[slug])
	seen[slug] = true
	switch {
	case exists && !update:
		res.Action = importSkipped
		return res
	case dryRun && exists:
		res.Action = importUpdated
		return res
	case dryRun:
		res.Action = importCreated
		return res
	}

	// Send the file as written, unless --draft has to change its status.
	send := string(raw)
	if asDraft {
		send = post.Encode(p)
	}
	var version int64
	if exists {
		version = current.Version
	}
	saved, err := cl.SaveMarkdown(c, slug, send, version)
	if err != nil {
		return res.failed(err)
	}
	res.Action, res.Status, res.Version = importCreated, saved.Status, saved.Version
	if exists {
		res.Action = importUpdated
		if saved.Version == version {
			res.Action = importUnchanged
		}
	}
	return res
}

// reportImport prints the results and returns the run's failure, if any.
func (a *App) reportImport(results []importFile, dryRun bool) error {
	counts := map[string]int{}
	for _, r := range results {
		counts[r.Action]++
	}
	if a.asJSON {
		if err := a.emit(struct {
			DryRun    bool         `json:"dryRun"`
			Created   int          `json:"created"`
			Updated   int          `json:"updated"`
			Unchanged int          `json:"unchanged"`
			Skipped   int          `json:"skipped"`
			Failed    int          `json:"failed"`
			Files     []importFile `json:"files"`
		}{dryRun, counts[importCreated], counts[importUpdated], counts[importUnchanged], counts[importSkipped], counts[importFailed], results}); err != nil {
			return err
		}
	} else {
		would := ""
		if dryRun {
			would = "would be "
		}
		for _, r := range results {
			switch r.Action {
			case importCreated, importUpdated:
				fmt.Fprintf(a.Out, "%s %s %s%s (%s)\n", a.paint(green, "✓"), a.paint(bold, clean(r.Slug)), would, r.Action, clean(r.Status))
			case importUnchanged:
				fmt.Fprintf(a.Out, "%s unchanged\n", clean(r.Slug))
			case importSkipped:
				fmt.Fprintf(a.Out, "skipped %s: already exists\n", clean(r.Slug))
			case importFailed:
				fmt.Fprintf(a.Err, "%s %s: %s\n", a.paint(red, "failed"), clean(r.File), cleanLines(r.Error.Message))
			}
		}
		prefix := ""
		if dryRun {
			prefix = "Dry run: "
		}
		fmt.Fprintf(a.Out, "\n%s%d created, %d updated, %d unchanged, %d skipped, %d failed\n", prefix,
			counts[importCreated], counts[importUpdated], counts[importUnchanged], counts[importSkipped], counts[importFailed])
	}
	if counts[importFailed] == 0 {
		return nil
	}
	// One kind of failure keeps its code and exit code; a mix is a plain error.
	code, exit := "import_failed", ExitError
	exits := map[int]bool{}
	for _, r := range results {
		if r.Action == importFailed {
			code, exit = r.Error.Code, r.exit
			exits[r.exit] = true
		}
	}
	if len(exits) > 1 {
		code, exit = "import_failed", ExitError
	}
	return &exitError{code, exit, fmt.Sprintf("%d of %s could not be imported", counts[importFailed], count(len(results), "file", "files"))}
}
