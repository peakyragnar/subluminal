package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// Status represents the test execution status
type Status int

const (
	StatusNone Status = iota // Not implemented
	StatusPass
	StatusFail
	StatusSkip
)

func (s Status) String() string {
	switch s {
	case StatusPass:
		return "pass"
	case StatusFail:
		return "FAIL"
	case StatusSkip:
		return "skip"
	default:
		return "none"
	}
}

func (s Status) Symbol() string {
	switch s {
	case StatusPass:
		return "\u2713" // checkmark
	case StatusFail:
		return "\u2717" // X
	case StatusSkip:
		return "\u25cb" // circle
	default:
		return "-"
	}
}

// TestResult represents actual test execution result
type TestResult struct {
	ID       string
	FuncName string
	Status   Status
	Duration time.Duration
	Output   string // Failure message if failed
}

// testEvent represents a single JSON event from go test -json
type testEvent struct {
	Time    time.Time `json:"Time"`
	Action  string    `json:"Action"`
	Package string    `json:"Package"`
	Test    string    `json:"Test"`
	Elapsed float64   `json:"Elapsed"`
	Output  string    `json:"Output"`
}

// UnitTestResult represents a unit test execution result
type UnitTestResult struct {
	FuncName string
	Package  string
	Status   Status
	Duration time.Duration
	Output   string
}

// runContractTests executes contract tests and returns results keyed by test ID
func runContractTests(projectRoot string) (map[string]TestResult, error) {
	cmd := exec.Command("go", "test", "-json", "-count=1", "./test/contract/...")
	cmd.Dir = projectRoot

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Run the tests - we don't check the error because test failures
	// will cause a non-zero exit code, but we still want the output
	cmd.Run()

	results := make(map[string]TestResult)
	outputs := make(map[string][]string) // Collect output lines per test

	// Pattern to extract test ID from function name
	pattern := regexp.MustCompile(`Test([A-Z]+)(\d+)_`)

	scanner := bufio.NewScanner(&stdout)
	for scanner.Scan() {
		var event testEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			continue
		}

		// Skip package-level events (no test name)
		if event.Test == "" {
			continue
		}

		// Extract test ID from function name
		matches := pattern.FindStringSubmatch(event.Test)
		if matches == nil {
			continue
		}
		id := matches[1] + "-" + matches[2]

		switch event.Action {
		case "pass":
			results[id] = TestResult{
				ID:       id,
				FuncName: event.Test,
				Status:   StatusPass,
				Duration: time.Duration(event.Elapsed * float64(time.Second)),
			}
		case "fail":
			result := TestResult{
				ID:       id,
				FuncName: event.Test,
				Status:   StatusFail,
				Duration: time.Duration(event.Elapsed * float64(time.Second)),
			}
			// Collect output for failed tests
			if lines, ok := outputs[event.Test]; ok {
				result.Output = strings.Join(lines, "")
			}
			results[id] = result
		case "skip":
			results[id] = TestResult{
				ID:       id,
				FuncName: event.Test,
				Status:   StatusSkip,
				Duration: time.Duration(event.Elapsed * float64(time.Second)),
			}
		case "output":
			// Collect output lines for potential failure messages
			outputs[event.Test] = append(outputs[event.Test], event.Output)
		}
	}

	return results, nil
}

// runUnitTests executes unit tests in pkg/ and returns results
func runUnitTests(projectRoot string) ([]UnitTestResult, error) {
	cmd := exec.Command("go", "test", "-json", "-count=1", "./pkg/...")
	cmd.Dir = projectRoot

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Run the tests
	cmd.Run()

	var results []UnitTestResult
	resultMap := make(map[string]*UnitTestResult)
	outputs := make(map[string][]string)

	scanner := bufio.NewScanner(&stdout)
	for scanner.Scan() {
		var event testEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			continue
		}

		// Skip package-level events
		if event.Test == "" {
			continue
		}

		// Extract package name from full package path
		pkgName := event.Package
		if idx := strings.LastIndex(pkgName, "/"); idx >= 0 {
			pkgName = pkgName[idx+1:]
		}

		key := event.Package + "/" + event.Test

		switch event.Action {
		case "pass":
			resultMap[key] = &UnitTestResult{
				FuncName: event.Test,
				Package:  pkgName,
				Status:   StatusPass,
				Duration: time.Duration(event.Elapsed * float64(time.Second)),
			}
		case "fail":
			result := &UnitTestResult{
				FuncName: event.Test,
				Package:  pkgName,
				Status:   StatusFail,
				Duration: time.Duration(event.Elapsed * float64(time.Second)),
			}
			if lines, ok := outputs[key]; ok {
				result.Output = strings.Join(lines, "")
			}
			resultMap[key] = result
		case "skip":
			resultMap[key] = &UnitTestResult{
				FuncName: event.Test,
				Package:  pkgName,
				Status:   StatusSkip,
				Duration: time.Duration(event.Elapsed * float64(time.Second)),
			}
		case "output":
			outputs[key] = append(outputs[key], event.Output)
		}
	}

	// Convert map to slice
	for _, r := range resultMap {
		results = append(results, *r)
	}

	return results, nil
}
