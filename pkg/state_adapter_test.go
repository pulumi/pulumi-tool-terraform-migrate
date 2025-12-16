package pkg

import (
	"os"
	"testing"

	"github.com/hexops/autogold/v2"
)

func TestConvertSimple(t *testing.T) {
	if os.Getenv("CI") == "true" {
		t.Skip("Skipping test in CI: TODO: set up pulumi credentials in CI")
	}
	data, _, err := TranslateState("testdata/bucket_state.json", "")
	if err != nil {
		t.Fatalf("failed to convert Terraform state: %v", err)
	}

	autogold.ExpectFile(t, data)
}

func TestConvertWithDependencies(t *testing.T) {
	if os.Getenv("CI") == "true" {
		t.Skip("Skipping test in CI: TODO: set up pulumi credentials in CI")
	}
	_, dependencies, err := TranslateState("testdata/bucket_state.json", "")
	if err != nil {
		t.Fatalf("failed to convert Terraform state: %v", err)
	}

	autogold.Expect(``).Equal(t, dependencies)
}

func TestConvertInvolved(t *testing.T) {
	if os.Getenv("CI") == "true" {
		t.Skip("Skipping test in CI: TODO: set up pulumi credentials in CI")
	}
	data, _, err := TranslateState("testdata/tofu_state.json", "")
	if err != nil {
		t.Fatalf("failed to convert Terraform state: %v", err)
	}

	autogold.ExpectFile(t, data)
}
