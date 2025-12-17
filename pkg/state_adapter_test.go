package pkg

import (
	"os"
	"testing"

	"github.com/hexops/autogold/v2"
)

func TestConvertSimple(t *testing.T) {
	t.Parallel()
	if os.Getenv("CI") == "true" {
		t.Skip("Skipping test in CI: TODO: set up pulumi credentials in CI")
	}
	data, err := TranslateState("testdata/bucket_state.json", "")
	if err != nil {
		t.Fatalf("failed to convert Terraform state: %v", err)
	}

	autogold.ExpectFile(t, data)
}

func TestConvertInvolved(t *testing.T) {
	t.Parallel()
	if os.Getenv("CI") == "true" {
		t.Skip("Skipping test in CI: TODO: set up pulumi credentials in CI")
	}
	data, err := TranslateState("testdata/tofu_state.json", "")
	if err != nil {
		t.Fatalf("failed to convert Terraform state: %v", err)
	}

	autogold.ExpectFile(t, data)
}