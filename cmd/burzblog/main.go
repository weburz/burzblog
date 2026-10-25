// Command burzblog runs the BurzBlog server and the terminal writing client.
package main

import (
	"os"

	"github.com/weburz/burzblog/internal/cli"
	"github.com/weburz/burzblog/internal/panel"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	os.Exit(cli.NewApp(version, panel.FS()).Execute(os.Args[1:]))
}
