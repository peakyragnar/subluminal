package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/peakyragnar/subluminal/pkg/secret"
)

func runSecrets(args []string) int {
	if len(args) == 0 {
		secretsUsage()
		return 2
	}

	switch args[0] {
	case "add":
		return runSecretsAdd(args[1:])
	case "get":
		return runSecretsGet(args[1:])
	case "list":
		return runSecretsList(args[1:])
	case "remove", "rm":
		return runSecretsRemove(args[1:])
	case "-h", "--help", "help":
		secretsUsage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "Unknown secrets command: %s\n", args[0])
		secretsUsage()
		return 2
	}
}

func runSecretsAdd(args []string) int {
	fs := flag.NewFlagSet("sub secrets add", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	value := fs.String("value", "", "Secret value (omit to read from stdin)")
	source := fs.String("source", "file", "Secret source (file)")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(os.Stderr, "Usage: sub secrets add <ref> [--value <value>] [--source file]")
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "Usage: sub secrets add <ref> [--value <value>] [--source file]")
		return 2
	}

	ref := strings.TrimSpace(fs.Arg(0))
	if ref == "" {
		fmt.Fprintln(os.Stderr, "Secret ref is required")
		return 2
	}

	if strings.ToLower(strings.TrimSpace(*source)) != "file" {
		fmt.Fprintln(os.Stderr, "Only source=file is supported in v0.1")
		return 2
	}

	secretValue := *value
	if secretValue == "" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to read secret value: %v\n", err)
			return 1
		}
		secretValue = strings.TrimRight(string(data), "\n")
	}
	if secretValue == "" {
		fmt.Fprintln(os.Stderr, "Secret value is required")
		return 2
	}

	path, err := secret.ResolveStorePath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Resolve secrets path: %v\n", err)
		return 1
	}

	store, err := secret.LoadStore(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Load secrets store: %v\n", err)
		return 1
	}

	store[ref] = secret.NewEntry(secretValue, "file")
	if err := secret.SaveStore(path, store); err != nil {
		fmt.Fprintf(os.Stderr, "Save secrets store: %v\n", err)
		return 1
	}

	fmt.Printf("Stored secret %s\n", ref)
	return 0
}

func runSecretsGet(args []string) int {
	fs := flag.NewFlagSet("sub secrets get", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	meta := fs.Bool("meta", false, "Print metadata only")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(os.Stderr, "Usage: sub secrets get <ref> [--meta]")
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "Usage: sub secrets get <ref> [--meta]")
		return 2
	}

	ref := strings.TrimSpace(fs.Arg(0))
	if ref == "" {
		fmt.Fprintln(os.Stderr, "Secret ref is required")
		return 2
	}

	path, err := secret.ResolveStorePath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Resolve secrets path: %v\n", err)
		return 1
	}
	store, err := secret.LoadStore(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Load secrets store: %v\n", err)
		return 1
	}

	entry, ok := store[ref]
	if !ok {
		fmt.Fprintf(os.Stderr, "Secret not found: %s\n", ref)
		return 1
	}

	if *meta {
		if err := json.NewEncoder(os.Stdout).Encode(entry); err != nil {
			fmt.Fprintf(os.Stderr, "Write metadata: %v\n", err)
			return 1
		}
		return 0
	}

	fmt.Fprintln(os.Stdout, entry.Value)
	return 0
}

func runSecretsList(args []string) int {
	fs := flag.NewFlagSet("sub secrets list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(os.Stderr, "Usage: sub secrets list")
		return 2
	}

	path, err := secret.ResolveStorePath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Resolve secrets path: %v\n", err)
		return 1
	}
	store, err := secret.LoadStore(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Load secrets store: %v\n", err)
		return 1
	}

	if len(store) == 0 {
		return 0
	}

	refs := make([]string, 0, len(store))
	for ref := range store {
		refs = append(refs, ref)
	}
	sort.Strings(refs)
	for _, ref := range refs {
		entry := store[ref]
		source := entry.Source
		if source == "" {
			source = "file"
		}
		fmt.Printf("%s\t%s\n", ref, source)
	}
	return 0
}

func runSecretsRemove(args []string) int {
	fs := flag.NewFlagSet("sub secrets remove", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(os.Stderr, "Usage: sub secrets remove <ref>")
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "Usage: sub secrets remove <ref>")
		return 2
	}

	ref := strings.TrimSpace(fs.Arg(0))
	if ref == "" {
		fmt.Fprintln(os.Stderr, "Secret ref is required")
		return 2
	}

	path, err := secret.ResolveStorePath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Resolve secrets path: %v\n", err)
		return 1
	}
	store, err := secret.LoadStore(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Load secrets store: %v\n", err)
		return 1
	}

	if _, ok := store[ref]; !ok {
		fmt.Fprintf(os.Stderr, "Secret not found: %s\n", ref)
		return 1
	}
	delete(store, ref)
	if err := secret.SaveStore(path, store); err != nil {
		fmt.Fprintf(os.Stderr, "Save secrets store: %v\n", err)
		return 1
	}
	return 0
}

func secretsUsage() {
	fmt.Fprintln(os.Stderr, "Usage: sub secrets <add|get|list|remove> [options]")
}
