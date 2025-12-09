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
