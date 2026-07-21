package noforkscope

import (
	"os"
	"testing"
)

func TestReviewGenerateRealProvider(t *testing.T) {
	providerDir := os.Getenv("REVIEW_PROVIDER")
	if providerDir == "" {
		t.Skip("set REVIEW_PROVIDER to a prepared terraform-provider-oci directory")
	}

	_, err := Generate(providerDir, "", map[string][]string{
		"compute": {"oci_core_instance"},
	})
	if err != nil {
		t.Fatal(err)
	}
}
