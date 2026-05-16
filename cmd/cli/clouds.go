package main

import (
	"github.com/klehmer/nimbusfab/internal/cloud/aws"
	"github.com/klehmer/nimbusfab/internal/cloud/azure"
	"github.com/klehmer/nimbusfab/internal/cloud/gcp"
	"github.com/klehmer/nimbusfab/pkg/cloud"
)

// defaultCloudRegistry returns a Registry populated with every in-tree adapter.
// Callers should use this rather than building per-command registries inline,
// so adding a new cloud requires only one edit.
func defaultCloudRegistry() (cloud.Registry, error) {
	reg := cloud.NewRegistry()
	if err := reg.Register(aws.New()); err != nil {
		return nil, err
	}
	if err := reg.Register(azure.New()); err != nil {
		return nil, err
	}
	if err := reg.Register(gcp.New()); err != nil {
		return nil, err
	}
	return reg, nil
}
