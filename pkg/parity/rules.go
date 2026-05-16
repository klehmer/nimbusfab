package parity

import (
	"context"
	"fmt"
)

// EvaluateRules applies the project's parity rules to a report.
func (e *engine) EvaluateRules(ctx context.Context, report *ParityReport, rules ProjectRules) ([]Violation, error) {
	if rules.Default.Mode == "" && len(rules.Components) == 0 {
		return nil, nil
	}
	compRule, hasCompRule := rules.Components[report.Component]
	mode := rules.Default.Mode
	minScore := rules.Default.MinScore
	if hasCompRule {
		if compRule.Mode != "" {
			mode = compRule.Mode
		}
		if compRule.MinScore > 0 {
			minScore = compRule.MinScore
		}
	}
	if mode == "off" {
		return nil, nil
	}
	var out []Violation
	if minScore > 0 && report.Score < minScore {
		out = append(out, Violation{
			Component: report.Component, Policy: "minScore",
			Detail: fmt.Sprintf("score %.2f below minScore %.2f", report.Score, minScore),
			Action: actionFromMode(mode),
		})
	}
	if hasCompRule {
		for attrName, policy := range compRule.Attributes {
			if v := violationForAttr(report, attrName, policy); v != nil {
				v.Component = report.Component
				v.Action = actionFromMode(mode)
				out = append(out, *v)
			}
		}
	}
	return out, nil
}

func actionFromMode(mode string) string {
	if mode == "block" {
		return "block"
	}
	return "warn"
}

func violationForAttr(report *ParityReport, attrName string, policy AttributePolicy) *Violation {
	var cmp *AttrComparison
	for i := range report.Comparisons {
		if report.Comparisons[i].Attribute == attrName {
			cmp = &report.Comparisons[i]
			break
		}
	}
	if cmp == nil {
		return nil
	}
	switch policy.Policy {
	case "exact":
		if !cmp.AllMatch {
			return &Violation{Attribute: attrName, Policy: "exact",
				Detail: fmt.Sprintf("values differ: %v", cmp.Values)}
		}
	case "maxRatio":
		if cmp.Kind != "numeric" {
			return &Violation{Attribute: attrName, Policy: "maxRatio",
				Detail: "maxRatio only applies to numeric attributes"}
		}
		max, okMax := cmp.MaxValue.(float64)
		min, okMin := cmp.MinValue.(float64)
		if okMax && okMin && min > 0 {
			ratio := max / min
			if ratio > policy.MaxRatio {
				return &Violation{Attribute: attrName, Policy: "maxRatio",
					Detail: fmt.Sprintf("ratio %.2f exceeds maxRatio %.2f", ratio, policy.MaxRatio)}
			}
		}
	case "requireAll":
		for cloud, v := range cmp.Values {
			if b, ok := v.(bool); !ok || !b {
				return &Violation{Attribute: attrName, Policy: "requireAll",
					Detail: fmt.Sprintf("%s: %v (requireAll wants true everywhere)", cloud, v)}
			}
		}
	}
	return nil
}
