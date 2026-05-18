package upstream

import (
	"errors"
	"strings"
	"testing"

	"github.com/klehmer/nimbusfab/pkg/ir"
)

func TestPair_ExactMatch(t *testing.T) {
	all := []TargetIdent{
		{Component: "net", Cloud: "aws", Region: "us-east-1"},
		{Component: "net", Cloud: "aws", Region: "us-west-2"},
		{Component: "app", Cloud: "aws", Region: "us-east-1"},
	}
	got, err := Pair(TargetIdent{Component: "app", Cloud: "aws", Region: "us-east-1"}, "net", all)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if got.Region != "us-east-1" {
		t.Fatalf("got region %s", got.Region)
	}
}

func TestPair_CrossRegionFails(t *testing.T) {
	all := []TargetIdent{
		{Component: "net", Cloud: "aws", Region: "us-east-1"},
		{Component: "app", Cloud: "aws", Region: "us-west-2"},
	}
	_, err := Pair(TargetIdent{Component: "app", Cloud: "aws", Region: "us-west-2"}, "net", all)
	if !errors.Is(err, ErrCrossTargetRefUnsupported) {
		t.Fatalf("got %v, want ErrCrossTargetRefUnsupported", err)
	}
}

func TestPair_CrossCloudFails(t *testing.T) {
	all := []TargetIdent{
		{Component: "net", Cloud: "aws", Region: "us-east-1"},
		{Component: "app", Cloud: "azure", Region: "us-east-1"},
	}
	_, err := Pair(TargetIdent{Component: "app", Cloud: "azure", Region: "us-east-1"}, "net", all)
	if !errors.Is(err, ErrCrossTargetRefUnsupported) {
		t.Fatalf("got %v, want ErrCrossTargetRefUnsupported", err)
	}
}

func TestToposortTargets(t *testing.T) {
	components := []ir.Component{
		{Name: "app", Refs: []ir.ComponentRef{{Component: "net", Output: "vpc_id", As: "v"}}},
		{Name: "net"},
	}
	targets := []TargetIdent{
		{Component: "app", Cloud: "aws", Region: "us-east-1"},
		{Component: "net", Cloud: "aws", Region: "us-east-1"},
		{Component: "app", Cloud: "azure", Region: "eastus"},
		{Component: "net", Cloud: "azure", Region: "eastus"},
	}
	got, err := ToposortTargets(targets, components)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	netIdx, appIdx := -1, -1
	for i, ti := range got {
		switch ti.Component {
		case "net":
			netIdx = i
		case "app":
			if appIdx == -1 {
				appIdx = i
			}
		}
	}
	if netIdx >= appIdx {
		var lines []string
		for _, ti := range got {
			lines = append(lines, ti.Component+"/"+ti.Cloud)
		}
		t.Fatalf("expected all net targets before app targets, got %s", strings.Join(lines, ","))
	}
}
