// Copyright 2016-2025, Pulumi Corporation.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package pulumix

import (
	"context"
	"fmt"

	"github.com/pulumi/pulumi/sdk/v3/go/auto"
	"github.com/pulumi/pulumi/sdk/v3/go/auto/events"
	"github.com/pulumi/pulumi/sdk/v3/go/auto/optpreview"
	"github.com/pulumi/pulumi/sdk/v3/go/common/apitype"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/common/util/contract"
)

// ResourcePreviewStatus represents the operation status for a Pulumi resource during preview.
type ResourcePreviewStatus string

const (
	Same    ResourcePreviewStatus = "same"
	Create  ResourcePreviewStatus = "create"
	Update  ResourcePreviewStatus = "update"
	Replace ResourcePreviewStatus = "replace"
	Delete  ResourcePreviewStatus = "delete"
)

// PreviewOptions provides configuration for running a preview operation.
type PreviewOptions struct {
	// AdditionalOptions allows passing additional optpreview.Option values
	AdditionalOptions []optpreview.Option
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
	bufferSize := 1024

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

				status, ok := classifyOperation(metadata.Op)
				if ok {
					statusMap[urn] = status
				}
			}

			// Also capture ResOutputsEvent for completion status
			// This can provide additional information after the resource operation
			if event.ResOutputsEvent != nil {
				metadata := event.ResOutputsEvent.Metadata
				urn := resource.URN(metadata.URN)

				// Update existing status if not already set
				if _, exists := statusMap[urn]; !exists {
					status, ok := classifyOperation(metadata.Op)
					if ok {
						statusMap[urn] = status
					}
				}
			}
		}
	}()

	// Build preview options with refresh enabled
	previewOpts := append([]optpreview.Option{
		optpreview.EventStreams(eventChannel),
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

// classifyOperation maps apitype.OpType to ResourcePreviewStatus.
func classifyOperation(op apitype.OpType) (ResourcePreviewStatus, bool) {
	switch op {
	// OpSame indicates no change was made.
	case apitype.OpSame:
		return Same, true
	// OpCreate indicates a new resource was created.
	case apitype.OpCreate:
		return Create, true
	// OpUpdate indicates an existing resource was updated.
	case apitype.OpUpdate:
		return Update, true
	// OpDelete indicates an existing resource was deleted.
	case apitype.OpDelete:
		return Delete, true
	// OpReplace indicates an existing resource was replaced with a new one.
	case apitype.OpReplace:
		return Replace, true
	// OpCreateReplacement indicates a new resource was created for a replacement.
	case apitype.OpCreateReplacement:
		return "", false // skip this as too detailed
	// OpDeleteReplaced indicates an existing resource was deleted after replacement.
	case apitype.OpDeleteReplaced:
		return "", false // skip this as too detailed
	// OpRead indicates reading an existing resource.
	case apitype.OpRead:
		contract.Failf("OpRead operation is not expected")
		return "", false
	// OpReadReplacement indicates reading an existing resource for a replacement.
	case apitype.OpReadReplacement:
		contract.Failf("OpReadReplacement operation is not expected")
		return "", false
	// OpRefresh indicates refreshing an existing resource.
	case apitype.OpRefresh:
		contract.Failf("OpRefresh operation is not expected")
		return "", false
	// OpReadDiscard indicates removing a resource that was read.
	case apitype.OpReadDiscard:
		contract.Failf("OpReadDiscard operation is not expected")
		return "", false
	// OpDiscardReplaced indicates discarding a read resource that was replaced.
	case apitype.OpDiscardReplaced:
		return "", false // skip this as too detailed
	// OpRemovePendingReplace indicates removing a pending replace resource.
	case apitype.OpRemovePendingReplace:
		return "", false // skip this as too detailed
	// OpImport indicates importing an existing resource.
	case apitype.OpImport:
		contract.Failf("OpImport operation is not expected")
		return "", false
	// OpImportReplacement indicates replacement of an existing resource with an imported resource.
	case apitype.OpImportReplacement:
		contract.Failf("OpImportReplacement operation is not expected")
		return "", false
	default:
		contract.Failf("Unhandled OpType case: %v", op)
		return "", false
	}
}
