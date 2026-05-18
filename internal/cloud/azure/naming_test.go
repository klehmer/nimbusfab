package azure

import (
	"strings"
	"testing"
)

func TestAzureStorageAccountName(t *testing.T) {
	tests := []struct {
		component, depID string
		wantPrefix       string
	}{
		{"uploads", "dep-abc12345-6789-aaaa-bbbb", "uploads"},
		{"my-very-long-component-name", "dep-xyz12345", "myverylongco"},
		{"web", "dep-9", "web"},
	}
	for _, tc := range tests {
		got := azureStorageAccountName(tc.component, tc.depID)
		if len(got) < 3 || len(got) > 24 {
			t.Errorf("%s/%s: len=%d (want 3..24): %q", tc.component, tc.depID, len(got), got)
		}
		for _, r := range got {
			if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')) {
				t.Errorf("%s/%s: invalid char %q in %q", tc.component, tc.depID, r, got)
			}
		}
		if !strings.HasPrefix(got, tc.wantPrefix) {
			t.Errorf("%s/%s: got %q want prefix %q", tc.component, tc.depID, got, tc.wantPrefix)
		}
	}
}

func TestAzureCloudResourceName(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"orders-db", "orders-db"},
		{"Web_App", "web-app"},
		{"net", "net"},
	}
	for _, tc := range tests {
		got := azureCloudResourceName(tc.in)
		if got != tc.want {
			t.Errorf("azureCloudResourceName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
