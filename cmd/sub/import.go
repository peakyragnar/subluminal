package main

import (
	"fmt"
	"os"

	"github.com/subluminal/subluminal/pkg/importer"
)

func runImport(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "Usage: sub import <claude|codex>")
		return 2
	}

	client, err := importer.ParseClient(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	result, err := importer.Import(importer.Options{Client: client})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Import failed: %v\n", err)
		return 1
	}

	fmt.Printf("Imported %s config: %s\n", result.Client, result.ConfigPath)
	if result.BackupPath != "" {
		fmt.Printf("Backup: %s\n", result.BackupPath)
	}
	if !result.Changed {
		fmt.Println("No changes needed (already imported)")
	}
	fmt.Printf("To restore: sub restore %s\n", result.Client)
	return 0
}
