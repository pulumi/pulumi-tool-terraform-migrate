# Migrating infrastructure from Terraform to Pulumi

If the user asks to migrate infra from Terraform to Pulumi, you must use the migration CLI tool
`pulumi-terraform-migrate`. Iterate by calling `pulumi-terraform-migrate next` and following the suggestions for the
next steps that the tool emits until the migration is complete. If the tool says STOP, terminate and wait for next
suggestions from the user.

When asked to translate source code:

- do not run tf2pulumi
- do not run `pulumi convert`
- instead translate the source code directly into the appropriate Pulumi language preserving as much structure as possible

Do not timeout `pulumi-terraform-migrate` as it can take a while. Allow 30 minutes.
