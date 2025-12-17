package main

import (
	"bufio"
	"os"
	"regexp"
	"strings"
)

// TestSpec represents a test from the checklist
type TestSpec struct {
	ID          string // e.g., "EVT-001"
	Priority    string // "P0" or "P1"
	Description string // Human-readable description
	Category    string // Derived from ID prefix: EVT, HASH, POL, etc.
}

// parseChecklist reads Contract-Test-Checklist.md and extracts test specs
func parseChecklist(path string) ([]TestSpec, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var specs []TestSpec

	// Pattern to match test ID at start of line (e.g., "EVT-001" or "HASH-001")
	idPattern := regexp.MustCompile(`^([A-Z]+)-(\d+)\s`)

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()

		// Skip empty lines and non-table lines
		if !idPattern.MatchString(line) {
			continue
		}

		// Split by tab (the checklist uses tabs)
		fields := strings.Split(line, "\t")
		if len(fields) < 3 {
			// Try splitting by multiple spaces if tabs don't work
			fields = regexp.MustCompile(`\s{2,}`).Split(line, -1)
			if len(fields) < 3 {
				continue
			}
		}

		id := strings.TrimSpace(fields[0])
		priority := strings.TrimSpace(fields[1])
		description := strings.TrimSpace(fields[2])

		// Extract category from ID (e.g., "EVT" from "EVT-001")
		category := extractCategory(id)

		// Clean up description - remove spec references like "(A §1.1)"
		description = cleanDescription(description)

		specs = append(specs, TestSpec{
			ID:          id,
			Priority:    priority,
			Description: description,
			Category:    category,
		})
	}

	return specs, scanner.Err()
}

// extractCategory gets the category prefix from a test ID
func extractCategory(id string) string {
	parts := strings.Split(id, "-")
	if len(parts) > 0 {
		return parts[0]
	}
	return "UNKNOWN"
}

// cleanDescription removes spec references and cleans up the description
func cleanDescription(desc string) string {
	// Remove trailing spec references like "(A §1.1)" or "(B §2.3 precedence via order)"
	// Keep just the main description
	parenIdx := strings.Index(desc, "(")
	if parenIdx > 0 {
		desc = strings.TrimSpace(desc[:parenIdx])
	}
	return desc
}

// categoryNames maps category prefixes to human-readable names
var categoryNames = map[string]string{
	"EVT":   "Events",
	"HASH":  "Hashing",
	"BUF":   "Buffering",
	"POL":   "Policy",
	"ERR":   "Errors",
	"SEC":   "Secrets",
	"PROC":  "Process",
	"ID":    "Identity",
	"LED":   "Ledger",
	"IMP":   "Importer",
	"ADAPT": "Adapter",
}

// categoryName returns the human-readable name for a category
func categoryName(cat string) string {
	if name, ok := categoryNames[cat]; ok {
		return name
	}
	return cat
}

// categoryOrder defines the display order of categories
var categoryOrder = []string{
	"EVT", "HASH", "BUF", "POL", "ERR", "SEC", "PROC", "ID", "LED", "IMP", "ADAPT",
}
