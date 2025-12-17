// Command teststatus shows the status of all tests (unit + contract).
//
// It cross-references docs/Contract-Test-Checklist.md against actual test
// implementations and test results to give you instant visibility into
// what exists, what's passing, and what's missing.
//
// Test Layers:
//   - Layer 1: Unit tests (pkg/*_test.go) - fast, no shim needed
//   - Layer 2: Contract tests (test/contract/*_test.go) - integration
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	var (
		noRun      = flag.Bool("no-run", false, "Skip running tests, just show implementation status")
		problems   = flag.Bool("problems", false, "Show only failures and missing tests")
		jsonOutput = flag.Bool("json", false, "Output as JSON for tooling")
		category   = flag.String("category", "", "Filter by category (comma-separated: EVT,POL,ERR)")
		p0Only     = flag.Bool("p0-only", false, "Show only P0 (must-ship) tests")
		layer      = flag.String("layer", "", "Filter by layer: unit, contract, or all (default)")
	)
	flag.Parse()

	// Find project root (where go.mod lives)
	root, err := findProjectRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Parse the checklist to get expected contract tests
	checklistPath := filepath.Join(root, "docs", "Contract-Test-Checklist.md")
	specs, err := parseChecklist(checklistPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing checklist: %v\n", err)
		os.Exit(1)
	}

	// Scan contract test files
	contractDir := filepath.Join(root, "test", "contract")
	contractImpl, err := scanTestFiles(contractDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error scanning contract test files: %v\n", err)
		os.Exit(1)
	}

	// Scan unit test files in pkg/
	pkgDir := filepath.Join(root, "pkg")
	unitTests, err := scanUnitTests(pkgDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error scanning unit test files: %v\n", err)
		os.Exit(1)
	}

	// Run tests to get actual status (unless -no-run)
	var contractResults map[string]TestResult
	var unitResults []UnitTestResult
	if !*noRun {
		contractResults, err = runContractTests(root)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: some contract tests may have failed: %v\n", err)
		}
		unitResults, err = runUnitTests(root)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: some unit tests may have failed: %v\n", err)
		}
	}

	// Build the report
	report := buildReport(specs, contractImpl, contractResults)
	report.UnitTests = buildUnitReport(unitTests, unitResults)

	// Apply filters
	if *layer == "unit" {
		report.Categories = nil // Hide contract tests
	} else if *layer == "contract" {
		report.UnitTests = nil // Hide unit tests
	}
	if *category != "" {
		report = filterByCategory(report, *category)
	}
	if *p0Only {
		report = filterByPriority(report, "P0")
	}
	if *problems {
		report = filterProblems(report)
	}

	// Output
	if *jsonOutput {
		printJSON(report)
	} else {
		printReport(report)
	}

	// Exit with error if any P0 tests are failing or unit tests failing
	if report.Summary.P0Failing > 0 || report.Summary.UnitFailing > 0 {
		os.Exit(1)
	}
}

func findProjectRoot() (string, error) {
	// Start from current directory and walk up looking for go.mod
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		goModPath := filepath.Join(dir, "go.mod")
		if _, err := os.Stat(goModPath); err == nil {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("could not find go.mod in any parent directory")
		}
		dir = parent
	}
}
