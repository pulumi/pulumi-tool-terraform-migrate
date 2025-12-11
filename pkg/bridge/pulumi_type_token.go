package bridge

import (
	"strings"
	"unicode"

	"github.com/pulumi/pulumi-terraform-bridge/v3/pkg/tfbridge"
	"github.com/pulumi/pulumi-terraform-bridge/v3/pkg/tfbridge/info"
	"github.com/pulumi/pulumi/sdk/v3/go/common/tokens"
	"github.com/pulumi/pulumi/sdk/v3/go/common/util/contract"
)

// copied from https://github.com/pulumi/pulumi-terraform-bridge/blob/main/pkg/tfbridge/provider.go#L425
func PulumiTypeToken(tfTypeName string, pulumiProvider *info.Provider) (tokens.Type, error) {
	resourceInfo := pulumiProvider.Resources[tfTypeName]
	if resourceInfo.Tok != "" {
		return resourceInfo.Tok, nil
	}
	camelName, pascalName := camelPascalPulumiName(tfTypeName, pulumiProvider)
	pkgName := tokens.NewPackageToken(tokens.PackageName(tokens.IntoQName(pulumiProvider.Name)))
	modTok := tokens.NewModuleToken(pkgName, tokens.ModuleName(camelName))
	return tokens.NewTypeToken(modTok, tokens.TypeName(pascalName)), nil
}

// copied from pulumi-terraform-bridge/pkg/tfbridge/provider.go
func camelPascalPulumiName(name string, prov *info.Provider) (string, string) {
	prefix := prov.GetResourcePrefix() + "_"
	contract.Assertf(strings.HasPrefix(name, prefix),
		"Expected all Terraform resources in this module to have a '%v' prefix (%q)", prefix, name)
	name = name[len(prefix):]
	camel := tfbridge.TerraformToPulumiNameV2(name, nil, nil)
	pascal := camel
	if pascal != "" {
		pascal = string(unicode.ToUpper(rune(pascal[0]))) + pascal[1:]
	}
	return camel, pascal
}
