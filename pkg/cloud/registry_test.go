package cloud_test

import (
	"testing"

	"github.com/klehmer/nimbusfab/pkg/cloud"
)

func TestRegistry_RegisterAndGet(t *testing.T) {
	r := cloud.NewRegistry()
	fake := cloud.NewFakeAdapter("aws")

	if err := r.Register(fake); err != nil {
		t.Fatalf("Register: %v", err)
	}
	got, ok := r.Get("aws")
	if !ok {
		t.Fatal("Get(\"aws\"): ok=false, want true")
	}
	if got.Name() != "aws" {
		t.Errorf("Get(\"aws\").Name() = %q, want \"aws\"", got.Name())
	}
}

func TestRegistry_DuplicateRegister(t *testing.T) {
	r := cloud.NewRegistry()
	a := cloud.NewFakeAdapter("aws")
	b := cloud.NewFakeAdapter("aws")
	if err := r.Register(a); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	if err := r.Register(b); err == nil {
		t.Fatal("duplicate Register: nil err, want non-nil")
	}
}

func TestRegistry_GetUnknown(t *testing.T) {
	r := cloud.NewRegistry()
	if _, ok := r.Get("nowhere"); ok {
		t.Fatal("Get(\"nowhere\"): ok=true, want false")
	}
}

func TestRegistry_ListIsAlphabetical(t *testing.T) {
	r := cloud.NewRegistry()
	_ = r.Register(cloud.NewFakeAdapter("gcp"))
	_ = r.Register(cloud.NewFakeAdapter("aws"))
	_ = r.Register(cloud.NewFakeAdapter("azure"))
	list := r.List()
	if len(list) != 3 {
		t.Fatalf("List len = %d, want 3", len(list))
	}
	want := []string{"aws", "azure", "gcp"}
	for i := range want {
		if list[i].Name() != want[i] {
			t.Errorf("List[%d].Name() = %q, want %q", i, list[i].Name(), want[i])
		}
	}
}

func TestRegistry_RegisterNil(t *testing.T) {
	r := cloud.NewRegistry()
	if err := r.Register(nil); err == nil {
		t.Error("Register(nil): nil err, want non-nil")
	}
}
