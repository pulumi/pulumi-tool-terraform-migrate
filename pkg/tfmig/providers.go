package tfmig

// TerraformToPulumiProvider maps Terraform provider addresses to their
// corresponding Pulumi bridged provider packages.
var TerraformToPulumiProvider = map[string]string{
	// AWS
	"registry.terraform.io/hashicorp/aws":  "aws",
	"registry.opentofu.org/hashicorp/aws": "aws",

	// Azure
	"registry.terraform.io/hashicorp/azurerm": "azure",
	"registry.terraform.io/hashicorp/azuread": "azuread",

	// Google Cloud Platform
	"registry.terraform.io/hashicorp/google":      "gcp",
	"registry.terraform.io/hashicorp/google-beta": "gcp",

	// Oracle Cloud Infrastructure
	"registry.terraform.io/oracle/oci": "oci",

	// Database & Storage
	"registry.terraform.io/hashicorp/postgresql": "postgresql",
	"registry.terraform.io/hashicorp/mysql":      "mysql",

	// Monitoring & Observability
	"registry.terraform.io/hashicorp/datadog":  "datadog",
	"registry.terraform.io/grafana/grafana":    "grafana",
	"registry.terraform.io/hashicorp/newrelic": "newrelic",

	// DevOps & CI/CD
	"registry.terraform.io/integrations/github": "github",
	"registry.terraform.io/hashicorp/gitlab":    "gitlab",

	// Networking & CDN
	"registry.terraform.io/cloudflare/cloudflare": "cloudflare",

	// Security & Secrets Management
	"registry.terraform.io/hashicorp/vault":  "vault",
	"registry.terraform.io/hashicorp/consul": "consul",

	// Infrastructure & Virtualization
	"registry.terraform.io/hashicorp/vsphere":         "vsphere",
	"registry.terraform.io/digitalocean/digitalocean": "digitalocean",
	"registry.terraform.io/linode/linode":             "linode",

	// Container & Orchestration
	"registry.terraform.io/hashicorp/nomad": "nomad",

	// Random & Utility
	"registry.terraform.io/hashicorp/random": "random",
}

// GetPulumiProvider returns the Pulumi provider package for a given Terraform provider address.
// Returns empty string if no mapping exists.
func GetPulumiProvider(terraformProvider string) string {
	return TerraformToPulumiProvider[terraformProvider]
}

// IsMapped checks if a Terraform provider has a known Pulumi equivalent.
func IsMapped(terraformProvider string) bool {
	_, exists := TerraformToPulumiProvider[terraformProvider]
	return exists
}

// getProviderVersion returns a hardcoded version for a given Pulumi provider.
func GetProviderVersion(providerName string) string {
	versions := map[string]string{
		"aws":          "v7.11.0",
		"azure":        "v6.31.0",
		"azuread":      "v6.4.0",
		"gcp":          "v8.17.0",
		"oci":          "v3.9.0",
		"postgresql":   "v3.18.0",
		"mysql":        "v3.5.0",
		"datadog":      "v4.43.0",
		"grafana":      "v0.14.0",
		"newrelic":     "v5.54.0",
		"github":       "v6.5.0",
		"gitlab":       "v8.8.0",
		"cloudflare":   "v6.2.0",
		"vault":        "v6.5.0",
		"consul":       "v3.14.0",
		"vsphere":      "v4.15.0",
		"digitalocean": "v4.38.0",
		"linode":       "v4.37.0",
		"nomad":        "v2.4.0",
		"random":       "v4.18.4",
	}

	if version, ok := versions[providerName]; ok {
		return version
	}

	// Default to a reasonable version if not in the map
	return "v1.0.0"
}
