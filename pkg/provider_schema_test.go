package pkg

import (
	"testing"

	bridgeConfigSchema "github.com/pulumi/pulumi-terraform-bridge/v3/pkg/vendored/opentofu/configs/configschema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zclconf/go-cty/cty"
)

func TestFindSensitiveAttributes(t *testing.T) {
	t.Parallel()

	t.Run("no sensitive attributes", func(t *testing.T) {
		block := &bridgeConfigSchema.Block{
			Attributes: map[string]*bridgeConfigSchema.Attribute{
				"name":   {Type: cty.String},
				"bucket": {Type: cty.String},
			},
		}
		result := findSensitiveAttributes(block, "")
		assert.Empty(t, result)
	})

	t.Run("top-level sensitive", func(t *testing.T) {
		block := &bridgeConfigSchema.Block{
			Attributes: map[string]*bridgeConfigSchema.Attribute{
				"name":          {Type: cty.String},
				"secret_string": {Type: cty.String, Sensitive: true},
				"password":      {Type: cty.String, Sensitive: true},
			},
		}
		result := findSensitiveAttributes(block, "")
		assert.True(t, result["secret_string"])
		assert.True(t, result["password"])
		assert.False(t, result["name"])
		assert.Len(t, result, 2)
	})

	t.Run("nested sensitive", func(t *testing.T) {
		block := &bridgeConfigSchema.Block{
			Attributes: map[string]*bridgeConfigSchema.Attribute{
				"name": {Type: cty.String},
			},
			BlockTypes: map[string]*bridgeConfigSchema.NestedBlock{
				"connection": {
					Block: bridgeConfigSchema.Block{
						Attributes: map[string]*bridgeConfigSchema.Attribute{
							"host":     {Type: cty.String},
							"password": {Type: cty.String, Sensitive: true},
						},
					},
				},
			},
		}
		result := findSensitiveAttributes(block, "")
		assert.True(t, result["connection.password"])
		assert.False(t, result["name"])
		assert.False(t, result["connection.host"])
		assert.Len(t, result, 1)
	})

	t.Run("nil block", func(t *testing.T) {
		result := findSensitiveAttributes(nil, "")
		assert.Empty(t, result)
	})
}

func TestRedactSensitiveAttributes(t *testing.T) {
	t.Parallel()

	t.Run("redacts sensitive fields", func(t *testing.T) {
		attrs := map[string]interface{}{
			"name":          "my-secret",
			"secret_string": "super-secret-value",
			"arn":           "arn:aws:secretsmanager:us-east-1:123:secret:foo",
		}
		sensitive := map[string]bool{
			"secret_string": true,
		}

		result := RedactSensitiveAttributes(attrs, sensitive)

		assert.Equal(t, "my-secret", result["name"])
		assert.Equal(t, "(sensitive)", result["secret_string"])
		assert.Equal(t, "arn:aws:secretsmanager:us-east-1:123:secret:foo", result["arn"])
	})

	t.Run("no sensitive fields", func(t *testing.T) {
		attrs := map[string]interface{}{
			"name":   "my-bucket",
			"bucket": "my-bucket",
		}

		result := RedactSensitiveAttributes(attrs, nil)
		assert.Equal(t, attrs, result)
	})

	t.Run("empty attrs", func(t *testing.T) {
		result := RedactSensitiveAttributes(map[string]interface{}{}, map[string]bool{"foo": true})
		require.Empty(t, result)
	})

	t.Run("does not modify original", func(t *testing.T) {
		attrs := map[string]interface{}{
			"password": "secret123",
		}
		sensitive := map[string]bool{
			"password": true,
		}

		result := RedactSensitiveAttributes(attrs, sensitive)
		assert.Equal(t, "(sensitive)", result["password"])
		assert.Equal(t, "secret123", attrs["password"]) // original unchanged
	})
}
