package parity

import "fmt"

// defaultWeights are the per-attribute weights documented in the parity spec.
// NOT user-tunable in v1.
var defaultWeights = map[string]float64{
	"compute.vCPU":         0.20,
	"compute.memoryGB":     0.15,
	"compute.architecture": 0.05,
	"storage.sizeGB":       0.10,
	"storage.iops":         0.10,
	"storage.class":        0.05,
	"features":             0.20,
	"database.engine":      0.10,
	"database.version":     0.05,
}

// BuildComparisons takes per-target profiles and returns AttrComparisons
// covering every attribute relevant to the profile's class.
func BuildComparisons(targets []TargetProfile) []AttrComparison {
	if len(targets) == 0 {
		return nil
	}
	class := targets[0].Profile.Class
	var out []AttrComparison
	switch class {
	case "compute":
		out = append(out, computeComparisons(targets)...)
		out = append(out, storageComparisons(targets)...)
		out = append(out, featuresComparisons(targets)...)
	case "database":
		out = append(out, databaseComparisons(targets)...)
		out = append(out, featuresComparisons(targets)...)
	case "storage":
		out = append(out, storageComparisons(targets)...)
		out = append(out, featuresComparisons(targets)...)
	case "network":
		out = append(out, featuresComparisons(targets)...)
	}
	return out
}

// Score computes the weighted mean over the AttrComparisons.
func Score(comparisons []AttrComparison) float64 {
	if len(comparisons) == 0 {
		return 1.0
	}
	totalWeight := 0.0
	weightedSum := 0.0
	seenFeatures := false
	var featuresScoreSum float64
	var featuresCount int
	for _, c := range comparisons {
		if isFeatureAttr(c.Attribute) {
			featuresScoreSum += c.Score
			featuresCount++
			seenFeatures = true
			continue
		}
		w, ok := defaultWeights[c.Attribute]
		if !ok {
			continue
		}
		totalWeight += w
		weightedSum += w * c.Score
	}
	if seenFeatures && featuresCount > 0 {
		avgFeatures := featuresScoreSum / float64(featuresCount)
		w := defaultWeights["features"]
		totalWeight += w
		weightedSum += w * avgFeatures
	}
	if totalWeight == 0 {
		return 1.0
	}
	return weightedSum / totalWeight
}

func isFeatureAttr(attr string) bool {
	return len(attr) > len("features.") && attr[:len("features.")] == "features."
}

func computeComparisons(targets []TargetProfile) []AttrComparison {
	var out []AttrComparison
	out = append(out, numericCmp("compute.vCPU", targets, func(p ResourceProfile) (float64, bool) {
		if p.Compute == nil {
			return 0, false
		}
		return float64(p.Compute.VCPU), true
	}))
	out = append(out, numericCmp("compute.memoryGB", targets, func(p ResourceProfile) (float64, bool) {
		if p.Compute == nil {
			return 0, false
		}
		return p.Compute.MemoryGB, true
	}))
	out = append(out, exactCmp("compute.architecture", targets, func(p ResourceProfile) (any, bool) {
		if p.Compute == nil {
			return nil, false
		}
		return p.Compute.Architecture, true
	}))
	return out
}

func storageComparisons(targets []TargetProfile) []AttrComparison {
	var out []AttrComparison
	out = append(out, numericCmp("storage.sizeGB", targets, func(p ResourceProfile) (float64, bool) {
		if p.Storage == nil {
			return 0, false
		}
		return float64(p.Storage.SizeGB), true
	}))
	out = append(out, numericCmp("storage.iops", targets, func(p ResourceProfile) (float64, bool) {
		if p.Storage == nil {
			return 0, false
		}
		return float64(p.Storage.IOPS), true
	}))
	out = append(out, exactCmp("storage.class", targets, func(p ResourceProfile) (any, bool) {
		if p.Storage == nil {
			return nil, false
		}
		return p.Storage.Class, true
	}))
	return out
}

func databaseComparisons(targets []TargetProfile) []AttrComparison {
	var out []AttrComparison
	out = append(out, exactCmp("database.engine", targets, func(p ResourceProfile) (any, bool) {
		if p.Database == nil {
			return nil, false
		}
		return p.Database.Engine, true
	}))
	out = append(out, exactCmp("database.version", targets, func(p ResourceProfile) (any, bool) {
		if p.Database == nil {
			return nil, false
		}
		return p.Database.Version, true
	}))
	out = append(out, numericCmp("compute.vCPU", targets, func(p ResourceProfile) (float64, bool) {
		if p.Database == nil {
			return 0, false
		}
		return float64(p.Database.Compute.VCPU), true
	}))
	out = append(out, numericCmp("compute.memoryGB", targets, func(p ResourceProfile) (float64, bool) {
		if p.Database == nil {
			return 0, false
		}
		return p.Database.Compute.MemoryGB, true
	}))
	out = append(out, numericCmp("storage.sizeGB", targets, func(p ResourceProfile) (float64, bool) {
		if p.Database == nil {
			return 0, false
		}
		return float64(p.Database.Storage.SizeGB), true
	}))
	return out
}

func featuresComparisons(targets []TargetProfile) []AttrComparison {
	keys := map[string]bool{}
	for _, t := range targets {
		for k := range t.Profile.Features {
			keys[k] = true
		}
	}
	var out []AttrComparison
	for key := range keys {
		c := AttrComparison{
			Attribute: "features." + key,
			Kind:      "boolean",
			Values:    map[string]any{},
		}
		first := true
		var firstVal bool
		allMatch := true
		for _, t := range targets {
			v := t.Profile.Features[key]
			c.Values[targetKey(t)] = v
			if first {
				firstVal = v
				first = false
			} else if v != firstVal {
				allMatch = false
			}
		}
		c.AllMatch = allMatch
		if allMatch {
			c.Score = 1.0
		}
		out = append(out, c)
	}
	return out
}

func numericCmp(attr string, targets []TargetProfile, get func(ResourceProfile) (float64, bool)) AttrComparison {
	c := AttrComparison{Attribute: attr, Kind: "numeric", Values: map[string]any{}}
	var min, max float64
	haveAny := false
	for _, t := range targets {
		v, ok := get(t.Profile)
		if !ok {
			continue
		}
		c.Values[targetKey(t)] = v
		if !haveAny {
			min, max = v, v
			haveAny = true
		}
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}
	if !haveAny {
		c.Score = 1.0
		c.AllMatch = true
		return c
	}
	c.MinValue, c.MaxValue = min, max
	c.AllMatch = min == max
	if max == 0 {
		c.Score = 1.0
	} else {
		c.Score = 1.0 - (max-min)/max
		if c.Score < 0 {
			c.Score = 0
		}
	}
	return c
}

func exactCmp(attr string, targets []TargetProfile, get func(ResourceProfile) (any, bool)) AttrComparison {
	c := AttrComparison{Attribute: attr, Kind: "exact", Values: map[string]any{}}
	var first any
	haveFirst := false
	allMatch := true
	for _, t := range targets {
		v, ok := get(t.Profile)
		if !ok {
			continue
		}
		c.Values[targetKey(t)] = v
		if !haveFirst {
			first = v
			haveFirst = true
		} else if fmt.Sprintf("%v", v) != fmt.Sprintf("%v", first) {
			allMatch = false
		}
	}
	c.AllMatch = allMatch
	if allMatch {
		c.Score = 1.0
	}
	return c
}

func targetKey(t TargetProfile) string {
	return t.Cloud + "/" + t.Region
}
