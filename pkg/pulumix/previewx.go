package pulumix

import (
	"context"
	"fmt"

	"github.com/pulumi/pulumi/sdk/v3/go/auto"
	"github.com/pulumi/pulumi/sdk/v3/go/auto/events"
	"github.com/pulumi/pulumi/sdk/v3/go/auto/optpreview"
	"github.com/pulumi/pulumi/sdk/v3/go/common/apitype"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
)

// ResourcePreviewStatus represents the operation status for a Pulumi resource during preview.
type ResourcePreviewStatus struct {
	// URN uniquely identifies the resource
	URN resource.URN

	// Op indicates the operation type (create, update, delete, replace, etc.)
	Op apitype.OpType

	// DetailedDiff provides detailed property-level diff information
	DetailedDiff map[string]apitype.PropertyDiff
}

// PreviewOptions provides configuration for running a preview operation.
type PreviewOptions struct {
	// AdditionalOptions allows passing additional optpreview.Option values
	AdditionalOptions []optpreview.Option

	// BufferSize sets the event channel buffer size (default: 1000)
	BufferSize int
}

// Preview runs a preview operation on the given stack and returns a map of resource URNs
// to their operation status. This captures per-resource details including what operation
// will be performed (create, update, delete, replace, etc.) and detailed diff information.
//
// Example usage:
//
//	ctx := context.Background()
//	stack, _ := auto.SelectStack(ctx, "dev", workspace)
//	statusMap, err := pulumix.Preview(ctx, stack, nil)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	for urn, status := range statusMap {
//	    fmt.Printf("Resource %s: %s\n", urn, status.Op)
//	}
func Preview(ctx context.Context, stack auto.Stack, opts *PreviewOptions) (map[resource.URN]ResourcePreviewStatus, error) {
	if opts == nil {
		opts = &PreviewOptions{}
	}

	// Set default buffer size
	bufferSize := opts.BufferSize
	if bufferSize == 0 {
		bufferSize = 1000
	}

	// Create channel to receive engine events
	eventChannel := make(chan events.EngineEvent, bufferSize)

	// Map to store resource status by URN
	statusMap := make(map[resource.URN]ResourcePreviewStatus)

	// Channel to signal when event processing is complete
	eventsDone := make(chan error, 1)

	// Start goroutine to process events
	go func() {
		defer close(eventsDone)

		for event := range eventChannel {
			// Process resource pre-events which contain the operation details
			if event.ResourcePreEvent != nil {
				metadata := event.ResourcePreEvent.Metadata
				urn := resource.URN(metadata.URN)

				status := ResourcePreviewStatus{
					URN:          urn,
					Op:           metadata.Op,
					DetailedDiff: metadata.DetailedDiff,
				}

				statusMap[urn] = status
			}

			// Also capture ResOutputsEvent for completion status
			// This can provide additional information after the resource operation
			if event.ResOutputsEvent != nil {
				metadata := event.ResOutputsEvent.Metadata
				urn := resource.URN(metadata.URN)

				// Update existing status if present, or create new one
				status, exists := statusMap[urn]
				if !exists {
					status = ResourcePreviewStatus{
						URN: urn,
						Op:  metadata.Op,
					}
				}

				// Update with output event details
				status.DetailedDiff = metadata.DetailedDiff

				statusMap[urn] = status
			}
		}
	}()

	// Build preview options with refresh enabled
	previewOpts := append([]optpreview.Option{
		optpreview.EventStreams(eventChannel),
		optpreview.Refresh(),
	}, opts.AdditionalOptions...)

	// Run preview
	_, err := stack.Preview(ctx, previewOpts...)

	// Wait for event processing to complete
	if eventErr := <-eventsDone; eventErr != nil {
		return nil, fmt.Errorf("error processing events: %w", eventErr)
	}

	if err != nil {
		return nil, fmt.Errorf("preview failed: %w", err)
	}

	return statusMap, nil
}

// WillCreate returns true if the resource status indicates a create operation.
func (rs ResourcePreviewStatus) WillCreate() bool {
	return rs.Op == apitype.OpCreate || rs.Op == apitype.OpCreateReplacement || rs.Op == apitype.OpImport
}

// WillUpdate returns true if the resource status indicates an update operation.
func (rs ResourcePreviewStatus) WillUpdate() bool {
	return rs.Op == apitype.OpUpdate
}

// WillReplace returns true if the resource status indicates a replace operation.
func (rs ResourcePreviewStatus) WillReplace() bool {
	return rs.Op == apitype.OpReplace || rs.Op == apitype.OpCreateReplacement ||
		rs.Op == apitype.OpDeleteReplaced || rs.Op == apitype.OpImportReplacement
}

// WillDelete returns true if the resource status indicates a delete operation.
func (rs ResourcePreviewStatus) WillDelete() bool {
	return rs.Op == apitype.OpDelete || rs.Op == apitype.OpDeleteReplaced ||
		rs.Op == apitype.OpReadDiscard || rs.Op == apitype.OpDiscardReplaced
}

// WillNotChange returns true if the resource will not be modified.
func (rs ResourcePreviewStatus) WillNotChange() bool {
	return rs.Op == apitype.OpSame
}

// String returns a human-readable representation of the resource status.
func (rs ResourcePreviewStatus) String() string {
	return fmt.Sprintf("ResourceStatus{URN: %s, Op: %s}", rs.URN, rs.Op)
}
