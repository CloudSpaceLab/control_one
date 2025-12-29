package provisioning

import (
	"os"
	"testing"
)

func TestDetectProviderPrefersAWSRegionEnv(t *testing.T) {
	t.Setenv("AWS_REGION", "eu-central-1")
	t.Setenv("AWS_DEFAULT_REGION", "")
	provider, metadata := DetectProvider()
	if provider != "aws" {
		t.Fatalf("expected aws provider, got %s", provider)
	}
	if metadata["region"] != "eu-central-1" {
		t.Fatalf("expected region metadata, got %v", metadata)
	}
}

func TestDetectProviderFallsBackToDefaultRegion(t *testing.T) {
	os.Unsetenv("AWS_REGION")
	t.Setenv("AWS_DEFAULT_REGION", "us-west-1")
	provider, metadata := DetectProvider()
	if provider != "aws" {
		t.Fatalf("expected aws provider via default region, got %s", provider)
	}
	if metadata["region"] != "us-west-1" {
		t.Fatalf("expected default region metadata, got %v", metadata)
	}
}
