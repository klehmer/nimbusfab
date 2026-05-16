package aws_test

import (
	"context"
	"testing"

	"github.com/klehmer/nimbusfab/internal/cloud/aws"
	"github.com/klehmer/nimbusfab/pkg/cloud"
	"github.com/klehmer/nimbusfab/pkg/ir"
)

func TestEmitStorage_BasicShape(t *testing.T) {
	a := aws.New()
	target := ir.DeploymentTarget{
		Cloud: "aws", Region: "us-east-1",
		Spec: map[string]any{"__type": "storage", "__component": "uploads"},
	}
	prims, err := a.Emit(context.Background(), target, cloud.ResolvedRefs{})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	var bucket, versioning, pab, enc int
	for _, p := range prims {
		switch p.TofuType {
		case "aws_s3_bucket":
			bucket++
		case "aws_s3_bucket_versioning":
			versioning++
		case "aws_s3_bucket_public_access_block":
			pab++
		case "aws_s3_bucket_server_side_encryption_configuration":
			enc++
		}
	}
	if bucket != 1 || versioning != 1 || pab != 1 || enc != 1 {
		t.Errorf("primitive counts: bucket=%d versioning=%d pab=%d enc=%d", bucket, versioning, pab, enc)
	}
}

func TestEmitStorage_BucketNameDeterministic(t *testing.T) {
	a := aws.New()
	target := ir.DeploymentTarget{
		Cloud: "aws", Region: "us-east-1",
		Spec: map[string]any{"__type": "storage", "__component": "uploads"},
	}
	p1, _ := a.Emit(context.Background(), target, cloud.ResolvedRefs{})
	p2, _ := a.Emit(context.Background(), target, cloud.ResolvedRefs{})
	var n1, n2 string
	for _, p := range p1 {
		if p.TofuType == "aws_s3_bucket" {
			n1, _ = p.Attributes["bucket"].(string)
		}
	}
	for _, p := range p2 {
		if p.TofuType == "aws_s3_bucket" {
			n2, _ = p.Attributes["bucket"].(string)
		}
	}
	if n1 == "" || n1 != n2 {
		t.Errorf("bucket name non-deterministic: %q vs %q", n1, n2)
	}
}

func TestEmitStorage_ExplicitName(t *testing.T) {
	a := aws.New()
	target := ir.DeploymentTarget{
		Cloud: "aws", Region: "us-east-1",
		Spec: map[string]any{"__type": "storage", "__component": "uploads", "name": "my-explicit-bucket"},
	}
	prims, _ := a.Emit(context.Background(), target, cloud.ResolvedRefs{})
	for _, p := range prims {
		if p.TofuType == "aws_s3_bucket" {
			if p.Attributes["bucket"] != "my-explicit-bucket" {
				t.Errorf("bucket = %v, want my-explicit-bucket", p.Attributes["bucket"])
			}
		}
	}
}

func TestEmitStorage_PublicAccessAllowed(t *testing.T) {
	a := aws.New()
	target := ir.DeploymentTarget{
		Cloud: "aws", Region: "us-east-1",
		Spec: map[string]any{"__type": "storage", "__component": "uploads", "publicAccess": "allowed"},
	}
	prims, _ := a.Emit(context.Background(), target, cloud.ResolvedRefs{})
	for _, p := range prims {
		if p.TofuType == "aws_s3_bucket_public_access_block" {
			if p.Attributes["block_public_acls"] != false {
				t.Errorf("publicAccess=allowed should set block_public_acls=false; got %v", p.Attributes["block_public_acls"])
			}
		}
	}
}
