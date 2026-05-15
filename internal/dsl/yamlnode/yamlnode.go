// Package yamlnode wraps gopkg.in/yaml.v3 to give every decoded value a
// Source pointing back at the YAML file and line it came from. The loader
// uses this to attribute validator diagnostics; consumers see the same
// IR types but can ask "where did this field come from?".
package yamlnode

import (
	"errors"
	"fmt"

	"gopkg.in/yaml.v3"

	"github.com/klehmer/nimbusfab/pkg/ir"
)

// Document is a decoded YAML document plus a top-level provenance map.
// For the v1 implementation we only track provenance for the top-level
// fields of the document; nested provenance is added in Phase 2.
type Document struct {
	APIVersion string
	Name       string
	Raw        *yaml.Node // root MappingNode, accessible for deeper decoding

	source string               // file path
	prov   map[string]ir.Source // top-level field -> source
}

// SourceFor returns the Source attached to the named top-level field.
// If the field was not present, an empty Source is returned.
func (d *Document) SourceFor(field string) ir.Source {
	return d.prov[field]
}

// Error wraps a yaml.v3 parse error with a Source so callers can attribute
// it to a file:line.
type Error struct {
	Source ir.Source
	Err    error
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s: %v", e.Source, e.Err)
}

func (e *Error) Unwrap() error { return e.Err }

// Decode parses a YAML document and returns a Document. The filename is used
// only for provenance; the bytes are the actual YAML body.
func Decode(filename string, body []byte) (*Document, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(body, &root); err != nil {
		// yaml.v3 errors include line numbers in their text but not as fields;
		// best-effort extraction:
		return nil, &Error{
			Source: ir.Source{File: filename},
			Err:    err,
		}
	}
	if root.Kind != yaml.DocumentNode || len(root.Content) == 0 {
		return nil, &Error{
			Source: ir.Source{File: filename},
			Err:    errors.New("empty document"),
		}
	}
	body0 := root.Content[0]
	if body0.Kind != yaml.MappingNode {
		return nil, &Error{
			Source: ir.Source{File: filename, Line: body0.Line, Column: body0.Column},
			Err:    fmt.Errorf("expected mapping at document root, got %v", kindString(body0.Kind)),
		}
	}

	doc := &Document{
		Raw:    body0,
		source: filename,
		prov:   map[string]ir.Source{},
	}

	for i := 0; i < len(body0.Content); i += 2 {
		keyNode := body0.Content[i]
		valNode := body0.Content[i+1]
		doc.prov[keyNode.Value] = ir.Source{
			File:   filename,
			Line:   keyNode.Line,
			Column: keyNode.Column,
		}
		switch keyNode.Value {
		case "apiVersion":
			doc.APIVersion = valNode.Value
		case "name":
			doc.Name = valNode.Value
		}
	}
	return doc, nil
}

func kindString(k yaml.Kind) string {
	switch k {
	case yaml.DocumentNode:
		return "document"
	case yaml.SequenceNode:
		return "sequence"
	case yaml.MappingNode:
		return "mapping"
	case yaml.ScalarNode:
		return "scalar"
	case yaml.AliasNode:
		return "alias"
	default:
		return fmt.Sprintf("kind%d", k)
	}
}
