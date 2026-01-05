package main

import (
	"fmt"
	"os"

	"github.com/peakyragnar/subluminal/pkg/importer"
)

func runRestore(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "Usage: sub restore <claude|codex>")
		return 2
	}

	client, err := importer.ParseClient(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	backupPath, err := importer.Restore(importer.Options{Client: client})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Restore failed: %v\n", err)
		return 1
	}

	fmt.Printf("Restored %s config from %s\n", client, backupPath)
	return 0
}
