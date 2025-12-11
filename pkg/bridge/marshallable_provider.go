package bridge

import (
	"github.com/pulumi/pulumi-terraform-bridge/v3/pkg/tfbridge/info"
	shim "github.com/pulumi/pulumi-terraform-bridge/v3/pkg/tfshim"
	"github.com/pulumi/pulumi-terraform-bridge/v3/pkg/tfshim/schema"
	"github.com/pulumi/pulumi-terraform-bridge/v3/pkg/valueshim"
)

// MarshallableProvider is a JSON-marshallable form of a Pulumi provider.
// This contrasts with the type in tfbridge/info, with the fact that the Resource also contains a SchemaType field.
// TODO: We might be able to get rid of this and call the bridge [info.MarshallableProvider] instead, using [ImpliedType]
// directly for producing the [cty.Type] instead of relying on the [SchemaType] method.
type MarshallableProvider struct {
	Provider          *MarshallableProviderShimWithTypes      `json:"provider"`
	Name              string                                  `json:"name"`
	Version           string                                  `json:"version"`
	Config            map[string]*info.MarshallableSchema     `json:"config,omitempty"`
	Resources         map[string]*info.MarshallableResource   `json:"resources,omitempty"`
	DataSources       map[string]*info.MarshallableDataSource `json:"dataSources,omitempty"`
	TFProviderVersion string                                  `json:"tfProviderVersion,omitempty"`
}

func (m *MarshallableProvider) Unmarshal() *info.Provider {
	config := make(map[string]*info.Schema)
	for k, v := range m.Config {
		config[k] = v.Unmarshal()
	}
	resources := make(map[string]*info.Resource)
	for k, v := range m.Resources {
		resources[k] = v.Unmarshal()
	}
	dataSources := make(map[string]*info.DataSource)
	for k, v := range m.DataSources {
		dataSources[k] = v.Unmarshal()
	}

	info := info.Provider{
		P:                 m.Provider.Unmarshal(),
		Name:              m.Name,
		Version:           m.Version,
		Config:            config,
		Resources:         resources,
		DataSources:       dataSources,
		TFProviderVersion: m.TFProviderVersion,
	}

	return &info
}

// MarshallableProviderShim is the JSON-marshallable form of a Terraform provider schema.
// This contrasts with the type in tfbridge/info, with the fact that the Resource also contains a SchemaType field.
type MarshallableProviderShimWithTypes struct {
	Schema      map[string]*info.MarshallableSchemaShim      `json:"schema,omitempty"`
	Resources   map[string]MarshallableResourceShimWithTypes `json:"resources,omitempty"`
	DataSources map[string]MarshallableResourceShimWithTypes `json:"dataSources,omitempty"`
}

// Unmarshal creates a mostly-initialized Terraform provider schema from a MarshallableProvider
func (m *MarshallableProviderShimWithTypes) Unmarshal() shim.Provider {
	if m == nil {
		return nil
	}

	config := schema.SchemaMap{}
	for k, v := range m.Schema {
		config[k] = v.Unmarshal()
	}
	resources := schema.ResourceMap{}
	for k, v := range m.Resources {
		resources[k] = v.Unmarshal()
	}
	dataSources := schema.ResourceMap{}
	for k, v := range m.DataSources {
		dataSources[k] = v.Unmarshal()
	}
	return (&schema.Provider{
		Schema:         config,
		ResourcesMap:   resources,
		DataSourcesMap: dataSources,
	}).Shim()
}

// MarshallableResourceShimWithTypes is a JSON-marshallable form of a Terraform resource schema.
// This contrasts with the type in tfbridge/info, with the fact that the Resource also contains a SchemaType field.
type MarshallableResourceShimWithTypes map[string]*info.MarshallableSchemaShim

// Unmarshal creates a mostly-initialized Terraform resource schema from the given MarshallableResourceShim.
func (m MarshallableResourceShimWithTypes) Unmarshal() shim.Resource {
	s := schema.SchemaMap{}
	for k, v := range m {
		s[k] = v.Unmarshal()
	}
	return (&schema.Resource{
		Schema:     s,
		SchemaType: valueshim.FromCtyType(ImpliedType(s, true)),
	}).Shim()
}
