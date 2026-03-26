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
	"sort"
	"strings"

	"github.com/blang/semver/v4"
	"github.com/pulumi/pulumi/sdk/v3/go/common/util/contract"
	"gopkg.in/yaml.v3"
)

//go:embed versions.yaml
var embeddedVersionsYAML []byte

var refinedVersionMap *VersionMap

func init() {
	// Load the embedded versions.yaml file
	var vm VersionMap
	err := yaml.Unmarshal(embeddedVersionsYAML, &vm)
	contract.AssertNoErrorf(err, "versions.yaml does not unmarshal")
	refinedVersionMap = &vm
}

// A full name such as "registry.terraform.io/hashicorp/aws"
type TerraformProviderName string

type TerraformProvider struct {
	// Identifier such as "registry.opentofu.org/hashicorp/aws" or "registry.opentofu.org/hashicorp/aws"
	Identifier TerraformProviderName

	// Version such as v3.18.0
	Version string
}

// BridgedPulumiProvider represents a statically bridged Pulumi provider.
// Statically bridged providers have their own dedicated repository (e.g., pulumi/pulumi-aws)
// and are pre-built with schema information embedded.
type BridgedPulumiProvider struct {
	// Identifier such as "aws"
	Identifier string

	// Pulumi version such as "v7.12.0"
	Version string
}

// RecommendedPulumiProvider represents the recommended way to use a Terraform provider with Pulumi.
// It will recommend either:
//   - A statically bridged provider
//   - Dynamic bridging via the terraform-provider package
type RecommendedPulumiProvider struct {
	// UseDynamicBridging indicates that the terraform-provider package should be used
	// to dynamically bridge this Terraform provider. This is for providers that don't
	// have a dedicated statically bridged Pulumi provider.
	// See https://www.pulumi.com/registry/packages/terraform-provider/
	UseDynamicBridging bool

	// StaticallyBridgedProvider contains information about the statically bridged provider
	// to use, if one exists.
	StaticallyBridgedProvider *BridgedPulumiProvider
}

type providerMappingDetail struct {
	pulumiProviderName    string
	terraformProviderName string // Name of the upstream Terraform provider (e.g., "aws" for terraform-provider-aws)
}

// providerMapping maps Terraform/OpenTofu provider identifiers to Pulumi provider names.
// This is based on the provider list from https://github.com/pulumi/ci-mgmt/blob/master/provider-ci/providers.json
var providerMapping = map[TerraformProviderName]providerMappingDetail{
	// HashiCorp providers
	"registry.terraform.io/hashicorp/aws": {
		pulumiProviderName:    "aws",
		terraformProviderName: "aws",
	},
	"registry.opentofu.org/hashicorp/aws": {
		pulumiProviderName:    "aws",
		terraformProviderName: "aws",
	},
	"registry.terraform.io/hashicorp/azurerm": {
		pulumiProviderName:    "azure",
		terraformProviderName: "azurerm",
	},
	"registry.opentofu.org/hashicorp/azurerm": {
		pulumiProviderName:    "azure",
		terraformProviderName: "azurerm",
	},
	"registry.terraform.io/hashicorp/azuread": {
		pulumiProviderName:    "azuread",
		terraformProviderName: "azuread",
	},
	"registry.opentofu.org/hashicorp/azuread": {
		pulumiProviderName:    "azuread",
		terraformProviderName: "azuread",
	},
	"registry.terraform.io/hashicorp/google": {
		pulumiProviderName:    "gcp",
		terraformProviderName: "google",
	},
	"registry.opentofu.org/hashicorp/google": {
		pulumiProviderName:    "gcp",
		terraformProviderName: "google",
	},
	"registry.terraform.io/hashicorp/google-beta": {
		pulumiProviderName:    "gcp",
		terraformProviderName: "google",
	},
	"registry.opentofu.org/hashicorp/google-beta": {
		pulumiProviderName:    "gcp",
		terraformProviderName: "google",
	},
	"registry.terraform.io/hashicorp/consul": {
		pulumiProviderName:    "consul",
		terraformProviderName: "consul",
	},
	"registry.opentofu.org/hashicorp/consul": {
		pulumiProviderName:    "consul",
		terraformProviderName: "consul",
	},
	"registry.terraform.io/hashicorp/vault": {
		pulumiProviderName:    "vault",
		terraformProviderName: "vault",
	},
	"registry.opentofu.org/hashicorp/vault": {
		pulumiProviderName:    "vault",
		terraformProviderName: "vault",
	},
	"registry.terraform.io/hashicorp/nomad": {
		pulumiProviderName:    "nomad",
		terraformProviderName: "nomad",
	},
	"registry.opentofu.org/hashicorp/nomad": {
		pulumiProviderName:    "nomad",
		terraformProviderName: "nomad",
	},
	"registry.terraform.io/hashicorp/vsphere": {
		pulumiProviderName:    "vsphere",
		terraformProviderName: "vsphere",
	},
	"registry.opentofu.org/hashicorp/vsphere": {
		pulumiProviderName:    "vsphere",
		terraformProviderName: "vsphere",
	},
	"registry.terraform.io/hashicorp/random": {
		pulumiProviderName:    "random",
		terraformProviderName: "random",
	},
	"registry.opentofu.org/hashicorp/random": {
		pulumiProviderName:    "random",
		terraformProviderName: "random",
	},
	"registry.terraform.io/hashicorp/archive": {
		pulumiProviderName:    "archive",
		terraformProviderName: "archive",
	},
	"registry.opentofu.org/hashicorp/archive": {
		pulumiProviderName:    "archive",
		terraformProviderName: "archive",
	},
	"registry.terraform.io/hashicorp/tls": {
		pulumiProviderName:    "tls",
		terraformProviderName: "tls",
	},
	"registry.opentofu.org/hashicorp/tls": {
		pulumiProviderName:    "tls",
		terraformProviderName: "tls",
	},
	"registry.terraform.io/hashicorp/external": {
		pulumiProviderName:    "external",
		terraformProviderName: "external",
	},
	"registry.opentofu.org/hashicorp/external": {
		pulumiProviderName:    "external",
		terraformProviderName: "external",
	},
	"registry.terraform.io/hashicorp/http": {
		pulumiProviderName:    "http",
		terraformProviderName: "http",
	},
	"registry.opentofu.org/hashicorp/http": {
		pulumiProviderName:    "http",
		terraformProviderName: "http",
	},
	"registry.terraform.io/hashicorp/null": {
		pulumiProviderName:    "null",
		terraformProviderName: "null",
	},
	"registry.opentofu.org/hashicorp/null": {
		pulumiProviderName:    "null",
		terraformProviderName: "null",
	},
	"registry.terraform.io/hashicorp/cloudinit": {
		pulumiProviderName:    "cloudinit",
		terraformProviderName: "cloudinit",
	},
	"registry.opentofu.org/hashicorp/cloudinit": {
		pulumiProviderName:    "cloudinit",
		terraformProviderName: "cloudinit",
	},

	// Third-party providers
	"registry.terraform.io/aiven/aiven": {
		pulumiProviderName:    "aiven",
		terraformProviderName: "aiven",
	},
	"registry.opentofu.org/aiven/aiven": {
		pulumiProviderName:    "aiven",
		terraformProviderName: "aiven",
	},
	"registry.terraform.io/akamai/akamai": {
		pulumiProviderName:    "akamai",
		terraformProviderName: "akamai",
	},
	"registry.opentofu.org/akamai/akamai": {
		pulumiProviderName:    "akamai",
		terraformProviderName: "akamai",
	},
	"registry.terraform.io/aliyun/alicloud": {
		pulumiProviderName:    "alicloud",
		terraformProviderName: "alicloud",
	},
	"registry.opentofu.org/aliyun/alicloud": {
		pulumiProviderName:    "alicloud",
		terraformProviderName: "alicloud",
	},
	"registry.terraform.io/jfrog/artifactory": {
		pulumiProviderName:    "artifactory",
		terraformProviderName: "artifactory",
	},
	"registry.opentofu.org/jfrog/artifactory": {
		pulumiProviderName:    "artifactory",
		terraformProviderName: "artifactory",
	},
	"registry.terraform.io/auth0/auth0": {
		pulumiProviderName:    "auth0",
		terraformProviderName: "auth0",
	},
	"registry.opentofu.org/auth0/auth0": {
		pulumiProviderName:    "auth0",
		terraformProviderName: "auth0",
	},
	"registry.terraform.io/microsoft/azuredevops": {
		pulumiProviderName:    "azuredevops",
		terraformProviderName: "azuredevops",
	},
	"registry.opentofu.org/microsoft/azuredevops": {
		pulumiProviderName:    "azuredevops",
		terraformProviderName: "azuredevops",
	},
	"registry.terraform.io/cloudamqp/cloudamqp": {
		pulumiProviderName:    "cloudamqp",
		terraformProviderName: "cloudamqp",
	},
	"registry.opentofu.org/cloudamqp/cloudamqp": {
		pulumiProviderName:    "cloudamqp",
		terraformProviderName: "cloudamqp",
	},
	"registry.terraform.io/cloudflare/cloudflare": {
		pulumiProviderName:    "cloudflare",
		terraformProviderName: "cloudflare",
	},
	"registry.opentofu.org/cloudflare/cloudflare": {
		pulumiProviderName:    "cloudflare",
		terraformProviderName: "cloudflare",
	},
	"registry.terraform.io/confluentinc/confluent": {
		pulumiProviderName:    "confluentcloud",
		terraformProviderName: "confluent",
	},
	"registry.opentofu.org/confluentinc/confluent": {
		pulumiProviderName:    "confluentcloud",
		terraformProviderName: "confluent",
	},
	"registry.terraform.io/databricks/databricks": {
		pulumiProviderName:    "databricks",
		terraformProviderName: "databricks",
	},
	"registry.opentofu.org/databricks/databricks": {
		pulumiProviderName:    "databricks",
		terraformProviderName: "databricks",
	},
	"registry.terraform.io/datadog/datadog": {
		pulumiProviderName:    "datadog",
		terraformProviderName: "datadog",
	},
	"registry.opentofu.org/datadog/datadog": {
		pulumiProviderName:    "datadog",
		terraformProviderName: "datadog",
	},
	"registry.terraform.io/dbt-labs/dbtcloud": {
		pulumiProviderName:    "dbtcloud",
		terraformProviderName: "dbtcloud",
	},
	"registry.opentofu.org/dbt-labs/dbtcloud": {
		pulumiProviderName:    "dbtcloud",
		terraformProviderName: "dbtcloud",
	},
	"registry.terraform.io/digitalocean/digitalocean": {
		pulumiProviderName:    "digitalocean",
		terraformProviderName: "digitalocean",
	},
	"registry.opentofu.org/digitalocean/digitalocean": {
		pulumiProviderName:    "digitalocean",
		terraformProviderName: "digitalocean",
	},
	"registry.terraform.io/dnsimple/dnsimple": {
		pulumiProviderName:    "dnsimple",
		terraformProviderName: "dnsimple",
	},
	"registry.opentofu.org/dnsimple/dnsimple": {
		pulumiProviderName:    "dnsimple",
		terraformProviderName: "dnsimple",
	},
	"registry.terraform.io/kreuzwerker/docker": {
		pulumiProviderName:    "docker",
		terraformProviderName: "docker",
	},
	"registry.opentofu.org/kreuzwerker/docker": {
		pulumiProviderName:    "docker",
		terraformProviderName: "docker",
	},
	"registry.terraform.io/elastic/ec": {
		pulumiProviderName:    "ec",
		terraformProviderName: "ec",
	},
	"registry.opentofu.org/elastic/ec": {
		pulumiProviderName:    "ec",
		terraformProviderName: "ec",
	},
	"registry.terraform.io/f5networks/bigip": {
		pulumiProviderName:    "f5bigip",
		terraformProviderName: "bigip",
	},
	"registry.opentofu.org/f5networks/bigip": {
		pulumiProviderName:    "f5bigip",
		terraformProviderName: "bigip",
	},
	"registry.terraform.io/fastly/fastly": {
		pulumiProviderName:    "fastly",
		terraformProviderName: "fastly",
	},
	"registry.opentofu.org/fastly/fastly": {
		pulumiProviderName:    "fastly",
		terraformProviderName: "fastly",
	},
	"registry.terraform.io/integrations/github": {
		pulumiProviderName:    "github",
		terraformProviderName: "github",
	},
	"registry.opentofu.org/integrations/github": {
		pulumiProviderName:    "github",
		terraformProviderName: "github",
	},
	"registry.terraform.io/gitlabhq/gitlab": {
		pulumiProviderName:    "gitlab",
		terraformProviderName: "gitlab",
	},
	"registry.opentofu.org/gitlabhq/gitlab": {
		pulumiProviderName:    "gitlab",
		terraformProviderName: "gitlab",
	},
	"registry.terraform.io/harness/harness": {
		pulumiProviderName:    "harness",
		terraformProviderName: "harness",
	},
	"registry.opentofu.org/harness/harness": {
		pulumiProviderName:    "harness",
		terraformProviderName: "harness",
	},
	"registry.terraform.io/hetznercloud/hcloud": {
		pulumiProviderName:    "hcloud",
		terraformProviderName: "hcloud",
	},
	"registry.opentofu.org/hetznercloud/hcloud": {
		pulumiProviderName:    "hcloud",
		terraformProviderName: "hcloud",
	},
	"registry.terraform.io/ciscodevnet/ise": {
		pulumiProviderName:    "ise",
		terraformProviderName: "ise",
	},
	"registry.opentofu.org/ciscodevnet/ise": {
		pulumiProviderName:    "ise",
		terraformProviderName: "ise",
	},
	"registry.terraform.io/Juniper/mist": {
		pulumiProviderName:    "junipermist",
		terraformProviderName: "mist",
	},
	"registry.terraform.io/mongey/kafka": {
		pulumiProviderName:    "kafka",
		terraformProviderName: "kafka",
	},
	"registry.opentofu.org/mongey/kafka": {
		pulumiProviderName:    "kafka",
		terraformProviderName: "kafka",
	},
	"registry.terraform.io/mrparkers/keycloak": {
		pulumiProviderName:    "keycloak",
		terraformProviderName: "keycloak",
	},
	"registry.opentofu.org/mrparkers/keycloak": {
		pulumiProviderName:    "keycloak",
		terraformProviderName: "keycloak",
	},
	"registry.terraform.io/kevholditch/kong": {
		pulumiProviderName:    "kong",
		terraformProviderName: "kong",
	},
	"registry.opentofu.org/kevholditch/kong": {
		pulumiProviderName:    "kong",
		terraformProviderName: "kong",
	},
	"registry.terraform.io/linode/linode": {
		pulumiProviderName:    "linode",
		terraformProviderName: "linode",
	},
	"registry.opentofu.org/linode/linode": {
		pulumiProviderName:    "linode",
		terraformProviderName: "linode",
	},
	"registry.terraform.io/wgebis/mailgun": {
		pulumiProviderName:    "mailgun",
		terraformProviderName: "mailgun",
	},
	"registry.opentofu.org/wgebis/mailgun": {
		pulumiProviderName:    "mailgun",
		terraformProviderName: "mailgun",
	},
	"registry.terraform.io/cisco-open/meraki": {
		pulumiProviderName:    "meraki",
		terraformProviderName: "meraki",
	},
	"registry.opentofu.org/cisco-open/meraki": {
		pulumiProviderName:    "meraki",
		terraformProviderName: "meraki",
	},
	"registry.terraform.io/aminueza/minio": {
		pulumiProviderName:    "minio",
		terraformProviderName: "minio",
	},
	"registry.opentofu.org/aminueza/minio": {
		pulumiProviderName:    "minio",
		terraformProviderName: "minio",
	},
	"registry.terraform.io/mongodb/mongodbatlas": {
		pulumiProviderName:    "mongodbatlas",
		terraformProviderName: "mongodbatlas",
	},
	"registry.opentofu.org/mongodb/mongodbatlas": {
		pulumiProviderName:    "mongodbatlas",
		terraformProviderName: "mongodbatlas",
	},
	"registry.terraform.io/newrelic/newrelic": {
		pulumiProviderName:    "newrelic",
		terraformProviderName: "newrelic",
	},
	"registry.opentofu.org/newrelic/newrelic": {
		pulumiProviderName:    "newrelic",
		terraformProviderName: "newrelic",
	},
	"registry.terraform.io/ns1-terraform/ns1": {
		pulumiProviderName:    "ns1",
		terraformProviderName: "ns1",
	},
	"registry.opentofu.org/ns1-terraform/ns1": {
		pulumiProviderName:    "ns1",
		terraformProviderName: "ns1",
	},
	"registry.terraform.io/oracle/oci": {
		pulumiProviderName:    "oci",
		terraformProviderName: "oci",
	},
	"registry.opentofu.org/oracle/oci": {
		pulumiProviderName:    "oci",
		terraformProviderName: "oci",
	},
	"registry.terraform.io/okta/okta": {
		pulumiProviderName:    "okta",
		terraformProviderName: "okta",
	},
	"registry.opentofu.org/okta/okta": {
		pulumiProviderName:    "okta",
		terraformProviderName: "okta",
	},
	"registry.terraform.io/terraform-provider-openstack/openstack": {
		pulumiProviderName:    "openstack",
		terraformProviderName: "openstack",
	},
	"registry.opentofu.org/terraform-provider-openstack/openstack": {
		pulumiProviderName:    "openstack",
		terraformProviderName: "openstack",
	},
	"registry.terraform.io/opsgenie/opsgenie": {
		pulumiProviderName:    "opsgenie",
		terraformProviderName: "opsgenie",
	},
	"registry.opentofu.org/opsgenie/opsgenie": {
		pulumiProviderName:    "opsgenie",
		terraformProviderName: "opsgenie",
	},
	"registry.terraform.io/pagerduty/pagerduty": {
		pulumiProviderName:    "pagerduty",
		terraformProviderName: "pagerduty",
	},
	"registry.opentofu.org/pagerduty/pagerduty": {
		pulumiProviderName:    "pagerduty",
		terraformProviderName: "pagerduty",
	},
	"registry.terraform.io/cyrilgdn/postgresql": {
		pulumiProviderName:    "postgresql",
		terraformProviderName: "postgresql",
	},
	"registry.opentofu.org/cyrilgdn/postgresql": {
		pulumiProviderName:    "postgresql",
		terraformProviderName: "postgresql",
	},
	"registry.terraform.io/cyrilgdn/rabbitmq": {
		pulumiProviderName:    "rabbitmq",
		terraformProviderName: "rabbitmq",
	},
	"registry.opentofu.org/cyrilgdn/rabbitmq": {
		pulumiProviderName:    "rabbitmq",
		terraformProviderName: "rabbitmq",
	},
	"registry.terraform.io/rancher/rancher2": {
		pulumiProviderName:    "rancher2",
		terraformProviderName: "rancher2",
	},
	"registry.opentofu.org/rancher/rancher2": {
		pulumiProviderName:    "rancher2",
		terraformProviderName: "rancher2",
	},
	"registry.terraform.io/ciscodevnet/sdwan": {
		pulumiProviderName:    "sdwan",
		terraformProviderName: "sdwan",
	},
	"registry.opentofu.org/ciscodevnet/sdwan": {
		pulumiProviderName:    "sdwan",
		terraformProviderName: "sdwan",
	},
	"registry.terraform.io/splunk-terraform/signalfx": {
		pulumiProviderName:    "signalfx",
		terraformProviderName: "signalfx",
	},
	"registry.opentofu.org/splunk-terraform/signalfx": {
		pulumiProviderName:    "signalfx",
		terraformProviderName: "signalfx",
	},
	"registry.terraform.io/jmatsu/slack": {
		pulumiProviderName:    "slack",
		terraformProviderName: "slack",
	},
	"registry.opentofu.org/jmatsu/slack": {
		pulumiProviderName:    "slack",
		terraformProviderName: "slack",
	},
	"registry.terraform.io/snowflake-labs/snowflake": {
		pulumiProviderName:    "snowflake",
		terraformProviderName: "snowflake",
	},
	"registry.opentofu.org/snowflake-labs/snowflake": {
		pulumiProviderName:    "snowflake",
		terraformProviderName: "snowflake",
	},
	"registry.terraform.io/splunk/splunk": {
		pulumiProviderName:    "splunk",
		terraformProviderName: "splunk",
	},
	"registry.opentofu.org/splunk/splunk": {
		pulumiProviderName:    "splunk",
		terraformProviderName: "splunk",
	},
	"registry.terraform.io/spotinst/spotinst": {
		pulumiProviderName:    "spotinst",
		terraformProviderName: "spotinst",
	},
	"registry.opentofu.org/spotinst/spotinst": {
		pulumiProviderName:    "spotinst",
		terraformProviderName: "spotinst",
	},
	"registry.terraform.io/tailscale/tailscale": {
		pulumiProviderName:    "tailscale",
		terraformProviderName: "tailscale",
	},
	"registry.opentofu.org/tailscale/tailscale": {
		pulumiProviderName:    "tailscale",
		terraformProviderName: "tailscale",
	},
	"registry.terraform.io/venafi/venafi": {
		pulumiProviderName:    "venafi",
		terraformProviderName: "venafi",
	},
	"registry.opentofu.org/venafi/venafi": {
		pulumiProviderName:    "venafi",
		terraformProviderName: "venafi",
	},
	"registry.terraform.io/vmware/wavefront": {
		pulumiProviderName:    "wavefront",
		terraformProviderName: "wavefront",
	},
	"registry.opentofu.org/vmware/wavefront": {
		pulumiProviderName:    "wavefront",
		terraformProviderName: "wavefront",
	},
}

func RecommendPulumiProvider(tf TerraformProvider) RecommendedPulumiProvider {
	// Check if there is a bridged provider for this Terraform provider
	mapping, ok := providerMapping[tf.Identifier]
	if !ok {
		// Default to using terraform-provider package if no bridged provider exists
		return RecommendedPulumiProvider{UseDynamicBridging: true}
	}
	bridgedProvider := BridgedProvider(mapping.pulumiProviderName)
	originalVersionPairs, exists := refinedVersionMap.Bridged[bridgedProvider]
	if !exists {
		return RecommendedPulumiProvider{UseDynamicBridging: true}
	}
	versionPairs := []VersionPair{}
	for _, vp := range originalVersionPairs {
		if vp.Error != "" {
			continue
		}
		versionPairs = append(versionPairs, vp)
	}

	// Normalize the Terraform version, normalize invalid versions to empty.
	tfVersion := strings.TrimPrefix(tf.Version, "v")
	if _, err := semver.Parse(tfVersion); err != nil {
		tfVersion = ""
	}

	// Determine which Pulumi provider version to recommend.
	var recommendedVersion ReleaseTag

	if tfVersion == "" {
		// If no desired TF version, use latest.
		recommendedVersion = latestVersion(versionPairs)
	} else {
		tfSemver, err := semver.Parse(tfVersion)
		contract.AssertNoErrorf(err, "A non-empty tfVersion should be a valid semver: %v", err)

		// Pass 1: Check for exact match
		for _, vp := range versionPairs {
			upstreamSemver := vp.Upstream.Semver()
			if upstreamSemver.EQ(tfSemver) {
				recommendedVersion = vp.Pulumi
				break
			}
		}

		// Pass 2: If no exact match found, find the largest upstream < tfVersion in the same major.
		if recommendedVersion == "" {
			sort.Slice(versionPairs, func(i, j int) bool {
				return versionPairs[i].Upstream.Semver().GT(versionPairs[j].Upstream.Semver())
			})
			for _, vp := range versionPairs {
				upstreamSemver := vp.Upstream.Semver()
				if upstreamSemver.Major != tfSemver.Major {
					continue
				}
				if upstreamSemver.LE(tfSemver) {
					recommendedVersion = vp.Pulumi
					break
				}
			}
		}

		// If we still did not find a recommended version, fall back to latest, by major or generally.
		if recommendedVersion == "" {
			v, ok := latestVersionMatchingMajor(originalVersionPairs, tfSemver)
			if !ok {
				v = latestVersion(originalVersionPairs)
			}
			recommendedVersion = v
		}
	}

	return RecommendedPulumiProvider{
		StaticallyBridgedProvider: &BridgedPulumiProvider{
			Identifier: mapping.pulumiProviderName,
			Version:    string(recommendedVersion),
		},
	}
}

// Assumes versionPairs are ordered with latest first.
func latestVersion(versionPairs []VersionPair) ReleaseTag {
	for _, vp := range versionPairs {
		if vp.Error != "" {
			continue
		}
		return vp.Pulumi
	}
	contract.Failf("Expected to always find the latest version in data")
	return ""
}

// Assumes versionPairs are ordered with latest first.
func latestVersionMatchingMajor(versionPairs []VersionPair, v semver.Version) (ReleaseTag, bool) {
	major := v.Major
	for _, vp := range versionPairs {
		if vp.Error != "" {
			continue
		}
		if vp.Upstream.Semver().Major != major {
			continue
		}
		return vp.Pulumi, true
	}
	return "", false
}

// GetUpstreamVersion returns the TF provider version for the given TF provider address
// and Pulumi provider version. If pulumiVersion is empty, returns the latest version.
// This is useful for loading the TF provider binary for state upgrades.
func GetUpstreamVersion(addr TerraformProviderName, pulumiVersion string) (string, bool) {
	mapping, ok := providerMapping[addr]
	if !ok {
		return "", false
	}

	versionPairs := refinedVersionMap.Bridged[BridgedProvider(mapping.pulumiProviderName)]
	if len(versionPairs) == 0 {
		return "", false
	}

	// Normalize version format
	pulumiVersion = strings.TrimPrefix(pulumiVersion, "v")

	for _, vp := range versionPairs {
		if vp.Error != "" {
			continue
		}
		// If no version specified, return latest (first valid entry)
		if pulumiVersion == "" {
			return strings.TrimPrefix(string(vp.Upstream), "v"), true
		}
		// Match specific version
		if strings.TrimPrefix(string(vp.Pulumi), "v") == pulumiVersion {
			return strings.TrimPrefix(string(vp.Upstream), "v"), true
		}
	}
	return "", false
}
