package parity

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

// RenderText prints a human-readable parity report to w.
func RenderText(w io.Writer, report *ParityReport) {
	fmt.Fprintf(w, "Component: %s  (%s", report.Component, report.Type)
	if report.Size != "" {
		fmt.Fprintf(w, ", size=%s", report.Size)
	}
	fmt.Fprintln(w, ")")
	if report.Contract.Type != "" {
		fmt.Fprintln(w, contractLine(report.Contract))
	}
	if len(report.Targets) > 1 {
		renderComparisonTable(w, report)
	} else if len(report.Targets) == 1 {
		renderSingleTarget(w, report.Targets[0])
	}
	fmt.Fprintf(w, "\nParity score: %.2f", report.Score)
	if report.Score >= 0.95 {
		fmt.Fprint(w, "  (excellent)")
	} else if report.Score >= 0.7 {
		fmt.Fprint(w, "  (good)")
	} else {
		fmt.Fprint(w, "  (divergent)")
	}
	fmt.Fprintln(w)
	if len(report.Warnings) > 0 {
		fmt.Fprintln(w, "Warnings:")
		for _, w2 := range report.Warnings {
			fmt.Fprintf(w, "  - %s\n", w2)
		}
	}
}

// RenderViolations prints violations one per line.
func RenderViolations(w io.Writer, violations []Violation) {
	if len(violations) == 0 {
		fmt.Fprintln(w, "Rule violations: none")
		return
	}
	fmt.Fprintf(w, "Rule violations (%d):\n", len(violations))
	for _, v := range violations {
		attr := v.Attribute
		if attr == "" {
			attr = "(component)"
		}
		fmt.Fprintf(w, "  [%s] %s %s/%s: %s\n", v.Action, v.Component, attr, v.Policy, v.Detail)
	}
}

func contractLine(f ContractFloor) string {
	parts := []string{}
	if f.Compute != nil {
		parts = append(parts, fmt.Sprintf("vCPU>=%d, RAM>=%v GiB", f.Compute.MinVCPU, f.Compute.MinMemoryGB))
	}
	if f.Storage != nil && f.Storage.MinSizeGB > 0 {
		parts = append(parts, fmt.Sprintf("storage>=%d GiB", f.Storage.MinSizeGB))
	}
	if len(f.Features) > 0 {
		feats := []string{}
		for k := range f.Features {
			feats = append(feats, k)
		}
		sort.Strings(feats)
		parts = append(parts, "requires: "+strings.Join(feats, ", "))
	}
	return "Contract floor: " + strings.Join(parts, "; ")
}

func renderSingleTarget(w io.Writer, t TargetProfile) {
	fmt.Fprintf(w, "\nTarget %s/%s  SKU=%s\n", t.Cloud, t.Region, t.Profile.SKU)
}

func renderComparisonTable(w io.Writer, r *ParityReport) {
	targets := r.Targets
	fmt.Fprintln(w)
	headers := []string{"Attribute"}
	for _, t := range targets {
		headers = append(headers, t.Cloud+"/"+t.Region)
	}
	rows := [][]string{headers}
	for _, c := range r.Comparisons {
		row := []string{c.Attribute}
		for _, t := range targets {
			v := c.Values[t.Cloud+"/"+t.Region]
			row = append(row, fmt.Sprintf("%v", v))
		}
		rows = append(rows, row)
	}
	printAlignedRows(w, rows)
}

func printAlignedRows(w io.Writer, rows [][]string) {
	if len(rows) == 0 {
		return
	}
	widths := make([]int, len(rows[0]))
	for _, row := range rows {
		for i, cell := range row {
			if len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}
	for _, row := range rows {
		for i, cell := range row {
			fmt.Fprintf(w, "%-*s  ", widths[i], cell)
		}
		fmt.Fprintln(w)
	}
}
