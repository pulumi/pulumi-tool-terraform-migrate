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

type TerraformProvider struct {
	// Identifier such as "registry.opentofu.org/hashicorp/aws" or "registry.opentofu.org/hashicorp/aws"
	Identifier string

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

// providerMapping maps Terraform/OpenTOFU provider identifiers to Pulumi provider names.
// This is based on the provider list from https://github.com/pulumi/ci-mgmt/blob/master/provider-ci/providers.json
var providerMapping = map[string]string{
	// HashiCorp providers
	"registry.terraform.io/hashicorp/aws":       "aws",
	"registry.opentofu.org/hashicorp/aws":       "aws",
	"registry.terraform.io/hashicorp/azurerm":   "azure",
	"registry.opentofu.org/hashicorp/azurerm":   "azure",
	"registry.terraform.io/hashicorp/azuread":   "azuread",
	"registry.opentofu.org/hashicorp/azuread":   "azuread",
	"registry.terraform.io/hashicorp/google":    "gcp",
	"registry.opentofu.org/hashicorp/google":    "gcp",
	"registry.terraform.io/hashicorp/consul":    "consul",
	"registry.opentofu.org/hashicorp/consul":    "consul",
	"registry.terraform.io/hashicorp/vault":     "vault",
	"registry.opentofu.org/hashicorp/vault":     "vault",
	"registry.terraform.io/hashicorp/nomad":     "nomad",
	"registry.opentofu.org/hashicorp/nomad":     "nomad",
	"registry.terraform.io/hashicorp/vsphere":   "vsphere",
	"registry.opentofu.org/hashicorp/vsphere":   "vsphere",
	"registry.terraform.io/hashicorp/random":    "random",
	"registry.opentofu.org/hashicorp/random":    "random",
	"registry.terraform.io/hashicorp/archive":   "archive",
	"registry.opentofu.org/hashicorp/archive":   "archive",
	"registry.terraform.io/hashicorp/tls":       "tls",
	"registry.opentofu.org/hashicorp/tls":       "tls",
	"registry.terraform.io/hashicorp/external":  "external",
	"registry.opentofu.org/hashicorp/external":  "external",
	"registry.terraform.io/hashicorp/http":      "http",
	"registry.opentofu.org/hashicorp/http":      "http",
	"registry.terraform.io/hashicorp/null":      "null",
	"registry.opentofu.org/hashicorp/null":      "null",
	"registry.terraform.io/hashicorp/cloudinit": "cloudinit",
	"registry.opentofu.org/hashicorp/cloudinit": "cloudinit",

	// Third-party providers
	"registry.terraform.io/aiven/aiven":                            "aiven",
	"registry.opentofu.org/aiven/aiven":                            "aiven",
	"registry.terraform.io/akamai/akamai":                          "akamai",
	"registry.opentofu.org/akamai/akamai":                          "akamai",
	"registry.terraform.io/aliyun/alicloud":                        "alicloud",
	"registry.opentofu.org/aliyun/alicloud":                        "alicloud",
	"registry.terraform.io/jfrog/artifactory":                      "artifactory",
	"registry.opentofu.org/jfrog/artifactory":                      "artifactory",
	"registry.terraform.io/auth0/auth0":                            "auth0",
	"registry.opentofu.org/auth0/auth0":                            "auth0",
	"registry.terraform.io/microsoft/azuredevops":                  "azuredevops",
	"registry.opentofu.org/microsoft/azuredevops":                  "azuredevops",
	"registry.terraform.io/cloudamqp/cloudamqp":                    "cloudamqp",
	"registry.opentofu.org/cloudamqp/cloudamqp":                    "cloudamqp",
	"registry.terraform.io/cloudflare/cloudflare":                  "cloudflare",
	"registry.opentofu.org/cloudflare/cloudflare":                  "cloudflare",
	"registry.terraform.io/paloaltonetworks/cloudngfwaws":          "cloudngfwaws",
	"registry.opentofu.org/paloaltonetworks/cloudngfwaws":          "cloudngfwaws",
	"registry.terraform.io/confluentinc/confluent":                 "confluentcloud",
	"registry.opentofu.org/confluentinc/confluent":                 "confluentcloud",
	"registry.terraform.io/databricks/databricks":                  "databricks",
	"registry.opentofu.org/databricks/databricks":                  "databricks",
	"registry.terraform.io/datadog/datadog":                        "datadog",
	"registry.opentofu.org/datadog/datadog":                        "datadog",
	"registry.terraform.io/dbt-labs/dbtcloud":                      "dbtcloud",
	"registry.opentofu.org/dbt-labs/dbtcloud":                      "dbtcloud",
	"registry.terraform.io/digitalocean/digitalocean":              "digitalocean",
	"registry.opentofu.org/digitalocean/digitalocean":              "digitalocean",
	"registry.terraform.io/dnsimple/dnsimple":                      "dnsimple",
	"registry.opentofu.org/dnsimple/dnsimple":                      "dnsimple",
	"registry.terraform.io/kreuzwerker/docker":                     "docker",
	"registry.opentofu.org/kreuzwerker/docker":                     "docker",
	"registry.terraform.io/elastic/ec":                             "ec",
	"registry.opentofu.org/elastic/ec":                             "ec",
	"registry.terraform.io/f5networks/bigip":                       "f5bigip",
	"registry.opentofu.org/f5networks/bigip":                       "f5bigip",
	"registry.terraform.io/fastly/fastly":                          "fastly",
	"registry.opentofu.org/fastly/fastly":                          "fastly",
	"registry.terraform.io/integrations/github":                    "github",
	"registry.opentofu.org/integrations/github":                    "github",
	"registry.terraform.io/gitlabhq/gitlab":                        "gitlab",
	"registry.opentofu.org/gitlabhq/gitlab":                        "gitlab",
	"registry.terraform.io/harness/harness":                        "harness",
	"registry.opentofu.org/harness/harness":                        "harness",
	"registry.terraform.io/hetznercloud/hcloud":                    "hcloud",
	"registry.opentofu.org/hetznercloud/hcloud":                    "hcloud",
	"registry.terraform.io/ciscodevnet/ise":                        "ise",
	"registry.opentofu.org/ciscodevnet/ise":                        "ise",
	"registry.terraform.io/Juniper/mist":                           "junipermist",
	"registry.opentofu.org/Juniper/mist":                           "junipermist",
	"registry.terraform.io/mongey/kafka":                           "kafka",
	"registry.opentofu.org/mongey/kafka":                           "kafka",
	"registry.terraform.io/mrparkers/keycloak":                     "keycloak",
	"registry.opentofu.org/mrparkers/keycloak":                     "keycloak",
	"registry.terraform.io/kevholditch/kong":                       "kong",
	"registry.opentofu.org/kevholditch/kong":                       "kong",
	"registry.terraform.io/hashicorp/kubernetes":                   "kubernetes",
	"registry.opentofu.org/hashicorp/kubernetes":                   "kubernetes",
	"registry.terraform.io/linode/linode":                          "linode",
	"registry.opentofu.org/linode/linode":                          "linode",
	"registry.terraform.io/wgebis/mailgun":                         "mailgun",
	"registry.opentofu.org/wgebis/mailgun":                         "mailgun",
	"registry.terraform.io/cisco-open/meraki":                      "meraki",
	"registry.opentofu.org/cisco-open/meraki":                      "meraki",
	"registry.terraform.io/aminueza/minio":                         "minio",
	"registry.opentofu.org/aminueza/minio":                         "minio",
	"registry.terraform.io/mongodb/mongodbatlas":                   "mongodbatlas",
	"registry.opentofu.org/mongodb/mongodbatlas":                   "mongodbatlas",
	"registry.terraform.io/newrelic/newrelic":                      "newrelic",
	"registry.opentofu.org/newrelic/newrelic":                      "newrelic",
	"registry.terraform.io/ns1-terraform/ns1":                      "ns1",
	"registry.opentofu.org/ns1-terraform/ns1":                      "ns1",
	"registry.terraform.io/oracle/oci":                             "oci",
	"registry.opentofu.org/oracle/oci":                             "oci",
	"registry.terraform.io/okta/okta":                              "okta",
	"registry.opentofu.org/okta/okta":                              "okta",
	"registry.terraform.io/terraform-provider-openstack/openstack": "openstack",
	"registry.opentofu.org/terraform-provider-openstack/openstack": "openstack",
	"registry.terraform.io/opsgenie/opsgenie":                      "opsgenie",
	"registry.opentofu.org/opsgenie/opsgenie":                      "opsgenie",
	"registry.terraform.io/pagerduty/pagerduty":                    "pagerduty",
	"registry.opentofu.org/pagerduty/pagerduty":                    "pagerduty",
	"registry.terraform.io/cyrilgdn/postgresql":                    "postgresql",
	"registry.opentofu.org/cyrilgdn/postgresql":                    "postgresql",
	"registry.terraform.io/cyrilgdn/rabbitmq":                      "rabbitmq",
	"registry.opentofu.org/cyrilgdn/rabbitmq":                      "rabbitmq",
	"registry.terraform.io/rancher/rancher2":                       "rancher2",
	"registry.opentofu.org/rancher/rancher2":                       "rancher2",
	"registry.terraform.io/ciscodevnet/sdwan":                      "sdwan",
	"registry.opentofu.org/ciscodevnet/sdwan":                      "sdwan",
	"registry.terraform.io/splunk-terraform/signalfx":              "signalfx",
	"registry.opentofu.org/splunk-terraform/signalfx":              "signalfx",
	"registry.terraform.io/jmatsu/slack":                           "slack",
	"registry.opentofu.org/jmatsu/slack":                           "slack",
	"registry.terraform.io/snowflake-labs/snowflake":               "snowflake",
	"registry.opentofu.org/snowflake-labs/snowflake":               "snowflake",
	"registry.terraform.io/splunk/splunk":                          "splunk",
	"registry.opentofu.org/splunk/splunk":                          "splunk",
	"registry.terraform.io/spotinst/spotinst":                      "spotinst",
	"registry.opentofu.org/spotinst/spotinst":                      "spotinst",
	"registry.terraform.io/tailscale/tailscale":                    "tailscale",
	"registry.opentofu.org/tailscale/tailscale":                    "tailscale",
	"registry.terraform.io/venafi/venafi":                          "venafi",
	"registry.opentofu.org/venafi/venafi":                          "venafi",
	"registry.terraform.io/vmware/wavefront":                       "wavefront",
	"registry.opentofu.org/vmware/wavefront":                       "wavefront",
}

func RecommendPulumiProvider(tf TerraformProvider) RecommendedPulumiProvider {
	// Check if there's a bridged provider for this Terraform provider
	if pulumiProvider, ok := providerMapping[tf.Identifier]; ok {
		return RecommendedPulumiProvider{
			BridgedPulumiProvider: &BridgedPulumiProvider{
				Identifier: pulumiProvider,
				Version:    "", // Version can be looked up separately if needed
			},
		}
	}

	// Default to using terraform-provider package if no bridged provider exists
	return RecommendedPulumiProvider{UseTerraformProviderPackage: true}
}
