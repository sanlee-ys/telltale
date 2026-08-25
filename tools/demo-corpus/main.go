// Command demo-corpus writes the synthetic HUD corpus to disk.
//
// It is a separate program rather than a telltale mode on purpose: it WRITES,
// and the gauges never write anything that is not theirs (CLAUDE.md's
// read/write boundary). The HUD's half of the pairing is a read-only flag.
//
//	go run ./tools/demo-corpus
//	telltale hud --root <the directory it printed>
//
// Regenerate right before a demo: the corpus timestamps are relative to the
// moment of writing, so the live session is live because its records are
// seconds old. See internal/democorpus for what the corpus contains and why.
//
// The tool deletes an existing output directory only when the corpus marker
// file is inside it — it can never wipe a directory it did not write. Any
// other existing directory is refused with the way out named.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/sanlee-ys/telltale/internal/democorpus"
)

func main() {
	out := flag.String("out", filepath.Join(os.TempDir(), "telltale-demo-corpus"),
		"directory to write the corpus into")
	flag.Parse()

	if err := run(*out); err != nil {
		fmt.Fprintln(os.Stderr, "demo-corpus:", err)
		os.Exit(1)
	}
}

func run(out string) error {
	abs, err := filepath.Abs(out)
	if err != nil {
		return err
	}

	_, statErr := os.Stat(abs)
	switch {
	case errors.Is(statErr, fs.ErrNotExist):
		// Fresh directory: nothing to clear.
	case statErr != nil:
		return statErr
	default:
		if _, err := os.Stat(filepath.Join(abs, democorpus.MarkerName)); err != nil {
			return fmt.Errorf("%s exists and carries no %s marker; this tool never deletes a directory it did not write — delete it yourself or pass a different --out",
				abs, democorpus.MarkerName)
		}
		if err := os.RemoveAll(abs); err != nil {
			return err
		}
	}

	if err := os.MkdirAll(abs, 0o755); err != nil {
		return err
	}
	if err := democorpus.Write(abs, time.Now()); err != nil {
		return err
	}

	fmt.Println("wrote the synthetic demo corpus to", abs)
	fmt.Println("every session, path and id in it is invented")
	fmt.Println()
	fmt.Println("point the HUD at it:")
	fmt.Println("  telltale hud --root", abs)
	return nil
}
