package ui

import "testing"

func TestWtKeyRoundTrip(t *testing.T) {
	key := wtKey("myapp", "feature-x")
	if key != "myapp/feature-x" {
		t.Fatalf("wtKey = %q, want myapp/feature-x", key)
	}
	site, wtBase, isWt := splitWtKey(key)
	if !isWt || site != "myapp" || wtBase != "feature-x" {
		t.Errorf("splitWtKey(%q) = (%q, %q, %v), want (myapp, feature-x, true)", key, site, wtBase, isWt)
	}
}

func TestSplitWtKey_mainSite(t *testing.T) {
	site, wtBase, isWt := splitWtKey("myapp")
	if isWt || site != "myapp" || wtBase != "" {
		t.Errorf("splitWtKey(myapp) = (%q, %q, %v), want (myapp, \"\", false)", site, wtBase, isWt)
	}
}
