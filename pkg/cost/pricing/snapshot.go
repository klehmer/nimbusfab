package pricing

import (
	"embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

//go:embed snapshot/*.json
var snapshotFS embed.FS

// SnapshotEntry is one curated price-list row.
type SnapshotEntry struct {
	Key           map[string]any `json:"key"`
	UnitPrice     float64        `json:"unitPrice"`
	UnitOfMeasure string         `json:"unitOfMeasure"`
}

// Snapshot is one cloud's full snapshot file.
type Snapshot struct {
	Cloud     string          `json:"cloud"`
	Currency  string          `json:"currency"`
	FetchedAt time.Time       `json:"fetchedAt"`
	Source    string          `json:"source"`
	Entries   []SnapshotEntry `json:"entries"`
}

// LoadSnapshots reads every embedded snapshot file and returns them keyed by cloud.
func LoadSnapshots() (map[string]*Snapshot, error) {
	out := map[string]*Snapshot{}
	entries, err := snapshotFS.ReadDir("snapshot")
	if err != nil {
		return nil, fmt.Errorf("LoadSnapshots: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		body, err := snapshotFS.ReadFile("snapshot/" + e.Name())
		if err != nil {
			return nil, fmt.Errorf("LoadSnapshots: read %s: %w", e.Name(), err)
		}
		var s Snapshot
		if err := json.Unmarshal(body, &s); err != nil {
			return nil, fmt.Errorf("LoadSnapshots: parse %s: %w", e.Name(), err)
		}
		if s.Cloud == "" {
			return nil, fmt.Errorf("LoadSnapshots: %s missing 'cloud'", e.Name())
		}
		out[s.Cloud] = &s
	}
	return out, nil
}

// CanonicalKey flattens a PricingKey-shaped map to a deterministic string.
// Drops empty values, sorts keys, joins as k1=v1;k2=v2;...
func CanonicalKey(key map[string]any) string {
	keys := make([]string, 0, len(key))
	for k, v := range key {
		if v == nil {
			continue
		}
		s := fmt.Sprintf("%v", v)
		if s == "" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", k, key[k]))
	}
	return strings.Join(parts, ";")
}
