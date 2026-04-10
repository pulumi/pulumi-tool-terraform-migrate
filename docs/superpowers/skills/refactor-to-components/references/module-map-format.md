# module-map.json Format

Schema reference for the output of `pulumi-terraform-migrate module-map`.

## Top-level structure

```json
{
  "modules": {
    "<module-key>": { ... }
  }
}
```

`<module-key>` is a unique identifier for each module instance (e.g., `module.vpc`, `module.subnet[0]`).

## Module object fields

| Field | Type | Description |
|-------|------|-------------|
| `terraformPath` | `string` | Full Terraform address (e.g., `module.vpc`, `module.subnet["us-east-1"]`) |
| `source` | `string` | Module source path or registry address |
| `indexKey` | `string \| null` | Instance key when module uses `count` or `for_each`. Omitted for non-indexed modules. |
| `indexType` | `"count" \| "for_each" \| "none"` | How the module is instantiated |
| `resources` | `ModuleResource[]` | Resources belonging to this module instance (see below) |
| `interface` | `object` | Inputs and outputs for the module |
| `modules` | `object \| null` | Nested child modules (same structure as top-level `modules`). Omitted when empty. |

## ModuleResource object

| Field | Type | Description |
|-------|------|-------------|
| `translatedUrn` | `string` | Pulumi URN the resource would have in flat translated state |
| `terraformAddress` | `string` | Full Terraform resource address (e.g., `module.acm.aws_acm_certificate.this[0]`) |
| `importId` | `string` | Cloud provider resource ID for `pulumi import` |

## Interface object

```json
{
  "inputs": [ ... ],
  "outputs": [ ... ]
}
```

### Input fields

| Field | Type | Description |
|-------|------|-------------|
| `name` | `string` | Variable name as declared in the module |
| `type` | `string` | HCL type (`string`, `number`, `bool`, `list(string)`, `map(string)`, `object(...)`) |
| `required` | `bool` | `true` if no default is set |
| `default` | `any \| null` | Default value from variable declaration. `null` when no default. |
| `description` | `string` | From the variable's `description` attribute |
| `expression` | `string` | The HCL expression passed to this input in the calling module (e.g., `var.vpc_cidr`, `module.network.vpc_id`) |
| `evaluatedValue` | `any` | The resolved concrete value after evaluation |

### Output fields

| Field | Type | Description |
|-------|------|-------------|
| `name` | `string` | Output name as declared in the module |
| `description` | `string` | From the output's `description` attribute |

## Usage notes

- `expression` vs `evaluatedValue`: Use `expression` to determine if the value derives from a variable or another module output — prefer wiring references over hardcoding `evaluatedValue`.
- `indexType: "count"` modules have numeric `indexKey` values (`"0"`, `"1"`, ...). `for_each` modules have string keys.
- `resources[].translatedUrn` contains fully qualified Pulumi URNs matching entries in flat imported state.
- `resources[].importId` contains the cloud provider resource ID needed for `pulumi import`.
- Nested `modules` follow the same schema recursively. A module with children has its own resources AND child module resources.

## Example

```json
{
  "modules": {
    "module.vpc": {
      "terraformPath": "module.vpc",
      "source": "./modules/vpc",
      "indexKey": null,
      "indexType": "none",
      "resources": [
        {
          "translatedUrn": "urn:pulumi:dev::project::aws:ec2/vpc:Vpc::vpc_main",
          "terraformAddress": "module.vpc.aws_vpc.main",
          "importId": "vpc-0abc123def456"
        },
        {
          "translatedUrn": "urn:pulumi:dev::project::aws:ec2/subnet:Subnet::vpc_subnets_0",
          "terraformAddress": "module.vpc.aws_subnet.subnets[0]",
          "importId": "subnet-0abc123def456"
        }
      ],
      "interface": {
        "inputs": [
          {
            "name": "cidr_block",
            "type": "string",
            "required": true,
            "default": null,
            "description": "The CIDR block for the VPC",
            "expression": "var.vpc_cidr",
            "evaluatedValue": "10.0.0.0/16"
          }
        ],
        "outputs": [
          {
            "name": "vpc_id",
            "description": "The ID of the VPC"
          }
        ]
      },
      "modules": null
    }
  }
}
```
