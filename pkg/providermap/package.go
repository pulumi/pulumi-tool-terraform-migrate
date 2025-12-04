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

// Facilities to help decide which Pulumi provider use for a given Terraform or OpenTOFU provider.
//
// Pulumi maintains bridged providers listed in this file:
//
// https://github.com/pulumi/ci-mgmt/blob/master/provider-ci/providers.json
//
// For each provider such as "azuread" there is a corresponding GitHub repository such as "pulumi/pulumi-azuread".
//
// If there is no Pulumi maintained provider, one can be generated on the fly by Pulumi using this command:
//
//	pulumi package add terraform-provider <your-terraform-provider>
package providermap
