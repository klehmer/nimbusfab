package parity_test

import (
	"math"
	"testing"

	"github.com/klehmer/nimbusfab/pkg/parity"
)

func TestScore_AllIdenticalTargets(t *testing.T) {
	targets := []parity.TargetProfile{
		{Cloud: "aws", Region: "us-east-1", Profile: parity.ResourceProfile{
			Class:   "compute",
			Compute: &parity.ComputeProfile{VCPU: 2, MemoryGB: 4, Architecture: "x86_64"},
			Storage: &parity.StorageProfile{SizeGB: 30, Class: "ssd"},
		}},
		{Cloud: "gcp", Region: "us-central1", Profile: parity.ResourceProfile{
			Class:   "compute",
			Compute: &parity.ComputeProfile{VCPU: 2, MemoryGB: 4, Architecture: "x86_64"},
			Storage: &parity.StorageProfile{SizeGB: 30, Class: "ssd"},
		}},
	}
	cmps := parity.BuildComparisons(targets)
	score := parity.Score(cmps)
	if math.Abs(score-1.0) > 0.001 {
		t.Errorf("identical targets: score = %f, want 1.0", score)
	}
}

func TestScore_DivergentMemory(t *testing.T) {
	targets := []parity.TargetProfile{
		{Cloud: "aws", Region: "us-east-1", Profile: parity.ResourceProfile{
			Class:   "compute",
			Compute: &parity.ComputeProfile{VCPU: 2, MemoryGB: 4, Architecture: "x86_64"},
			Storage: &parity.StorageProfile{SizeGB: 30, Class: "ssd"},
		}},
		{Cloud: "gcp", Region: "us-central1", Profile: parity.ResourceProfile{
			Class:   "compute",
			Compute: &parity.ComputeProfile{VCPU: 2, MemoryGB: 8, Architecture: "x86_64"},
			Storage: &parity.StorageProfile{SizeGB: 30, Class: "ssd"},
		}},
	}
	cmps := parity.BuildComparisons(targets)
	score := parity.Score(cmps)
	if score >= 1.0 || score <= 0.5 {
		t.Errorf("score for diverging memory = %f; want in (0.5, 1.0)", score)
	}
}

func TestScore_DifferentArchitecture_ZerosOutThatGroup(t *testing.T) {
	targets := []parity.TargetProfile{
		{Cloud: "aws", Region: "us-east-1", Profile: parity.ResourceProfile{
			Class:   "compute",
			Compute: &parity.ComputeProfile{VCPU: 2, MemoryGB: 4, Architecture: "x86_64"},
		}},
		{Cloud: "gcp", Region: "us-central1", Profile: parity.ResourceProfile{
			Class:   "compute",
			Compute: &parity.ComputeProfile{VCPU: 2, MemoryGB: 4, Architecture: "arm64"},
		}},
	}
	cmps := parity.BuildComparisons(targets)
	for _, c := range cmps {
		if c.Attribute == "compute.architecture" {
			if c.Score != 0 {
				t.Errorf("arch mismatch: score = %f, want 0", c.Score)
			}
			return
		}
	}
	t.Error("no architecture comparison in output")
}

func TestScore_SingleTargetIsTrivially1(t *testing.T) {
	targets := []parity.TargetProfile{
		{Cloud: "aws", Region: "us-east-1", Profile: parity.ResourceProfile{
			Class:   "compute",
			Compute: &parity.ComputeProfile{VCPU: 2, MemoryGB: 4, Architecture: "x86_64"},
		}},
	}
	cmps := parity.BuildComparisons(targets)
	score := parity.Score(cmps)
	if score < 0.99 {
		t.Errorf("single target should be ~1.0, got %f", score)
	}
}

func TestScore_FeaturesGroup_AllMatchVsMismatch(t *testing.T) {
	matching := []parity.TargetProfile{
		{Cloud: "aws", Profile: parity.ResourceProfile{Class: "database",
			Database: &parity.DatabaseProfile{Engine: "postgres", Compute: parity.ComputeProfile{VCPU: 2, MemoryGB: 4}, Storage: parity.StorageProfile{SizeGB: 100, Class: "ssd"}},
			Features: map[string]bool{"multiAZ": true, "pointInTimeRestore": true}}},
		{Cloud: "gcp", Profile: parity.ResourceProfile{Class: "database",
			Database: &parity.DatabaseProfile{Engine: "postgres", Compute: parity.ComputeProfile{VCPU: 2, MemoryGB: 4}, Storage: parity.StorageProfile{SizeGB: 100, Class: "ssd"}},
			Features: map[string]bool{"multiAZ": true, "pointInTimeRestore": true}}},
	}
	mismatching := []parity.TargetProfile{
		matching[0],
		{Cloud: "gcp", Profile: parity.ResourceProfile{Class: "database",
			Database: &parity.DatabaseProfile{Engine: "postgres", Compute: parity.ComputeProfile{VCPU: 2, MemoryGB: 4}, Storage: parity.StorageProfile{SizeGB: 100, Class: "ssd"}},
			Features: map[string]bool{"multiAZ": false, "pointInTimeRestore": true}}},
	}
	matchScore := parity.Score(parity.BuildComparisons(matching))
	mismatchScore := parity.Score(parity.BuildComparisons(mismatching))
	if mismatchScore >= matchScore {
		t.Errorf("mismatch should score lower: match=%f mismatch=%f", matchScore, mismatchScore)
	}
}
