package main

import (
	"flag"
	"os"

	"github.com/geodro/lerd/internal/tray"
)

func main() {
	var mono bool
	flag.BoolVar(&mono, "mono", false, "Use monochrome (template) icon — lets the OS recolor it to match the panel")
	flag.Parse()
	if err := tray.Run(mono); err != nil {
		os.Exit(1)
	}
}
