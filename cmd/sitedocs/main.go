// Command sitedocs renders docs/*.md into the project website (site/docs/).
// Run it with `task site:docs` after changing a guide.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/weburz/burzblog/internal/sitedocs"
)

func main() {
	docs := flag.String("docs", "docs", "folder with the Markdown guides")
	out := flag.String("out", "site/docs", "folder to write the HTML pages into")
	sitemap := flag.String("sitemap", "site/sitemap.xml", "where to write the sitemap; empty skips it")
	flag.Parse()
	if err := sitedocs.Build(*docs, *out); err != nil {
		fmt.Fprintln(os.Stderr, "sitedocs:", err)
		os.Exit(1)
	}
	if *sitemap == "" {
		return
	}
	pages, err := sitedocs.Render(*docs)
	if err == nil {
		err = os.WriteFile(*sitemap, sitedocs.Sitemap(pages), 0o644)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "sitedocs: sitemap:", err)
		os.Exit(1)
	}
}
