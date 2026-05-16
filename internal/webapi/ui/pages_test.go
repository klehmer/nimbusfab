package ui_test

import (
	"io/fs"
	"testing"

	"github.com/klehmer/nimbusfab/internal/webapi/ui"
	"github.com/klehmer/nimbusfab/pkg/inventory"
)

func TestNewRenderer_ParsesTemplates(t *testing.T) {
	r, err := ui.NewRenderer(inventory.NewNullRepo(), "default")
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}
	if r == nil {
		t.Fatal("renderer is nil")
	}
}

func TestAssetsFS_ContainsStylesheet(t *testing.T) {
	assets, err := ui.AssetsFS()
	if err != nil {
		t.Fatalf("AssetsFS: %v", err)
	}
	data, err := fs.ReadFile(assets, "style.css")
	if err != nil {
		t.Fatalf("read style.css: %v", err)
	}
	if len(data) == 0 {
		t.Error("style.css is empty")
	}
}
