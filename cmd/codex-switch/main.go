package main

import (
	"fmt"
	"os"
	"runtime/debug"

	"github.com/SilkageNet/codex-switch/internal/app"
)

var version = "dev"

func main() {
	if version == "dev" {
		if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
			version = info.Main.Version
		}
	}
	command := app.NewCommand(version)
	if err := command.Execute(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
