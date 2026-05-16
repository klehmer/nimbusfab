package estimator_test

import (
	"testing"

	"github.com/klehmer/nimbusfab/pkg/cost/estimator"
)

func TestUnitsFor_EC2DefaultIsHoursPerMonth(t *testing.T) {
	if got := estimator.UnitsFor("aws_instance", nil); got != estimator.HoursPerMonth {
		t.Errorf("got %v, want %v", got, estimator.HoursPerMonth)
	}
}

func TestUnitsFor_RDSDefaultIsHoursPerMonth(t *testing.T) {
	if got := estimator.UnitsFor("aws_db_instance", nil); got != estimator.HoursPerMonth {
		t.Errorf("got %v, want %v", got, estimator.HoursPerMonth)
	}
}

func TestUnitsFor_S3DefaultIs100GB(t *testing.T) {
	if got := estimator.UnitsFor("aws_s3_bucket", nil); got != estimator.DefaultStorageGB {
		t.Errorf("got %v, want %v", got, estimator.DefaultStorageGB)
	}
}

func TestUnitsFor_UserOverrideForCompute(t *testing.T) {
	got := estimator.UnitsFor("aws_instance", map[string]any{"hoursPerMonth": 365})
	if got != 365 {
		t.Errorf("got %v, want 365", got)
	}
}

func TestUnitsFor_UserOverrideForStorage(t *testing.T) {
	got := estimator.UnitsFor("aws_s3_bucket", map[string]any{"storageGB": 500.0})
	if got != 500 {
		t.Errorf("got %v, want 500", got)
	}
}

func TestUnitsFor_UnpricedTypeReturnsZero(t *testing.T) {
	if got := estimator.UnitsFor("aws_vpc", nil); got != 0 {
		t.Errorf("got %v, want 0", got)
	}
}
