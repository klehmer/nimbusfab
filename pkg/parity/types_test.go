package parity_test

import (
	"testing"

	"github.com/klehmer/nimbusfab/pkg/parity"
)

func TestResourceProfile_ZeroValue(t *testing.T) {
	var p parity.ResourceProfile
	if p.Class != "" {
		t.Errorf("zero ResourceProfile.Class = %q, want empty", p.Class)
	}
	if p.Compute != nil || p.Storage != nil || p.Database != nil || p.Network != nil {
		t.Error("zero ResourceProfile should have nil sub-profiles")
	}
}
