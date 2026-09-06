package main

import "testing"

func TestValidateSummary(t *testing.T) {
	result := summary{Success: 8, Errors: 2, Codes: map[string]int{"Unavailable": 2}}
	if err := validateSummary(result, 8, 2, "Unavailable"); err != nil {
		t.Fatalf("valid summary rejected: %v", err)
	}
	for name, check := range map[string]func() error{
		"success": func() error { return validateSummary(result, 9, 0, "") },
		"errors":  func() error { return validateSummary(result, 0, 3, "") },
		"code":    func() error { return validateSummary(result, 0, 0, "Internal") },
	} {
		t.Run(name, func(t *testing.T) {
			if err := check(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}
