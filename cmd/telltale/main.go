// telltale — an honest gauge for your coding agents.
//
// One binary, two modes (decisions/002):
//
//	telltale statusline   read Claude Code's statusline JSON on stdin, print one line
//	telltale hud          cross-vendor watch-mode TUI (in development)
package main

import (
	"fmt"
	"os"

	"github.com/sanlee-ys/telltale/internal/claude"
	"github.com/sanlee-ys/telltale/internal/statusline"
)

var version = "dev" // set via -ldflags at release time

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "statusline":
		runStatusline()
	case "hud":
		fmt.Fprintln(os.Stderr, "telltale hud: not built yet (see docs/design.md)")
		os.Exit(1)
	case "version", "--version", "-v":
		fmt.Println("telltale", version)
	default:
		usage()
		os.Exit(2)
	}
}

func runStatusline() {
	in, err := claude.Parse(os.Stdin)
	if err != nil {
		// A gauge must never crash the host UI: on bad input, render nothing
		// and exit clean. The error goes to stderr for `/statusline` debugging.
		fmt.Fprintln(os.Stderr, "telltale: bad statusline input:", err)
		os.Exit(0)
	}
	noColor := os.Getenv("NO_COLOR") != ""
	fmt.Println(statusline.Render(in, statusline.Options{NoColor: noColor}))
}

func usage() {
	fmt.Fprintln(os.Stderr, `telltale — an honest gauge for your coding agents

usage:
  telltale statusline    (wire into Claude Code settings.json statusLine command)
  telltale hud           cross-vendor session HUD (in development)
  telltale version`)
}
