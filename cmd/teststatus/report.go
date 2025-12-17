package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Report contains the full test status report
type Report struct {
	UnitTests   []UnitTestStatus `json:"unit_tests,omitempty"`
	Categories  []CategoryReport `json:"categories,omitempty"`
	Summary     Summary          `json:"summary"`
	NextActions []string         `json:"next_actions"`
}

// UnitTestStatus represents a unit test with its result
type UnitTestStatus struct {
	FuncName string `json:"func_name"`
	Package  string `json:"package"`
	Status   Status `json:"status"`
	File     string `json:"file,omitempty"`
	Line     int    `json:"line,omitempty"`
}

// CategoryReport contains tests grouped by category
type CategoryReport struct {
	Name  string       `json:"name"`
	Code  string       `json:"code"`
	Tests []TestStatus `json:"tests"`
}

// TestStatus combines spec, implementation, and result
type TestStatus struct {
	ID          string `json:"id"`
	Priority    string `json:"priority"`
	Description string `json:"description"`
	Status      Status `json:"status"`
	Implemented bool   `json:"implemented"`
	File        string `json:"file,omitempty"`
	Line        int    `json:"line,omitempty"`
	Output      string `json:"output,omitempty"`
}

// Summary contains aggregate statistics
type Summary struct {
	// Unit tests
	UnitTotal   int `json:"unit_total"`
	UnitPassing int `json:"unit_passing"`
	UnitFailing int `json:"unit_failing"`
	UnitSkipped int `json:"unit_skipped"`
	// Contract tests
	Total       int `json:"total"`
	Implemented int `json:"implemented"`
	Passing     int `json:"passing"`
	Failing     int `json:"failing"`
	Skipped     int `json:"skipped"`
	Missing     int `json:"missing"`
	P0Total     int `json:"p0_total"`
	P0Passing   int `json:"p0_passing"`
	P0Failing   int `json:"p0_failing"`
	P1Total     int `json:"p1_total"`
	P1Passing   int `json:"p1_passing"`
}

// buildReport creates a report from specs, implemented tests, and results
func buildReport(specs []TestSpec, implemented map[string]ImplementedTest, results map[string]TestResult) Report {
	// Group by category
	catTests := make(map[string][]TestStatus)
	var summary Summary

	for _, spec := range specs {
		ts := TestStatus{
			ID:          spec.ID,
			Priority:    spec.Priority,
			Description: spec.Description,
		}

		// Check if implemented
		if impl, ok := implemented[spec.ID]; ok {
			ts.Implemented = true
			ts.File = impl.File
			ts.Line = impl.Line

			// Check test result
			if result, ok := results[spec.ID]; ok {
				ts.Status = result.Status
				ts.Output = result.Output
			} else {
				// Implemented but no result (tests weren't run)
				ts.Status = StatusNone
			}
		} else {
			ts.Status = StatusNone
			ts.Implemented = false
		}

		catTests[spec.Category] = append(catTests[spec.Category], ts)

		// Update summary
		summary.Total++
		if ts.Implemented {
			summary.Implemented++
		} else {
			summary.Missing++
		}

		switch ts.Status {
		case StatusPass:
			summary.Passing++
		case StatusFail:
			summary.Failing++
		case StatusSkip:
			summary.Skipped++
		}

		// P0/P1 tracking
		if spec.Priority == "P0" {
			summary.P0Total++
			if ts.Status == StatusPass {
				summary.P0Passing++
			} else if ts.Status == StatusFail {
				summary.P0Failing++
			}
		} else {
			summary.P1Total++
			if ts.Status == StatusPass {
				summary.P1Passing++
			}
		}
	}

	// Build category reports in order
	var categories []CategoryReport
	for _, catCode := range categoryOrder {
		if tests, ok := catTests[catCode]; ok {
			categories = append(categories, CategoryReport{
				Name:  categoryName(catCode),
				Code:  catCode,
				Tests: tests,
			})
		}
	}

	// Generate next actions
	var nextActions []string
	for _, cat := range categories {
		for _, test := range cat.Tests {
			if test.Status == StatusFail {
				nextActions = append(nextActions, fmt.Sprintf("Fix failing: %s", test.ID))
			}
		}
	}

	var missingP0 []string
	for _, cat := range categories {
		for _, test := range cat.Tests {
			if !test.Implemented && test.Priority == "P0" {
				missingP0 = append(missingP0, test.ID)
			}
		}
	}
	if len(missingP0) > 0 {
		if len(missingP0) <= 5 {
			nextActions = append(nextActions, fmt.Sprintf("Implement missing P0: %s", strings.Join(missingP0, ", ")))
		} else {
			nextActions = append(nextActions, fmt.Sprintf("Implement %d missing P0 tests", len(missingP0)))
		}
	}

	return Report{
		Categories:  categories,
		Summary:     summary,
		NextActions: nextActions,
	}
}

// buildUnitReport creates a unit test section from scanned tests and results
func buildUnitReport(tests []UnitTest, results []UnitTestResult) []UnitTestStatus {
	// Build result map for quick lookup
	resultMap := make(map[string]UnitTestResult)
	for _, r := range results {
		key := r.Package + "/" + r.FuncName
		resultMap[key] = r
	}

	var statuses []UnitTestStatus
	for _, t := range tests {
		key := t.Package + "/" + t.FuncName
		status := UnitTestStatus{
			FuncName: t.FuncName,
			Package:  t.Package,
			File:     t.File,
			Line:     t.Line,
			Status:   StatusNone,
		}
		if r, ok := resultMap[key]; ok {
			status.Status = r.Status
		}
		statuses = append(statuses, status)
	}
	return statuses
}

// filterByCategory filters report to only show specified categories
func filterByCategory(report Report, categories string) Report {
	wanted := make(map[string]bool)
	for _, c := range strings.Split(categories, ",") {
		wanted[strings.TrimSpace(strings.ToUpper(c))] = true
	}

	var filtered []CategoryReport
	for _, cat := range report.Categories {
		if wanted[cat.Code] {
			filtered = append(filtered, cat)
		}
	}
	report.Categories = filtered
	return report
}

// filterByPriority filters to only show tests with given priority
func filterByPriority(report Report, priority string) Report {
	for i, cat := range report.Categories {
		var filtered []TestStatus
		for _, test := range cat.Tests {
			if test.Priority == priority {
				filtered = append(filtered, test)
			}
		}
		report.Categories[i].Tests = filtered
	}
	return report
}

// filterProblems filters to only show failing and missing tests
func filterProblems(report Report) Report {
	for i, cat := range report.Categories {
		var filtered []TestStatus
		for _, test := range cat.Tests {
			if test.Status == StatusFail || !test.Implemented {
				filtered = append(filtered, test)
			}
		}
		report.Categories[i].Tests = filtered
	}

	// Remove empty categories
	var nonEmpty []CategoryReport
	for _, cat := range report.Categories {
		if len(cat.Tests) > 0 {
			nonEmpty = append(nonEmpty, cat)
		}
	}
	report.Categories = nonEmpty
	return report
}

// printReport outputs the report in human-readable format
func printReport(report Report) {
	fmt.Println()
	fmt.Println("SUBLUMINAL TEST STATUS")
	fmt.Println(strings.Repeat("\u2550", 70))

	// Calculate unit test summary
	var unitPass, unitFail, unitSkip int
	for _, t := range report.UnitTests {
		switch t.Status {
		case StatusPass:
			unitPass++
		case StatusFail:
			unitFail++
		case StatusSkip:
			unitSkip++
		}
	}
	report.Summary.UnitTotal = len(report.UnitTests)
	report.Summary.UnitPassing = unitPass
	report.Summary.UnitFailing = unitFail
	report.Summary.UnitSkipped = unitSkip

	// Layer 1: Unit Tests
	if len(report.UnitTests) > 0 {
		fmt.Println()
		fmt.Println("LAYER 1: UNIT TESTS (pkg/*)")
		fmt.Printf("  %-10s %-10s %s\n", "PACKAGE", "STATUS", "TEST")
		fmt.Printf("  %s\n", strings.Repeat("\u2500", 66))

		// Group by package
		byPkg := make(map[string][]UnitTestStatus)
		for _, t := range report.UnitTests {
			byPkg[t.Package] = append(byPkg[t.Package], t)
		}

		for pkg, tests := range byPkg {
			for i, t := range tests {
				pkgName := ""
				if i == 0 {
					pkgName = pkg
				}
				statusStr := fmt.Sprintf("%s %s", t.Status.Symbol(), t.Status.String())
				fmt.Printf("  %-10s %-10s %s\n", pkgName, statusStr, t.FuncName)
			}
		}
		fmt.Println()
	}

	// Layer 2: Contract Tests
	if len(report.Categories) > 0 {
		fmt.Println("LAYER 2: CONTRACT TESTS (test/contract/*)")
		fmt.Println()

		for _, cat := range report.Categories {
			if len(cat.Tests) == 0 {
				continue
			}

			fmt.Printf("%s (%d tests)\n", strings.ToUpper(cat.Name), len(cat.Tests))
			fmt.Printf("  %-10s %-5s %-10s %s\n", "ID", "PRI", "STATUS", "DESCRIPTION")
			fmt.Printf("  %s\n", strings.Repeat("\u2500", 66))

			for _, test := range cat.Tests {
				statusStr := fmt.Sprintf("%s %s", test.Status.Symbol(), test.Status.String())
				fmt.Printf("  %-10s %-5s %-10s %s\n",
					test.ID,
					test.Priority,
					statusStr,
					truncate(test.Description, 40))
			}
			fmt.Println()
		}
	}

	// Summary
	fmt.Println(strings.Repeat("\u2550", 70))
	fmt.Println("SUMMARY")
	fmt.Println()

	if report.Summary.UnitTotal > 0 {
		fmt.Println("  Unit Tests (Layer 1):")
		fmt.Printf("    Total:   %d tests\n", report.Summary.UnitTotal)
		fmt.Printf("    Passing: %d  Failing: %d  Skipped: %d\n",
			report.Summary.UnitPassing, report.Summary.UnitFailing, report.Summary.UnitSkipped)
		fmt.Println()
	}

	if report.Summary.Total > 0 {
		fmt.Println("  Contract Tests (Layer 2):")
		fmt.Printf("    Total:       %d tests in checklist\n", report.Summary.Total)
		fmt.Printf("    Implemented: %d tests (%d%%)\n", report.Summary.Implemented, pct(report.Summary.Implemented, report.Summary.Total))
		fmt.Printf("    Passing:     %d  Failing: %d  Skipped: %d\n",
			report.Summary.Passing, report.Summary.Failing, report.Summary.Skipped)
		fmt.Printf("    Missing:     %d tests (not implemented)\n", report.Summary.Missing)
		fmt.Println()
		fmt.Printf("    P0 STATUS:   %d/%d passing (%d%%)\n", report.Summary.P0Passing, report.Summary.P0Total, pct(report.Summary.P0Passing, report.Summary.P0Total))
		fmt.Printf("    P1 STATUS:   %d/%d passing (%d%%)\n", report.Summary.P1Passing, report.Summary.P1Total, pct(report.Summary.P1Passing, report.Summary.P1Total))
	}

	if len(report.NextActions) > 0 {
		fmt.Println()
		fmt.Println("NEXT ACTIONS:")
		for _, action := range report.NextActions {
			fmt.Printf("  \u2022 %s\n", action)
		}
	}

	fmt.Println(strings.Repeat("\u2550", 70))
	fmt.Println()
}

// printJSON outputs the report as JSON
func printJSON(report Report) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(report)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

func pct(num, denom int) int {
	if denom == 0 {
		return 0
	}
	return (num * 100) / denom
}
