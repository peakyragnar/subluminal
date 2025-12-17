package main

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ImplementedTest represents a contract test found in source code
type ImplementedTest struct {
	ID       string // Normalized ID: "EVT-001"
	FuncName string // Full function name: "TestEVT001_JSONLSingleLineEvents"
	File     string // Source file path
	Line     int    // Line number in file
}

// UnitTest represents a unit test found in pkg/*_test.go
type UnitTest struct {
	FuncName string // e.g., "TestCanonicalize_KeyOrdering"
	Package  string // e.g., "canonical"
	File     string // Source file path
	Line     int    // Line number
}

// scanTestFiles finds all implemented contract test functions in test/contract/
func scanTestFiles(testDir string) (map[string]ImplementedTest, error) {
	implemented := make(map[string]ImplementedTest)

	// Pattern to match test function declarations
	// Captures: TestEVT001_Name or TestHASH001_Name etc.
	pattern := regexp.MustCompile(`^func (Test([A-Z]+)(\d+)_\w+)\(`)

	err := filepath.Walk(testDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Only process Go test files
		if info.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return nil
		}

		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		lineNum := 0
		for scanner.Scan() {
			lineNum++
			line := scanner.Text()

			matches := pattern.FindStringSubmatch(line)
			if matches == nil {
				continue
			}

			funcName := matches[1] // TestEVT001_JSONLSingleLineEvents
			category := matches[2] // EVT
			number := matches[3]   // 001

			// Normalize to ID format: EVT-001
			id := category + "-" + number

			implemented[id] = ImplementedTest{
				ID:       id,
				FuncName: funcName,
				File:     path,
				Line:     lineNum,
			}
		}

		return scanner.Err()
	})

	return implemented, err
}

// scanUnitTests finds all unit test functions in pkg/
func scanUnitTests(pkgDir string) ([]UnitTest, error) {
	var tests []UnitTest

	// Pattern to match any test function
	pattern := regexp.MustCompile(`^func (Test\w+)\(`)

	err := filepath.Walk(pkgDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Only process Go test files
		if info.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return nil
		}

		// Extract package name from path (e.g., pkg/canonical -> canonical)
		relPath, _ := filepath.Rel(pkgDir, path)
		pkgName := filepath.Dir(relPath)
		if pkgName == "." {
			pkgName = filepath.Base(filepath.Dir(path))
		}

		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		lineNum := 0
		for scanner.Scan() {
			lineNum++
			line := scanner.Text()

			matches := pattern.FindStringSubmatch(line)
			if matches == nil {
				continue
			}

			tests = append(tests, UnitTest{
				FuncName: matches[1],
				Package:  pkgName,
				File:     path,
				Line:     lineNum,
			})
		}

		return scanner.Err()
	})

	return tests, err
}
