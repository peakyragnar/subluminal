package main

import (
	"fmt"
	"os"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		usage()
		return 2
	}

	switch args[0] {
	case "import":
		return runImport(args[1:])
	case "restore":
		return runRestore(args[1:])
	case "ledgerd":
		return runLedgerd(args[1:])
	case "run":
		return runRun(args[1:])
	case "tail":
		return runTail(args[1:])
	case "query":
		return runQuery(args[1:])
	case "doctor":
		return runDoctor(args[1:])
	case "version":
		return runVersion(args[1:])
	case "-h", "--help", "help":
		usage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", args[0])
		usage()
		return 2
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "Usage: sub <command> [options]")
	fmt.Fprintln(os.Stderr, "Commands: import, restore, ledgerd, run, tail, query, doctor, version")
	fmt.Fprintln(os.Stderr, "Clients: claude, codex, headless, custom")
}
