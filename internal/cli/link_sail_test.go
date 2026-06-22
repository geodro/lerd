package cli

import "testing"

func TestLinkShouldImportSail(t *testing.T) {
	cases := []struct {
		name        string
		interactive bool
		skipImport  bool
		hasSail     bool
		want        bool
	}{
		{"fresh interactive sail link", true, false, true, true},
		{"non-interactive never prompts", false, false, true, false},
		{"unattended import suppressed", true, true, true, false},
		{"no sail dependency", true, false, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := linkShouldImportSail(tc.interactive, tc.skipImport, tc.hasSail); got != tc.want {
				t.Fatalf("linkShouldImportSail(%v, %v, %v) = %v, want %v",
					tc.interactive, tc.skipImport, tc.hasSail, got, tc.want)
			}
		})
	}
}

// Regression guard: routing `lerd link` through the init wizard suppresses the
// post-link "Run lerd setup?" prompt via linkSkipSetupPrompt. That suppression
// must not also drop the Sail data-import offer, which has its own
// linkSkipDataImport flag — otherwise a fresh `lerd link` on a Sail project
// silently skips importing the existing Sail database.
func TestLinkImportsSailWhenWizardSuppressesSetupPrompt(t *testing.T) {
	linkSkipSetupPrompt = true
	linkSkipDataImport = false
	t.Cleanup(func() { linkSkipSetupPrompt = false })
	if !linkShouldImportSail(true, linkSkipDataImport, true) {
		t.Fatal("Sail import must still be offered in the wizard flow that suppresses the setup prompt")
	}
}
