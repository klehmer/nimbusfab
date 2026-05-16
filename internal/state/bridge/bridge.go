// Package bridge parses `tofu show -json` output into the provisioner's
// StateSnapshot type. It does NOT touch the inventory database — that lives
// in the inventory persistence phase. Bridge is pure: given JSON bytes,
// return a typed snapshot.
package bridge

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/klehmer/nimbusfab/pkg/provisioner"
)

// Parse turns `tofu show -json` raw bytes into a StateSnapshot.
// DeploymentTargetID is left empty — the caller fills it from context.
func Parse(raw []byte) (*provisioner.StateSnapshot, error) {
	var doc struct {
		FormatVersion    string `json:"format_version"`
		TerraformVersion string `json:"terraform_version"`
		Serial           int64  `json:"serial"`
		Values           struct {
			Outputs map[string]struct {
				Value any `json:"value"`
				Type  any `json:"type"`
			} `json:"outputs"`
			RootModule struct {
				Resources []struct {
					Address string         `json:"address"`
					Type    string         `json:"type"`
					Name    string         `json:"name"`
					Values  map[string]any `json:"values"`
				} `json:"resources"`
			} `json:"root_module"`
		} `json:"values"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("bridge.Parse: %w", err)
	}
	snap := &provisioner.StateSnapshot{
		TofuVersion:  doc.TerraformVersion,
		SerialNumber: doc.Serial,
		Outputs:      map[string]any{},
		CapturedAt:   time.Now().UTC(),
	}
	for k, v := range doc.Values.Outputs {
		snap.Outputs[k] = v.Value
	}
	for _, r := range doc.Values.RootModule.Resources {
		snap.Resources = append(snap.Resources, provisioner.StateResource{
			Address:         r.Address,
			Type:            r.Type,
			Name:            r.Name,
			CloudResourceID: cloudResourceID(r.Values),
			AttributesHash:  hashAttributes(r.Values),
			Attributes:      r.Values,
		})
	}
	sort.Slice(snap.Resources, func(i, j int) bool {
		return snap.Resources[i].Address < snap.Resources[j].Address
	})
	return snap, nil
}

func cloudResourceID(attrs map[string]any) string {
	for _, key := range []string{"arn", "id", "self_link"} {
		if v, ok := attrs[key].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

func hashAttributes(attrs map[string]any) string {
	b := canonical(attrs)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func canonical(v any) []byte {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		buf := []byte{'{'}
		for i, k := range keys {
			if i > 0 {
				buf = append(buf, ',')
			}
			kb, _ := json.Marshal(k)
			buf = append(buf, kb...)
			buf = append(buf, ':')
			buf = append(buf, canonical(t[k])...)
		}
		buf = append(buf, '}')
		return buf
	case []any:
		buf := []byte{'['}
		for i, x := range t {
			if i > 0 {
				buf = append(buf, ',')
			}
			buf = append(buf, canonical(x)...)
		}
		buf = append(buf, ']')
		return buf
	default:
		b, _ := json.Marshal(t)
		return b
	}
}
