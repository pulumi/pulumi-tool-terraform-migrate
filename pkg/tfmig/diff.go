package tfmig

import (
	"context"
	"fmt"

	tfjson "github.com/hashicorp/terraform-json"
	"github.com/pulumi/pulumi-terraform-migrate/pkg/pulumix"
	"github.com/pulumi/pulumi/sdk/v3/go/auto"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// Current status of a resource relative to the migration process.
type ResourceStatus interface {
	isResourceStatus()
}

// Explicitly ignored for migration per migration.json
type ResourceSkipped struct{}

func (ResourceSkipped) isResourceStatus() {}

var _ ResourceStatus = (*ResourceSkipped)(nil)

// Exists in Terraform but not in migration.json yet.
type ResourceNotTracked struct{}

func (ResourceNotTracked) isResourceStatus() {}

var _ ResourceStatus = (*ResourceNotTracked)(nil)

// Sources have been translated to Pulumi and the resource has an URN.
type ResourceTranslated struct {
	URN              pulumi.URN
	TranslatedStatus TranslatedStatus
}

type TranslatedStatus string

const (
	TranslatedStatusNoState      TranslatedStatus = "no-state"
	TranslatedStatusNeedsUpdate  TranslatedStatus = "needs-update"
	TranslatedStatusNeedsReplace TranslatedStatus = "needs-replace"
	TranslatedStatusMigrated     TranslatedStatus = "migrated"
)

func (ResourceTranslated) isResourceStatus() {}

var _ ResourceStatus = (*ResourceTranslated)(nil)

// ComputeDiff analyzes Terraform resources and computes their migration status.
// Returns a map of Terraform addresses to their ResourceStatus.
func ComputeDiff(ctx context.Context, stackConfig Stack, ws auto.Workspace, tfState *tfjson.State) (map[string]ResourceStatus, error) {
	// Get all TF resources
	allTFResources, err := AllResources(tfState)
	if err != nil {
		return nil, fmt.Errorf("failed to get Terraform resources: %w", err)
	}

	// Select stack
	stack, err := auto.SelectStack(ctx, stackConfig.PulumiStack, ws)
	if err != nil {
		return nil, fmt.Errorf("failed to select stack %s: %w", stackConfig.PulumiStack, err)
	}

	// Run preview with refresh to get resource status
	previewStatus, err := pulumix.Preview(ctx, stack, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to run preview: %w", err)
	}

	// Build map of TF addr to resource config
	tfAddrToResource := make(map[string]*Resource)
	for i := range stackConfig.Resources {
		res := &stackConfig.Resources[i]
		if res.TFAddr != "" {
			tfAddrToResource[res.TFAddr] = res
		}
	}

	// Compute status for each TF resource
	result := make(map[string]ResourceStatus)

	for _, tfRes := range allTFResources {
		// Look up the resource in migration config
		migRes := tfAddrToResource[tfRes.Address]
		if migRes == nil {
			// Resource not tracked in migration.json
			result[tfRes.Address] = &ResourceNotTracked{}
			continue
		}

		// Check if skipped
		if migRes.Migrate == "skip" {
			result[tfRes.Address] = &ResourceSkipped{}
			continue
		}

		// Resource is tracked and not skipped
		if migRes.URN == "" {
			// No URN mapping yet - not translated
			result[tfRes.Address] = &ResourceNotTracked{}
		} else {
			// Check if URN exists in preview status
			urn, err := resource.ParseURN(migRes.URN)
			if err != nil {
				return nil, fmt.Errorf("invalid URN %s: %w", migRes.URN, err)
			}

			previewStat, hasPreviewStatus := previewStatus[urn]
			var translatedStatus TranslatedStatus

			if !hasPreviewStatus {
				// URN not found in preview - no state
				translatedStatus = TranslatedStatusNoState
			} else if previewStat.WillReplace() {
				translatedStatus = TranslatedStatusNeedsReplace
			} else if previewStat.WillUpdate() {
				translatedStatus = TranslatedStatusNeedsUpdate
			} else if previewStat.WillNotChange() {
				translatedStatus = TranslatedStatusMigrated
			} else {
				// Other operations (create, delete, etc.) - treat as needs update
				translatedStatus = TranslatedStatusNeedsUpdate
			}

			result[tfRes.Address] = &ResourceTranslated{
				URN:              pulumi.URN(migRes.URN),
				TranslatedStatus: translatedStatus,
			}
		}
	}

	return result, nil
}
