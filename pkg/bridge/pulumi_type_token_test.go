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
	"testing"

	"github.com/pulumi/pulumi-terraform-bridge/v3/pkg/tfbridge/info"
	"github.com/pulumi/pulumi/sdk/v3/go/common/tokens"
	"github.com/stretchr/testify/require"
)

func TestPulumiTypeToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		tfTypeName     string
		pulumiProvider *info.Provider
		expectedToken  tokens.Type
	}{
		{
			name:       "explicit token",
			tfTypeName: "aws_apigatewayv2_api",
			pulumiProvider: &info.Provider{
				Name: "aws",
				Resources: map[string]*info.Resource{
					"aws_apigatewayv2_api": {
						Tok: "aws:apigatewayApi:ApigatewayApi",
					},
				},
			},
			expectedToken: tokens.Type("aws:apigatewayApi:ApigatewayApi"),
		},
		{
			name:       "implicit token",
			tfTypeName: "aws_apigatewayv2_api",
			pulumiProvider: &info.Provider{
				Name: "aws",
				Resources: map[string]*info.Resource{
					"aws_apigatewayv2_api": {},
				},
			},
			expectedToken: tokens.Type("aws:apigatewayv2Api:Apigatewayv2Api"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			token, err := PulumiTypeToken(test.tfTypeName, test.pulumiProvider)
			require.NoError(t, err)
			require.Equal(t, test.expectedToken, token)
		})
	}
}
