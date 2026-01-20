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

package providermap

import (
	_ "embed"
	"strings"

	"github.com/blang/semver"
	"gopkg.in/yaml.v3"
)

//go:embed versions.yaml
var embeddedVersionsYAML []byte

var refinedVersionMap *VersionMap

func init() {
	// Load the embedded versions.yaml file
	var vm VersionMap
	if err := yaml.Unmarshal(embeddedVersionsYAML, &vm); err == nil {
		refinedVersionMap = &vm
	}
	// If unmarshal fails, refinedVersionMap will remain nil and fallback logic will be used
}

// A full name such as "registry.terraform.io/hashicorp/aws"
type TerraformProviderName string

type TerraformProvider struct {
	// Identifier such as "registry.opentofu.org/hashicorp/aws" or "registry.opentofu.org/hashicorp/aws"
	Identifier TerraformProviderName

	// Version such as v3.18.0
	Version string
}

type BridgedPulumiProvider struct {
	// Identifier such as "aws"
	Identifier string

	// Pulumi version such as "v7.12.0"
	Version string
}

// The recommendation only sets one of the two fields.
type RecommendedPulumiProvider struct {
	// Use `pulumi package add terraform-provider ...`
	// See https://www.pulumi.com/blog/any-terraform-provider/
	UseTerraformProviderPackage bool

	// Use a Pulumi bridged provider.
	BridgedPulumiProvider *BridgedPulumiProvider
}

type providerMappingDetail struct {
	pulumiProviderName            string
	terraformProviderName         string // Name of the upstream Terraform provider (e.g., "aws" for terraform-provider-aws)
	latestVersionByTerraformMajor map[int]string
}

// providerMapping maps Terraform/OpenTofu provider identifiers to Pulumi provider names.
// This is based on the provider list from https://github.com/pulumi/ci-mgmt/blob/master/provider-ci/providers.json
var providerMapping = map[TerraformProviderName]providerMappingDetail{
	// HashiCorp providers
	"registry.terraform.io/hashicorp/aws": {
		pulumiProviderName:    "aws",
		terraformProviderName: "aws",
		latestVersionByTerraformMajor: map[int]string{
			5: "v6.83.2",
			6: "v7.16.0",
		},
	},
	"registry.opentofu.org/hashicorp/aws": {
		pulumiProviderName:    "aws",
		terraformProviderName: "aws",
		latestVersionByTerraformMajor: map[int]string{
			5: "v6.83.2",
			6: "v7.16.0",
		},
	},
	"registry.terraform.io/hashicorp/azurerm": {
		pulumiProviderName:    "azure",
		terraformProviderName: "azurerm",
		latestVersionByTerraformMajor: map[int]string{
			3: "v5.89.0",
			4: "v6.31.0",
		},
	},
	"registry.opentofu.org/hashicorp/azurerm": {
		pulumiProviderName:    "azure",
		terraformProviderName: "azurerm",
		latestVersionByTerraformMajor: map[int]string{
			3: "v5.89.0",
			4: "v6.31.0",
		},
	},
	"registry.terraform.io/hashicorp/azuread": {
		pulumiProviderName:    "azuread",
		terraformProviderName: "azuread",
		latestVersionByTerraformMajor: map[int]string{
			2: "v5.53.1",
			3: "v6.8.0",
		},
	},
	"registry.opentofu.org/hashicorp/azuread": {
		pulumiProviderName:    "azuread",
		terraformProviderName: "azuread",
		latestVersionByTerraformMajor: map[int]string{
			2: "v5.53.1",
			3: "v6.8.0",
		},
	},
	"registry.terraform.io/hashicorp/google": {
		pulumiProviderName:    "gcp",
		terraformProviderName: "google",
		latestVersionByTerraformMajor: map[int]string{
			6: "v8.41.1",
			7: "v9.10.0",
		},
	},
	"registry.opentofu.org/hashicorp/google": {
		pulumiProviderName:    "gcp",
		terraformProviderName: "google",
		latestVersionByTerraformMajor: map[int]string{
			6: "v8.41.1",
			7: "v9.10.0",
		},
	},
	"registry.terraform.io/hashicorp/google-beta": {
		pulumiProviderName:    "gcp",
		terraformProviderName: "google",
		latestVersionByTerraformMajor: map[int]string{
			6: "v8.41.1",
			7: "v9.10.0",
		},
	},
	"registry.opentofu.org/hashicorp/google-beta": {
		pulumiProviderName:    "gcp",
		terraformProviderName: "google",
		latestVersionByTerraformMajor: map[int]string{
			6: "v8.41.1",
			7: "v9.10.0",
		},
	},
	"registry.terraform.io/hashicorp/consul": {
		pulumiProviderName:    "consul",
		terraformProviderName: "consul",
		latestVersionByTerraformMajor: map[int]string{
			2: "v3.13.3",
		},
	},
	"registry.opentofu.org/hashicorp/consul": {
		pulumiProviderName:    "consul",
		terraformProviderName: "consul",
		latestVersionByTerraformMajor: map[int]string{
			2: "v3.13.3",
		},
	},
	"registry.terraform.io/hashicorp/vault": {
		pulumiProviderName:    "vault",
		terraformProviderName: "vault",
		latestVersionByTerraformMajor: map[int]string{
			3: "v5.20.0",
			4: "v6.7.0",
			5: "v7.5.0",
		},
	},
	"registry.opentofu.org/hashicorp/vault": {
		pulumiProviderName:    "vault",
		terraformProviderName: "vault",
		latestVersionByTerraformMajor: map[int]string{
			3: "v5.20.0",
			4: "v6.7.0",
			5: "v7.5.0",
		},
	},
	"registry.terraform.io/hashicorp/nomad": {
		pulumiProviderName:    "nomad",
		terraformProviderName: "nomad",
		latestVersionByTerraformMajor: map[int]string{
			1: "v0.4.1",
			2: "v2.5.3",
		},
	},
	"registry.opentofu.org/hashicorp/nomad": {
		pulumiProviderName:    "nomad",
		terraformProviderName: "nomad",
		latestVersionByTerraformMajor: map[int]string{
			1: "v0.4.1",
			2: "v2.5.3",
		},
	},
	"registry.terraform.io/hashicorp/vsphere": {
		pulumiProviderName:    "vsphere",
		terraformProviderName: "vsphere",
		latestVersionByTerraformMajor: map[int]string{
			2: "v4.16.0",
		},
	},
	"registry.opentofu.org/hashicorp/vsphere": {
		pulumiProviderName:    "vsphere",
		terraformProviderName: "vsphere",
		latestVersionByTerraformMajor: map[int]string{
			2: "v4.16.0",
		},
	},
	"registry.terraform.io/hashicorp/random": {
		pulumiProviderName:    "random",
		terraformProviderName: "random",
		latestVersionByTerraformMajor: map[int]string{
			3: "v4.18.1",
		},
	},
	"registry.opentofu.org/hashicorp/random": {
		pulumiProviderName:    "random",
		terraformProviderName: "random",
		latestVersionByTerraformMajor: map[int]string{
			3: "v4.18.1",
		},
	},
	"registry.terraform.io/hashicorp/archive": {
		pulumiProviderName:    "archive",
		terraformProviderName: "archive",
		latestVersionByTerraformMajor: map[int]string{
			2: "v0.3.3",
		},
	},
	"registry.opentofu.org/hashicorp/archive": {
		pulumiProviderName:    "archive",
		terraformProviderName: "archive",
		latestVersionByTerraformMajor: map[int]string{
			2: "v0.3.3",
		},
	},
	"registry.terraform.io/hashicorp/tls": {
		pulumiProviderName:    "tls",
		terraformProviderName: "tls",
		latestVersionByTerraformMajor: map[int]string{
			3: "v4.5.0",
			4: "v5.2.0",
		},
	},
	"registry.opentofu.org/hashicorp/tls": {
		pulumiProviderName:    "tls",
		terraformProviderName: "tls",
		latestVersionByTerraformMajor: map[int]string{
			3: "v4.5.0",
			4: "v5.2.0",
		},
	},
	"registry.terraform.io/hashicorp/external": {
		pulumiProviderName:    "external",
		terraformProviderName: "external",
		latestVersionByTerraformMajor: map[int]string{
			2: "v0.0.14",
		},
	},
	"registry.opentofu.org/hashicorp/external": {
		pulumiProviderName:    "external",
		terraformProviderName: "external",
		latestVersionByTerraformMajor: map[int]string{
			2: "v0.0.14",
		},
	},
	"registry.terraform.io/hashicorp/http": {
		pulumiProviderName:    "http",
		terraformProviderName: "http",
		latestVersionByTerraformMajor: map[int]string{
			3: "v0.1.0",
		},
	},
	"registry.opentofu.org/hashicorp/http": {
		pulumiProviderName:    "http",
		terraformProviderName: "http",
		latestVersionByTerraformMajor: map[int]string{
			3: "v0.1.0",
		},
	},
	"registry.terraform.io/hashicorp/null": {
		pulumiProviderName:    "null",
		terraformProviderName: "null",
		latestVersionByTerraformMajor: map[int]string{
			3: "v0.0.11",
		},
	},
	"registry.opentofu.org/hashicorp/null": {
		pulumiProviderName:    "null",
		terraformProviderName: "null",
		latestVersionByTerraformMajor: map[int]string{
			3: "v0.0.11",
		},
	},
	"registry.terraform.io/hashicorp/cloudinit": {
		pulumiProviderName:    "cloudinit",
		terraformProviderName: "cloudinit",
		latestVersionByTerraformMajor: map[int]string{
			2: "v1.4.12",
		},
	},
	"registry.opentofu.org/hashicorp/cloudinit": {
		pulumiProviderName:    "cloudinit",
		terraformProviderName: "cloudinit",
		latestVersionByTerraformMajor: map[int]string{
			2: "v1.4.12",
		},
	},

	// Third-party providers
	"registry.terraform.io/aiven/aiven": {
		pulumiProviderName:    "aiven",
		terraformProviderName: "aiven",
		latestVersionByTerraformMajor: map[int]string{
			4: "v6.45.0",
		},
	},
	"registry.opentofu.org/aiven/aiven": {
		pulumiProviderName:    "aiven",
		terraformProviderName: "aiven",
		latestVersionByTerraformMajor: map[int]string{
			4: "v6.45.0",
		},
	},
	"registry.terraform.io/akamai/akamai": {
		pulumiProviderName:    "akamai",
		terraformProviderName: "akamai",
		latestVersionByTerraformMajor: map[int]string{
			1: "v2.9.0",
			3: "v4.5.0",
			4: "v5.0.0",
			5: "v6.4.0",
			6: "v7.6.1",
			7: "v8.1.0",
			8: "v9.1.0",
			9: "v10.2.0",
		},
	},
	"registry.opentofu.org/akamai/akamai": {
		pulumiProviderName:    "akamai",
		terraformProviderName: "akamai",
		latestVersionByTerraformMajor: map[int]string{
			1: "v2.9.0",
			3: "v4.5.0",
			4: "v5.0.0",
			5: "v6.4.0",
			6: "v7.6.1",
			7: "v8.1.0",
			8: "v9.1.0",
			9: "v10.2.0",
		},
	},
	"registry.terraform.io/aliyun/alicloud": {
		pulumiProviderName:    "alicloud",
		terraformProviderName: "alicloud",
		latestVersionByTerraformMajor: map[int]string{
			1: "v3.90.0",
		},
	},
	"registry.opentofu.org/aliyun/alicloud": {
		pulumiProviderName:    "alicloud",
		terraformProviderName: "alicloud",
		latestVersionByTerraformMajor: map[int]string{
			1: "v3.90.0",
		},
	},
	"registry.terraform.io/jfrog/artifactory": {
		pulumiProviderName:    "artifactory",
		terraformProviderName: "artifactory",
		latestVersionByTerraformMajor: map[int]string{
			10: "v6.8.3",
			11: "v7.9.1",
			12: "v8.10.0",
		},
	},
	"registry.opentofu.org/jfrog/artifactory": {
		pulumiProviderName:    "artifactory",
		terraformProviderName: "artifactory",
		latestVersionByTerraformMajor: map[int]string{
			10: "v6.8.3",
			11: "v7.9.1",
			12: "v8.10.0",
		},
	},
	"registry.terraform.io/auth0/auth0": {
		pulumiProviderName:    "auth0",
		terraformProviderName: "auth0",
		latestVersionByTerraformMajor: map[int]string{
			1: "v3.34.0",
		},
	},
	"registry.opentofu.org/auth0/auth0": {
		pulumiProviderName:    "auth0",
		terraformProviderName: "auth0",
		latestVersionByTerraformMajor: map[int]string{
			1: "v3.34.0",
		},
	},
	"registry.terraform.io/microsoft/azuredevops": {
		pulumiProviderName:    "azuredevops",
		terraformProviderName: "azuredevops",
		latestVersionByTerraformMajor: map[int]string{
			0: "v2.15.0",
			1: "v3.10.0",
		},
	},
	"registry.opentofu.org/microsoft/azuredevops": {
		pulumiProviderName:    "azuredevops",
		terraformProviderName: "azuredevops",
		latestVersionByTerraformMajor: map[int]string{
			0: "v2.15.0",
			1: "v3.10.0",
		},
	},
	"registry.terraform.io/cloudamqp/cloudamqp": {
		pulumiProviderName:    "cloudamqp",
		terraformProviderName: "cloudamqp",
		latestVersionByTerraformMajor: map[int]string{
			1: "v3.24.3",
		},
	},
	"registry.opentofu.org/cloudamqp/cloudamqp": {
		pulumiProviderName:    "cloudamqp",
		terraformProviderName: "cloudamqp",
		latestVersionByTerraformMajor: map[int]string{
			1: "v3.24.3",
		},
	},
	"registry.terraform.io/cloudflare/cloudflare": {
		pulumiProviderName:    "cloudflare",
		terraformProviderName: "cloudflare",
		latestVersionByTerraformMajor: map[int]string{
			4: "v5.49.0",
			5: "v6.11.0",
		},
	},
	"registry.opentofu.org/cloudflare/cloudflare": {
		pulumiProviderName:    "cloudflare",
		terraformProviderName: "cloudflare",
		latestVersionByTerraformMajor: map[int]string{
			4: "v5.49.0",
			5: "v6.11.0",
		},
	},
	"registry.terraform.io/confluentinc/confluent": {
		pulumiProviderName:    "confluentcloud",
		terraformProviderName: "confluent",
		latestVersionByTerraformMajor: map[int]string{
			2: "v2.52.0",
		},
	},
	"registry.opentofu.org/confluentinc/confluent": {
		pulumiProviderName:    "confluentcloud",
		terraformProviderName: "confluent",
		latestVersionByTerraformMajor: map[int]string{
			2: "v2.52.0",
		},
	},
	"registry.terraform.io/databricks/databricks": {
		pulumiProviderName:    "databricks",
		terraformProviderName: "databricks",
		latestVersionByTerraformMajor: map[int]string{
			1: "v1.78.0",
		},
	},
	"registry.opentofu.org/databricks/databricks": {
		pulumiProviderName:    "databricks",
		terraformProviderName: "databricks",
		latestVersionByTerraformMajor: map[int]string{
			1: "v1.78.0",
		},
	},
	"registry.terraform.io/datadog/datadog": {
		pulumiProviderName:    "datadog",
		terraformProviderName: "datadog",
		latestVersionByTerraformMajor: map[int]string{
			3: "v4.60.0",
		},
	},
	"registry.opentofu.org/datadog/datadog": {
		pulumiProviderName:    "datadog",
		terraformProviderName: "datadog",
		latestVersionByTerraformMajor: map[int]string{
			3: "v4.60.0",
		},
	},
	"registry.terraform.io/dbt-labs/dbtcloud": {
		pulumiProviderName:    "dbtcloud",
		terraformProviderName: "dbtcloud",
		latestVersionByTerraformMajor: map[int]string{
			0: "v0.1.30",
			1: "v1.3.1",
		},
	},
	"registry.opentofu.org/dbt-labs/dbtcloud": {
		pulumiProviderName:    "dbtcloud",
		terraformProviderName: "dbtcloud",
		latestVersionByTerraformMajor: map[int]string{
			0: "v0.1.30",
			1: "v1.3.1",
		},
	},
	"registry.terraform.io/digitalocean/digitalocean": {
		pulumiProviderName:    "digitalocean",
		terraformProviderName: "digitalocean",
		latestVersionByTerraformMajor: map[int]string{
			2: "v4.55.0",
		},
	},
	"registry.opentofu.org/digitalocean/digitalocean": {
		pulumiProviderName:    "digitalocean",
		terraformProviderName: "digitalocean",
		latestVersionByTerraformMajor: map[int]string{
			2: "v4.55.0",
		},
	},
	"registry.terraform.io/dnsimple/dnsimple": {
		pulumiProviderName:    "dnsimple",
		terraformProviderName: "dnsimple",
		latestVersionByTerraformMajor: map[int]string{
			0: "v3.5.0",
			1: "v4.4.0",
		},
	},
	"registry.opentofu.org/dnsimple/dnsimple": {
		pulumiProviderName:    "dnsimple",
		terraformProviderName: "dnsimple",
		latestVersionByTerraformMajor: map[int]string{
			0: "v3.5.0",
			1: "v4.4.0",
		},
	},
	"registry.terraform.io/kreuzwerker/docker": {
		pulumiProviderName:    "docker",
		terraformProviderName: "docker",
		latestVersionByTerraformMajor: map[int]string{
			2: "v3.2.0",
			3: "v4.10.0",
		},
	},
	"registry.opentofu.org/kreuzwerker/docker": {
		pulumiProviderName:    "docker",
		terraformProviderName: "docker",
		latestVersionByTerraformMajor: map[int]string{
			2: "v3.2.0",
			3: "v4.10.0",
		},
	},
	"registry.terraform.io/elastic/ec": {
		pulumiProviderName:    "ec",
		terraformProviderName: "ec",
		latestVersionByTerraformMajor: map[int]string{
			0: "v0.10.4",
		},
	},
	"registry.opentofu.org/elastic/ec": {
		pulumiProviderName:    "ec",
		terraformProviderName: "ec",
		latestVersionByTerraformMajor: map[int]string{
			0: "v0.10.4",
		},
	},
	"registry.terraform.io/f5networks/bigip": {
		pulumiProviderName:    "f5bigip",
		terraformProviderName: "bigip",
		latestVersionByTerraformMajor: map[int]string{
			1: "v3.19.2",
		},
	},
	"registry.opentofu.org/f5networks/bigip": {
		pulumiProviderName:    "f5bigip",
		terraformProviderName: "bigip",
		latestVersionByTerraformMajor: map[int]string{
			1: "v3.19.2",
		},
	},
	"registry.terraform.io/fastly/fastly": {
		pulumiProviderName:    "fastly",
		terraformProviderName: "fastly",
		latestVersionByTerraformMajor: map[int]string{
			2: "v5.1.0",
			3: "v6.0.0",
			4: "v7.3.2",
			5: "v8.14.0",
			6: "v9.1.0",
			7: "v10.1.0",
			8: "v11.2.0",
		},
	},
	"registry.opentofu.org/fastly/fastly": {
		pulumiProviderName:    "fastly",
		terraformProviderName: "fastly",
		latestVersionByTerraformMajor: map[int]string{
			2: "v5.1.0",
			3: "v6.0.0",
			4: "v7.3.2",
			5: "v8.14.0",
			6: "v9.1.0",
			7: "v10.1.0",
			8: "v11.2.0",
		},
	},
	"registry.terraform.io/integrations/github": {
		pulumiProviderName:    "github",
		terraformProviderName: "github",
		latestVersionByTerraformMajor: map[int]string{
			5: "v5.26.0",
			6: "v6.9.1",
		},
	},
	"registry.opentofu.org/integrations/github": {
		pulumiProviderName:    "github",
		terraformProviderName: "github",
		latestVersionByTerraformMajor: map[int]string{
			5: "v5.26.0",
			6: "v6.9.1",
		},
	},
	"registry.terraform.io/gitlabhq/gitlab": {
		pulumiProviderName:    "gitlab",
		terraformProviderName: "gitlab",
		latestVersionByTerraformMajor: map[int]string{
			3:  "v4.10.0",
			16: "v6.11.0",
			17: "v8.11.0",
			18: "v9.5.0",
		},
	},
	"registry.opentofu.org/gitlabhq/gitlab": {
		pulumiProviderName:    "gitlab",
		terraformProviderName: "gitlab",
		latestVersionByTerraformMajor: map[int]string{
			3:  "v4.10.0",
			16: "v6.11.0",
			17: "v8.11.0",
			18: "v9.5.0",
		},
	},
	"registry.terraform.io/harness/harness": {
		pulumiProviderName:    "harness",
		terraformProviderName: "harness",
		latestVersionByTerraformMajor: map[int]string{
			0: "v0.9.3",
		},
	},
	"registry.opentofu.org/harness/harness": {
		pulumiProviderName:    "harness",
		terraformProviderName: "harness",
		latestVersionByTerraformMajor: map[int]string{
			0: "v0.9.3",
		},
	},
	"registry.terraform.io/hetznercloud/hcloud": {
		pulumiProviderName:    "hcloud",
		terraformProviderName: "hcloud",
		latestVersionByTerraformMajor: map[int]string{
			1: "v1.29.0",
		},
	},
	"registry.opentofu.org/hetznercloud/hcloud": {
		pulumiProviderName:    "hcloud",
		terraformProviderName: "hcloud",
		latestVersionByTerraformMajor: map[int]string{
			1: "v1.29.0",
		},
	},
	"registry.terraform.io/ciscodevnet/ise": {
		pulumiProviderName:    "ise",
		terraformProviderName: "ise",
		latestVersionByTerraformMajor: map[int]string{
			0: "v0.2.7",
		},
	},
	"registry.opentofu.org/ciscodevnet/ise": {
		pulumiProviderName:    "ise",
		terraformProviderName: "ise",
		latestVersionByTerraformMajor: map[int]string{
			0: "v0.2.7",
		},
	},
	"registry.terraform.io/Juniper/mist": {
		pulumiProviderName:    "junipermist",
		terraformProviderName: "mist",
		latestVersionByTerraformMajor: map[int]string{
			0: "v0.7.1",
		},
	},
	"registry.terraform.io/mongey/kafka": {
		pulumiProviderName:    "kafka",
		terraformProviderName: "kafka",
		latestVersionByTerraformMajor: map[int]string{
			0: "v3.12.1",
		},
	},
	"registry.opentofu.org/mongey/kafka": {
		pulumiProviderName:    "kafka",
		terraformProviderName: "kafka",
		latestVersionByTerraformMajor: map[int]string{
			0: "v3.12.1",
		},
	},
	"registry.terraform.io/mrparkers/keycloak": {
		pulumiProviderName:    "keycloak",
		terraformProviderName: "keycloak",
		latestVersionByTerraformMajor: map[int]string{
			3: "v4.8.0",
			4: "v5.4.0",
			5: "v6.8.0",
		},
	},
	"registry.opentofu.org/mrparkers/keycloak": {
		pulumiProviderName:    "keycloak",
		terraformProviderName: "keycloak",
		latestVersionByTerraformMajor: map[int]string{
			3: "v4.8.0",
			4: "v5.4.0",
			5: "v6.8.0",
		},
	},
	"registry.terraform.io/kevholditch/kong": {
		pulumiProviderName:    "kong",
		terraformProviderName: "kong",
		latestVersionByTerraformMajor: map[int]string{
			6: "v4.5.3",
		},
	},
	"registry.opentofu.org/kevholditch/kong": {
		pulumiProviderName:    "kong",
		terraformProviderName: "kong",
		latestVersionByTerraformMajor: map[int]string{
			6: "v4.5.3",
		},
	},
	"registry.terraform.io/linode/linode": {
		pulumiProviderName:    "linode",
		terraformProviderName: "linode",
		latestVersionByTerraformMajor: map[int]string{
			2: "v4.39.0",
			3: "v5.5.0",
		},
	},
	"registry.opentofu.org/linode/linode": {
		pulumiProviderName:    "linode",
		terraformProviderName: "linode",
		latestVersionByTerraformMajor: map[int]string{
			2: "v4.39.0",
			3: "v5.5.0",
		},
	},
	"registry.terraform.io/wgebis/mailgun": {
		pulumiProviderName:    "mailgun",
		terraformProviderName: "mailgun",
		latestVersionByTerraformMajor: map[int]string{
			0: "v3.6.1",
		},
	},
	"registry.opentofu.org/wgebis/mailgun": {
		pulumiProviderName:    "mailgun",
		terraformProviderName: "mailgun",
		latestVersionByTerraformMajor: map[int]string{
			0: "v3.6.1",
		},
	},
	"registry.terraform.io/cisco-open/meraki": {
		pulumiProviderName:    "meraki",
		terraformProviderName: "meraki",
		latestVersionByTerraformMajor: map[int]string{
			0: "v0.3.0",
		},
	},
	"registry.opentofu.org/cisco-open/meraki": {
		pulumiProviderName:    "meraki",
		terraformProviderName: "meraki",
		latestVersionByTerraformMajor: map[int]string{
			0: "v0.3.0",
		},
	},
	"registry.terraform.io/aminueza/minio": {
		pulumiProviderName:    "minio",
		terraformProviderName: "minio",
		latestVersionByTerraformMajor: map[int]string{
			1: "v0.15.1",
		},
	},
	"registry.opentofu.org/aminueza/minio": {
		pulumiProviderName:    "minio",
		terraformProviderName: "minio",
		latestVersionByTerraformMajor: map[int]string{
			1: "v0.15.1",
		},
	},
	"registry.terraform.io/mongodb/mongodbatlas": {
		pulumiProviderName:    "mongodbatlas",
		terraformProviderName: "mongodbatlas",
		latestVersionByTerraformMajor: map[int]string{
			1: "v3.37.0",
		},
	},
	"registry.opentofu.org/mongodb/mongodbatlas": {
		pulumiProviderName:    "mongodbatlas",
		terraformProviderName: "mongodbatlas",
		latestVersionByTerraformMajor: map[int]string{
			1: "v3.37.0",
		},
	},
	"registry.terraform.io/newrelic/newrelic": {
		pulumiProviderName:    "newrelic",
		terraformProviderName: "newrelic",
		latestVersionByTerraformMajor: map[int]string{
			3: "v5.57.3",
		},
	},
	"registry.opentofu.org/newrelic/newrelic": {
		pulumiProviderName:    "newrelic",
		terraformProviderName: "newrelic",
		latestVersionByTerraformMajor: map[int]string{
			3: "v5.57.3",
		},
	},
	"registry.terraform.io/ns1-terraform/ns1": {
		pulumiProviderName:    "ns1",
		terraformProviderName: "ns1",
		latestVersionByTerraformMajor: map[int]string{
			1: "v2.3.3",
			2: "v3.7.3",
		},
	},
	"registry.opentofu.org/ns1-terraform/ns1": {
		pulumiProviderName:    "ns1",
		terraformProviderName: "ns1",
		latestVersionByTerraformMajor: map[int]string{
			1: "v2.3.3",
			2: "v3.7.3",
		},
	},
	"registry.terraform.io/oracle/oci": {
		pulumiProviderName:    "oci",
		terraformProviderName: "oci",
		latestVersionByTerraformMajor: map[int]string{
			5: "v1.41.0",
			6: "v2.33.0",
			7: "v3.12.0",
		},
	},
	"registry.opentofu.org/oracle/oci": {
		pulumiProviderName:    "oci",
		terraformProviderName: "oci",
		latestVersionByTerraformMajor: map[int]string{
			5: "v1.41.0",
			6: "v2.33.0",
			7: "v3.12.0",
		},
	},
	"registry.terraform.io/okta/okta": {
		pulumiProviderName:    "okta",
		terraformProviderName: "okta",
		latestVersionByTerraformMajor: map[int]string{
			3: "v3.23.0",
			4: "v4.19.0",
			5: "v5.2.0",
			6: "v6.1.0",
		},
	},
	"registry.opentofu.org/okta/okta": {
		pulumiProviderName:    "okta",
		terraformProviderName: "okta",
		latestVersionByTerraformMajor: map[int]string{
			3: "v3.23.0",
			4: "v4.19.0",
			5: "v5.2.0",
			6: "v6.1.0",
		},
	},
	"registry.terraform.io/terraform-provider-openstack/openstack": {
		pulumiProviderName:    "openstack",
		terraformProviderName: "openstack",
		latestVersionByTerraformMajor: map[int]string{
			1: "v3.15.1",
			2: "v4.1.0",
			3: "v5.4.0",
		},
	},
	"registry.opentofu.org/terraform-provider-openstack/openstack": {
		pulumiProviderName:    "openstack",
		terraformProviderName: "openstack",
		latestVersionByTerraformMajor: map[int]string{
			1: "v3.15.1",
			2: "v4.1.0",
			3: "v5.4.0",
		},
	},
	"registry.terraform.io/opsgenie/opsgenie": {
		pulumiProviderName:    "opsgenie",
		terraformProviderName: "opsgenie",
		latestVersionByTerraformMajor: map[int]string{
			0: "v1.3.18",
		},
	},
	"registry.opentofu.org/opsgenie/opsgenie": {
		pulumiProviderName:    "opsgenie",
		terraformProviderName: "opsgenie",
		latestVersionByTerraformMajor: map[int]string{
			0: "v1.3.18",
		},
	},
	"registry.terraform.io/pagerduty/pagerduty": {
		pulumiProviderName:    "pagerduty",
		terraformProviderName: "pagerduty",
		latestVersionByTerraformMajor: map[int]string{
			3: "v4.29.7",
		},
	},
	"registry.opentofu.org/pagerduty/pagerduty": {
		pulumiProviderName:    "pagerduty",
		terraformProviderName: "pagerduty",
		latestVersionByTerraformMajor: map[int]string{
			3: "v4.29.7",
		},
	},
	"registry.terraform.io/cyrilgdn/postgresql": {
		pulumiProviderName:    "postgresql",
		terraformProviderName: "postgresql",
		latestVersionByTerraformMajor: map[int]string{
			1: "v3.16.0",
		},
	},
	"registry.opentofu.org/cyrilgdn/postgresql": {
		pulumiProviderName:    "postgresql",
		terraformProviderName: "postgresql",
		latestVersionByTerraformMajor: map[int]string{
			1: "v3.16.0",
		},
	},
	"registry.terraform.io/cyrilgdn/rabbitmq": {
		pulumiProviderName:    "rabbitmq",
		terraformProviderName: "rabbitmq",
		latestVersionByTerraformMajor: map[int]string{
			1: "v3.4.0",
		},
	},
	"registry.opentofu.org/cyrilgdn/rabbitmq": {
		pulumiProviderName:    "rabbitmq",
		terraformProviderName: "rabbitmq",
		latestVersionByTerraformMajor: map[int]string{
			1: "v3.4.0",
		},
	},
	"registry.terraform.io/rancher/rancher2": {
		pulumiProviderName:    "rancher2",
		terraformProviderName: "rancher2",
		latestVersionByTerraformMajor: map[int]string{
			1: "v3.9.0",
			2: "v4.0.0",
			3: "v5.2.0",
			4: "v6.2.0",
			5: "v7.1.0",
			6: "v8.1.5",
			7: "v9.2.0",
			8: "v10.3.0",
		},
	},
	"registry.opentofu.org/rancher/rancher2": {
		pulumiProviderName:    "rancher2",
		terraformProviderName: "rancher2",
		latestVersionByTerraformMajor: map[int]string{
			1: "v3.9.0",
			2: "v4.0.0",
			3: "v5.2.0",
			4: "v6.2.0",
			5: "v7.1.0",
			6: "v8.1.5",
			7: "v9.2.0",
			8: "v10.3.0",
		},
	},
	"registry.terraform.io/ciscodevnet/sdwan": {
		pulumiProviderName:    "sdwan",
		terraformProviderName: "sdwan",
		latestVersionByTerraformMajor: map[int]string{
			0: "v0.5.1",
		},
	},
	"registry.opentofu.org/ciscodevnet/sdwan": {
		pulumiProviderName:    "sdwan",
		terraformProviderName: "sdwan",
		latestVersionByTerraformMajor: map[int]string{
			0: "v0.5.1",
		},
	},
	"registry.terraform.io/splunk-terraform/signalfx": {
		pulumiProviderName:    "signalfx",
		terraformProviderName: "signalfx",
		latestVersionByTerraformMajor: map[int]string{
			6: "v5.10.0",
			8: "v6.1.0",
			9: "v7.19.3",
		},
	},
	"registry.opentofu.org/splunk-terraform/signalfx": {
		pulumiProviderName:    "signalfx",
		terraformProviderName: "signalfx",
		latestVersionByTerraformMajor: map[int]string{
			6: "v5.10.0",
			8: "v6.1.0",
			9: "v7.19.3",
		},
	},
	"registry.terraform.io/jmatsu/slack": {
		pulumiProviderName:    "slack",
		terraformProviderName: "slack",
		latestVersionByTerraformMajor: map[int]string{
			1: "v0.4.2",
		},
	},
	"registry.opentofu.org/jmatsu/slack": {
		pulumiProviderName:    "slack",
		terraformProviderName: "slack",
		latestVersionByTerraformMajor: map[int]string{
			1: "v0.4.2",
		},
	},
	"registry.terraform.io/snowflake-labs/snowflake": {
		pulumiProviderName:    "snowflake",
		terraformProviderName: "snowflake",
		latestVersionByTerraformMajor: map[int]string{
			0: "v0.62.0",
			1: "v1.3.0",
			2: "v2.10.0",
		},
	},
	"registry.opentofu.org/snowflake-labs/snowflake": {
		pulumiProviderName:    "snowflake",
		terraformProviderName: "snowflake",
		latestVersionByTerraformMajor: map[int]string{
			0: "v0.62.0",
			1: "v1.3.0",
			2: "v2.10.0",
		},
	},
	"registry.terraform.io/splunk/splunk": {
		pulumiProviderName:    "splunk",
		terraformProviderName: "splunk",
		latestVersionByTerraformMajor: map[int]string{
			1: "v1.2.21",
		},
	},
	"registry.opentofu.org/splunk/splunk": {
		pulumiProviderName:    "splunk",
		terraformProviderName: "splunk",
		latestVersionByTerraformMajor: map[int]string{
			1: "v1.2.21",
		},
	},
	"registry.terraform.io/spotinst/spotinst": {
		pulumiProviderName:    "spotinst",
		terraformProviderName: "spotinst",
		latestVersionByTerraformMajor: map[int]string{
			1: "v3.127.0",
		},
	},
	"registry.opentofu.org/spotinst/spotinst": {
		pulumiProviderName:    "spotinst",
		terraformProviderName: "spotinst",
		latestVersionByTerraformMajor: map[int]string{
			1: "v3.127.0",
		},
	},
	"registry.terraform.io/tailscale/tailscale": {
		pulumiProviderName:    "tailscale",
		terraformProviderName: "tailscale",
		latestVersionByTerraformMajor: map[int]string{
			0: "v0.23.0",
		},
	},
	"registry.opentofu.org/tailscale/tailscale": {
		pulumiProviderName:    "tailscale",
		terraformProviderName: "tailscale",
		latestVersionByTerraformMajor: map[int]string{
			0: "v0.23.0",
		},
	},
	"registry.terraform.io/venafi/venafi": {
		pulumiProviderName:    "venafi",
		terraformProviderName: "venafi",
		latestVersionByTerraformMajor: map[int]string{
			0: "v1.12.1",
		},
	},
	"registry.opentofu.org/venafi/venafi": {
		pulumiProviderName:    "venafi",
		terraformProviderName: "venafi",
		latestVersionByTerraformMajor: map[int]string{
			0: "v1.12.1",
		},
	},
	"registry.terraform.io/vmware/wavefront": {
		pulumiProviderName:    "wavefront",
		terraformProviderName: "wavefront",
		latestVersionByTerraformMajor: map[int]string{
			3: "v1.4.0",
			5: "v3.1.0",
		},
	},
	"registry.opentofu.org/vmware/wavefront": {
		pulumiProviderName:    "wavefront",
		terraformProviderName: "wavefront",
		latestVersionByTerraformMajor: map[int]string{
			3: "v1.4.0",
			5: "v3.1.0",
		},
	},
}

func RecommendPulumiProvider(tf TerraformProvider) RecommendedPulumiProvider {
	// Check if there's a bridged provider for this Terraform provider
	mapping, ok := providerMapping[tf.Identifier]
	if !ok {
		// Default to using terraform-provider package if no bridged provider exists
		return RecommendedPulumiProvider{UseTerraformProviderPackage: true}
	}

	// Determine which Pulumi provider version to recommend
	var recommendedVersion string

	// First, try to find a precise match in the refined version map (versions.yaml)
	if refinedVersionMap != nil && tf.Version != "" {
		bridgedProvider := BridgedProvider(mapping.pulumiProviderName)
		if versionPairs, exists := refinedVersionMap.Bridged[bridgedProvider]; exists {
			// Normalize the Terraform version
			tfVersion := strings.TrimPrefix(tf.Version, "v")

			// Search for a precise match in the version pairs
			for _, vp := range versionPairs {
				// Skip entries with errors
				if vp.Error != "" {
					continue
				}

				// Check if the upstream version matches the Terraform version
				upstreamVersion := strings.TrimPrefix(string(vp.Upstream), "v")
				if upstreamVersion == tfVersion {
					// Found a precise match - use this Pulumi version
					recommendedVersion = string(vp.Pulumi)
					break
				}
			}
		}
	}

	// If no precise match found, try to parse the Terraform version and find the matching Pulumi version
	if recommendedVersion == "" && tf.Version != "" {
		// Normalize the version string by removing "v" prefix if present
		versionStr := strings.TrimPrefix(tf.Version, "v")

		// Try to parse the version
		if v, err := semver.Parse(versionStr); err == nil {
			// Successfully parsed - look up by major version
			tfMajor := int(v.Major)
			if pulumiVersion, exists := mapping.latestVersionByTerraformMajor[tfMajor]; exists {
				recommendedVersion = pulumiVersion
			}
		}
	}

	// If we couldn't determine a version from the TF version, use the latest/maximum
	if recommendedVersion == "" {
		recommendedVersion = getLatestVersion(mapping.latestVersionByTerraformMajor)
	}

	return RecommendedPulumiProvider{
		BridgedPulumiProvider: &BridgedPulumiProvider{
			Identifier: mapping.pulumiProviderName,
			Version:    recommendedVersion,
		},
	}
}

// getLatestVersion returns the latest (maximum) version from a map of versions by major version
func getLatestVersion(versionsByMajor map[int]string) string {
	var latestVersion string
	var latestMajor int = -1

	for major, version := range versionsByMajor {
		if major > latestMajor {
			latestMajor = major
			latestVersion = version
		}
	}

	return latestVersion
}
