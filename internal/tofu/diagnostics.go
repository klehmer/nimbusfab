package tofu

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"
)

// Error codes the runner emits to engine.
const (
	ErrTofuStateLocked     = "ErrTofuStateLocked"
	ErrTofuCredsMissing    = "ErrTofuCredsMissing"
	ErrTofuProviderMissing = "ErrTofuProviderMissing"
	ErrTofuVersionMismatch = "ErrTofuVersionMismatch"
	ErrTofuDiagnostic      = "ErrTofuDiagnostic"
	ErrTofuOpaque          = "ErrTofuOpaque"
)

// Diagnostic is the structured form of a single Tofu diagnostic event.
type Diagnostic struct {
	Severity string
	Summary  string
	Detail   string
	Address  string
	Range    *Range
	Code     string
	Raw      map[string]any
}

type Range struct {
	Filename string
	Start    Position
	End      Position
}

type Position struct {
	Line   int
	Column int
	Byte   int
}

// ParseDiagnostic reads the FIRST JSON object from the reader and maps it to
// a Diagnostic. Used in tests and by ParseStream.
func ParseDiagnostic(r io.Reader) (Diagnostic, error) {
	var raw map[string]any
	if err := json.NewDecoder(r).Decode(&raw); err != nil {
		return Diagnostic{}, err
	}
	return mapEvent(raw), nil
}

// ParseStream reads newline-delimited JSON events from `r` and emits typed
// Diagnostics on the returned channel. The channel closes when r EOFs.
func ParseStream(r io.Reader) <-chan Diagnostic {
	ch := make(chan Diagnostic, 64)
	go func() {
		defer close(ch)
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 1<<20), 1<<24)
		for sc.Scan() {
			var raw map[string]any
			if err := json.Unmarshal(sc.Bytes(), &raw); err != nil {
				continue
			}
			ch <- mapEvent(raw)
		}
	}()
	return ch
}

func mapEvent(raw map[string]any) Diagnostic {
	d := Diagnostic{Raw: raw}
	if t, _ := raw["type"].(string); t != "diagnostic" {
		return d
	}
	body, _ := raw["diagnostic"].(map[string]any)
	d.Severity, _ = body["severity"].(string)
	d.Summary, _ = body["summary"].(string)
	d.Detail, _ = body["detail"].(string)
	d.Address, _ = body["address"].(string)
	d.Code = classify(d.Summary, d.Detail)
	return d
}

func classify(summary, detail string) string {
	s := strings.ToLower(summary)
	switch {
	case strings.Contains(s, "state lock"):
		return ErrTofuStateLocked
	case strings.Contains(s, "credentials"):
		return ErrTofuCredsMissing
	case strings.Contains(s, "provider") && strings.Contains(s, "not"):
		return ErrTofuProviderMissing
	case strings.Contains(s, "version"):
		return ErrTofuVersionMismatch
	case s == "":
		return ""
	default:
		return ErrTofuDiagnostic
	}
}
