package validator

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v5"

	"github.com/klehmer/nimbusfab/pkg/ir"
)

// phase3SchemaImpl validates the *ir.Project against the embedded JSON
// Schema and translates schema errors into ir.Issues.
func phase3SchemaImpl(proj *ir.Project, report *ir.ValidationReport) error {
	schemaBytes, err := ir.EmbeddedSchema("project.json")
	if err != nil {
		return fmt.Errorf("load embedded project schema: %w", err)
	}

	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("project.json", strings.NewReader(string(schemaBytes))); err != nil {
		return fmt.Errorf("add schema: %w", err)
	}
	schema, err := compiler.Compile("project.json")
	if err != nil {
		return fmt.Errorf("compile schema: %w", err)
	}

	// Marshal the project to a JSON map so the validator can walk it.
	docBytes, err := json.Marshal(proj)
	if err != nil {
		return fmt.Errorf("marshal project: %w", err)
	}
	var doc any
	if err := json.Unmarshal(docBytes, &doc); err != nil {
		return fmt.Errorf("unmarshal project: %w", err)
	}

	// Go's encoding/json does not treat empty strings or empty structs as
	// absent the way YAML does, so a zero-value `Name` or embedded
	// `StateBackend` would surface as a pattern/enum violation rather than
	// the more informative "required" or just-not-present check. Prune
	// empty-string properties and empty objects before handing the document
	// to the schema validator so that "missing" means missing.
	doc = pruneEmpty(doc)

	if err := schema.Validate(doc); err != nil {
		appendSchemaIssues(report, err)
	}
	return nil
}

// mapValuedFields names the IR struct properties whose JSON value is itself
// a free-form map. For these, an empty `{}` is a legitimate value (an empty
// stack body, a spec with no overrides) and pruneEmpty must leave them
// alone. Any other empty `{}` is zero-value pollution from Go's
// encoding/json (struct values do not honour `omitempty`) and gets stripped
// so the schema's `required` keyword can fire correctly.
var mapValuedFields = map[string]bool{
	"stacks":     true,
	"vars":       true,
	"config":     true,
	"spec":       true,
	"schema":     true,
	"attributes": true,
	"tags":       true,
}

// pruneEmpty recursively strips object properties whose value is an empty
// string or a struct that marshaled as `{}` despite `omitempty`. This
// collapses Go zero-value emissions into "field absent" so JSON Schema's
// `required` keyword fires when callers genuinely forgot to set a value.
// Empty maps that are themselves a typed map field (stacks, vars, config,
// spec, schema, attributes, tags) are preserved because their emptiness is
// semantically meaningful, not a serialization artifact.
func pruneEmpty(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, vv := range x {
			pruned := pruneEmptyAt(vv, k)
			if shouldDrop(pruned, k) {
				delete(x, k)
				continue
			}
			x[k] = pruned
		}
		return x
	case []any:
		out := make([]any, 0, len(x))
		for _, item := range x {
			out = append(out, pruneEmpty(item))
		}
		return out
	default:
		return v
	}
}

// pruneEmptyAt recurses into v knowing the key under which v lives. For
// keys named in mapValuedFields the children are map values: their
// emptiness is preserved (so an `{}` stack body stays `{}`), but their
// own children still get pruned.
func pruneEmptyAt(v any, key string) any {
	if !mapValuedFields[key] {
		return pruneEmpty(v)
	}
	m, ok := v.(map[string]any)
	if !ok {
		return pruneEmpty(v)
	}
	for k, vv := range m {
		m[k] = pruneEmpty(vv)
	}
	return m
}

func shouldDrop(v any, key string) bool {
	switch x := v.(type) {
	case string:
		return x == ""
	case map[string]any:
		return len(x) == 0 && !mapValuedFields[key]
	}
	return false
}

// appendSchemaIssues walks a jsonschema.ValidationError and emits one Issue
// per terminal cause. Code mapping is best-effort: "required" -> Required,
// "pattern" -> Pattern, anything else -> Generic.
func appendSchemaIssues(report *ir.ValidationReport, err error) {
	ve, ok := err.(*jsonschema.ValidationError)
	if !ok {
		report.Issues = append(report.Issues, ir.Issue{
			Severity: ir.SeverityError,
			Code:     "ErrSchemaGeneric",
			Message:  err.Error(),
		})
		return
	}
	for _, leaf := range collectLeaves(ve) {
		code := classifySchemaError(leaf.KeywordLocation)
		report.Issues = append(report.Issues, ir.Issue{
			Severity: ir.SeverityError,
			Code:     code,
			Message:  leaf.Message,
			Path:     pointerToPath(leaf.InstanceLocation),
		})
	}
}

func collectLeaves(ve *jsonschema.ValidationError) []*jsonschema.ValidationError {
	if len(ve.Causes) == 0 {
		return []*jsonschema.ValidationError{ve}
	}
	var out []*jsonschema.ValidationError
	for _, c := range ve.Causes {
		out = append(out, collectLeaves(c)...)
	}
	return out
}

func classifySchemaError(keywordLoc string) string {
	switch {
	case strings.HasSuffix(keywordLoc, "/required"):
		return "ErrSchemaRequiredField"
	case strings.HasSuffix(keywordLoc, "/pattern"):
		return "ErrSchemaPattern"
	case strings.HasSuffix(keywordLoc, "/enum"):
		return "ErrSchemaEnum"
	case strings.HasSuffix(keywordLoc, "/type"):
		return "ErrSchemaType"
	case strings.HasSuffix(keywordLoc, "/maxLength"):
		return "ErrSchemaTooLong"
	case strings.HasSuffix(keywordLoc, "/additionalProperties"):
		return "WarnUnknownField"
	default:
		return "ErrSchemaGeneric"
	}
}

func pointerToPath(ptr string) string {
	// "/components/0/name" -> "components[0].name"
	if ptr == "" || ptr == "/" {
		return ""
	}
	parts := strings.Split(strings.TrimPrefix(ptr, "/"), "/")
	out := ""
	for i, p := range parts {
		if isDigits(p) {
			out += "[" + p + "]"
		} else {
			if i > 0 {
				out += "."
			}
			out += p
		}
	}
	return out
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}
