package gcp

import (
	"strings"
	"testing"
)

func TestGCPResourceName(t *testing.T) {
	tests := []struct {
		component, depID string
		wantPrefix       string
	}{
		{"uploads", "dep-abcd1234-5678-aaaa", "uploads-"},
		{"web-network", "dep-xyz12345", "web-network-"},
		{"3invalid", "dep-1", "n3invalid-"},
	}
	for _, tc := range tests {
		got := gcpResourceName(tc.component, tc.depID)
		if len(got) < 3 || len(got) > 63 {
			t.Errorf("%s/%s: len=%d (want 3..63): %q", tc.component, tc.depID, len(got), got)
		}
		if got[0] < 'a' || got[0] > 'z' {
			t.Errorf("%s/%s: must start with letter, got %q", tc.component, tc.depID, got)
		}
		if strings.HasSuffix(got, "-") {
			t.Errorf("%s/%s: trailing hyphen in %q", tc.component, tc.depID, got)
		}
		for _, r := range got {
			if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-') {
				t.Errorf("%s/%s: invalid char %q in %q", tc.component, tc.depID, r, got)
			}
		}
		if !strings.HasPrefix(got, tc.wantPrefix) {
			t.Errorf("%s/%s: got %q want prefix %q", tc.component, tc.depID, got, tc.wantPrefix)
		}
	}
}
