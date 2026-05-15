package provisioner

import "testing"

func TestCanonicalJSON_SortsMapKeys(t *testing.T) {
	in := map[string]any{
		"z": 1,
		"a": map[string]any{"y": 2, "b": 3},
	}
	got, err := canonicalJSON(in)
	if err != nil {
		t.Fatalf("canonicalJSON: %v", err)
	}
	want := `{"a":{"b":3,"y":2},"z":1}`
	if string(got) != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestCanonicalJSON_DeterministicAcrossRuns(t *testing.T) {
	in := map[string]any{"k1": "v1", "k2": "v2", "k3": map[string]any{"a": 1, "b": 2}}
	a, _ := canonicalJSON(in)
	b, _ := canonicalJSON(in)
	if string(a) != string(b) {
		t.Errorf("nondeterministic output:\n%s\nvs\n%s", a, b)
	}
}

func TestCanonicalJSON_EmptyContainers(t *testing.T) {
	a, _ := canonicalJSON(map[string]any{})
	if string(a) != "{}" {
		t.Errorf("empty map -> %s, want {}", a)
	}
	b, _ := canonicalJSON([]any{})
	if string(b) != "[]" {
		t.Errorf("empty list -> %s, want []", b)
	}
}

func TestCanonicalJSON_StringSliceTreatedAsList(t *testing.T) {
	in := []string{"b", "a"}
	got, _ := canonicalJSON(in)
	if string(got) != `["b","a"]` {
		t.Errorf("got %s, want [\"b\",\"a\"]", got)
	}
}
