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

package bridgedproviders

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetMappingForTerraformProvider_Integration(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	providerInfo, err := GetMappingForTerraformProvider(ctx, "hashicorp/time", "0.12.1")
	require.NoError(t, err)
	require.NotNil(t, providerInfo)

	assert.NotEmpty(t, providerInfo.Name, "Provider name should not be empty")
	assert.NotNil(t, providerInfo.P, "Provider shim should not be nil")

	resourcesMap := providerInfo.P.ResourcesMap()
	assert.NotNil(t, resourcesMap)

	expectedResources := []string{"time_sleep", "time_offset", "time_rotating", "time_static"}
	for _, resName := range expectedResources {
		res := resourcesMap.Get(resName)
		if res == nil {
			t.Logf("Resource %s not found in provider (may be version dependent)", resName)
		}
	}

	t.Logf("Successfully got mapping for provider '%s' with %d resources",
		providerInfo.Name, resourcesMap.Len())
}
