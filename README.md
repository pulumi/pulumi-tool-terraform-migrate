# pulumi-terraform-migrate

This is an EXPERIMENTAL tool for assisting migrating Terraform projects to Pulumi.

For robust approaches to migration please see the [official documentation](https://www.pulumi.com/docs/iac/guides/migration/migrating-to-pulumi/from-terraform/).

## Usage

This tool is useful in pipelines that given a Terraform project with sources and state aim to produce equivalent Pulumi
sources and state tracking the same infrastructure. Crucially the pipeline should not do any write operations on the actual
infrastructure, staying in the purely symbolic exploratory world.

The key command is translate:

```
pulumi-terraform-migrate translate \
  --input-path terraform.tfstate \
  --output-file pulumi.json \
  --stack-folder path/to/pulumi/stack
  --required-providers-file required-providers.json
```

This produces a draft [Pulumi stack state](https://www.pulumi.com/docs/iac/cli/commands/pulumi_state/) that represents
a translated input Terraform state. Additionally it produces a map of recommended Pulumi provider names and versions to
use in the translation.

To proceed with the migration, import the state into your Pulumi stack, feed these artifacts into an LLM, and ask it to
produce Pulumi sources that translate the Terraform sources. Instructing the LLM to aim for a clean `pulumi preview`
helps is to fix discrepancies between code and state and get accurate results.
