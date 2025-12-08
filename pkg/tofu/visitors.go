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

package tofu

import (
	tfjson "github.com/hashicorp/terraform-json"
)

// VisitOptions configures the behavior of the VisitResources function
type VisitOptions struct {
	// IncludeDataSources controls whether data sources should be included during traversal.
	// Default: false (data sources are skipped by default)
	IncludeDataSources bool
}

// VisitResources recursively visits all resources in a Terraform state
// By default, data sources are skipped. Pass custom VisitOptions to change this behavior.
func VisitResources(state *tfjson.State, visitor func(*tfjson.StateResource) error, opts *VisitOptions) error {
	if state == nil || state.Values == nil {
		return nil
	}

	if opts == nil {
		opts = &VisitOptions{}
	}

	return visitModule(state.Values.RootModule, visitor, opts)
}

// visitModule recursively visits all resources in a module and its children
func visitModule(module *tfjson.StateModule, visitor func(*tfjson.StateResource) error, opts *VisitOptions) error {
	if module == nil {
		return nil
	}

	// Visit resources in this module
	for _, res := range module.Resources {
		// Skip data sources unless configured to include them
		if !opts.IncludeDataSources && res.Mode == tfjson.DataResourceMode {
			continue
		}

		if err := visitor(res); err != nil {
			return err
		}
	}

	// Visit child modules
	for _, child := range module.ChildModules {
		if err := visitModule(child, visitor, opts); err != nil {
			return err
		}
	}

	return nil
}
