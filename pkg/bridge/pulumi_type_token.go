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
