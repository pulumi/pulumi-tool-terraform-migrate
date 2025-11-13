# Migrating from Terraform to Pulumi

Given a Terraform project with sources and several workspaces, migration produces:

- a Pulumi project with sources describing the same infrastructure
- for every Terraform workspace, a matching Pulumi stack with Pulumi state tracking the same resources

Follow these steps to help the user migrate:

- create a `migration.json` file if one does not exist
  - use `pulumi-terraform migrate init-migration ...`
- translate Terraform source code to the chosen language and add files to the Pulumi project
- create Pulumi stacks mentioned in `migration.json`
- for each Pulumi stack, import as many resources as possible
  - iterate on fixing the sources until `pulumi preview` without issues
  - `pulumi preview --import-file import-stub.json`
  - `pulumi-terraform-migrate resolve-import-stubs ...`
  - `pulumi import --generate-code false --file import.json`
- for resources that fail to import, attempt to translate their state directly
  - `pulumi-terraform-migrate translate-state ...`
  - patch the stack's state with `pulumi stack export --file state.json`,
    edit `stack.json` and `pulumi stack import --file stack.json`
- iterate on fixing issues until the migration is successful
  - all Terraform resources should either map to Pulumi resources or be skipped
  - each stack should have a no-changes `pulumi preview`
  - each stack should have a no-changes `pulumi refresh`

## Formats

### migration.json file

The `migration.json` file helps the tooling correlate Terraform state files to Pulumi stacks, and correlate resource
states where a 1-1 correspondence is possible to establish directly. It looks like this:


```
{
  "migration": {
    "tf-sources": "/path/to/terraform-files",
    "pulumi-sources": "/path/to/pulumi-sources",
    "stacks": [
      {
        "tf-state": "/path/to/tfstate.json",
        "pulumi-stack": "dev",
        "resources": [
          {
            "tf-addr": "module.acm.aws_acm_certificate.this[0]",
            "urn": "urn:pulumi:dev::my-project::aws:acm/certificate:Certificate::cert"
          },
          {
            "tf-addr": "module.acm.aws_acm_certificate.this[1]",
            "migrate": "skip"
          }
        ]
      }
    ]
  }
}
```

### import.json file

List of Pulumi resources to import. Matches the following JSON format:

```
    {
        "resources": [
            {
                "type": "aws:ec2/vpc:Vpc",
                "name": "application-vpc",
                "id": "vpc-0ad77710973388316"
            },
            ...
            {
                ...
            }
        ],
    }
```

The full import file schema references can be found in the [import documentation]
(https://www.pulumi.com/docs/iac/adopting-pulumi/import/#bulk-import-operations).

### tfstate.json file

Terraform state file - `migration.json` should contain references to these files.

### stack.json file

Pulumi stack state file obtained by `pulumi export --file stack.json`.

## Tools

### `pulumi-terraform-migrate init-migration --migration migration.json --tf-sources /path/to/tf --pulumi-sources /path/to/pulumi-dir`

Drafts a `migration.json` file with recommended default mappings.

### `pulumi-terraform-migrate suggest-provider`

Suggests a Pulumi resource provider as a mapping target for a given Terraform provider's resources and data sources,
such as:

```
$ pulumi-terraform-migrate suggest-provider registry.terraform.io/hashicorp/aws
aws@v7.11.0
```

### `pulumi-terraform-migrate suggest-resource`

Suggests a specific Pulumi resource as a mapping target for a given Terraform provider's resource, such as:

```
$ pulumi-terraform-migrate suggest-resource registry.terraform.io/hashicorp/aws aws_acm_certificate
aws:acm/certificate:Certificate
```

### `pulumi preview --import-file import-stub.json`

Generates a stub `import.json` for resources declared in code but missing a state record. In the stub file resource IDs
are not known and are left as placeholders.

### `pulumi-terraform-migrate resolve-import-stubs --migration migration.json --stack dev --stubs import-stub.json --out import.json`

Attempts to resolve import IDs where possible to prepare for the import operation. Not all resources can be imported.

### `pulumi-terraform-migrate translate-state --migration migration.json --stack dev --tf-addr "module.acm.aws_acm_certificate.this[0]"`

Attempts to perform direct automated translation of a Terraform resoruce state to the corresponding Pulumi resource
state. This is helpful

### `pulumi import --generate-code false --file import.json`

Imports resources, crucially adding them to the state of the Pulumi stack.

### `pulumi stack export --file state.json`

Exports the current stack state to a file.

### `pulumi stack import --file state.json`

Imports a file to reset the current Pulumi stack state to the one specified in the file.
