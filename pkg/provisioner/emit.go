package provisioner

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
)

// canonicalJSON marshals v with all map keys sorted alphabetically at every
// nesting depth. Used everywhere the provisioner serializes Tofu workspace
// JSON so the output is byte-stable across runs and processes.
func canonicalJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	if err := writeCanonical(&buf, v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeCanonical(buf *bytes.Buffer, v any) error {
	switch t := v.(type) {
	case map[string]any:
		return writeMap(buf, t)
	case []any:
		return writeSlice(buf, t)
	case []string:
		s := make([]any, len(t))
		for i, x := range t {
			s[i] = x
		}
		return writeSlice(buf, s)
	case nil:
		buf.WriteString("null")
		return nil
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return fmt.Errorf("canonicalJSON: %w", err)
		}
		buf.Write(b)
		return nil
	}
}

func writeMap(buf *bytes.Buffer, m map[string]any) error {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	buf.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		kb, _ := json.Marshal(k)
		buf.Write(kb)
		buf.WriteByte(':')
		if err := writeCanonical(buf, m[k]); err != nil {
			return err
		}
	}
	buf.WriteByte('}')
	return nil
}

func writeSlice(buf *bytes.Buffer, s []any) error {
	buf.WriteByte('[')
	for i, x := range s {
		if i > 0 {
			buf.WriteByte(',')
		}
		if err := writeCanonical(buf, x); err != nil {
			return err
		}
	}
	buf.WriteByte(']')
	return nil
}
