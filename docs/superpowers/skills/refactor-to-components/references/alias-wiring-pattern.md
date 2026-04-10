# Alias Wiring Pattern

Migration aliases connect old flat URNs (from Terraform import) to new child resource names inside components. This is done externally via transforms so component classes stay clean.

## migration-aliases.json

Keys are NEW child resource names (as created inside the component). Values are OLD flat URNs from imported state.

```json
{
  "vpc-main-vpc": "urn:pulumi:dev::project::aws:ec2/vpc:Vpc::vpc_main",
  "vpc-subnet-0": "urn:pulumi:dev::project::aws:ec2/subnet:Subnet::vpc_subnets_0"
}
```

## TypeScript

```typescript
import * as pulumi from "@pulumi/pulumi";
import aliasMap from "./migration-aliases.json";
import { Vpc } from "./components/vpc";

const migrationTransform = (args: pulumi.ResourceTransformationArgs): pulumi.ResourceTransformationResult | undefined => {
  const oldUrn = (aliasMap as Record<string, string>)[args.name];
  if (oldUrn) {
    const existing = (args.opts.aliases as pulumi.Input<string | pulumi.Alias>[] | undefined) || [];
    return {
      props: args.props,
      opts: { ...args.opts, aliases: [...existing, oldUrn] },
    };
  }
  return undefined;
};

const vpc = new Vpc("vpc", { ...inputs }, {
  transformations: [migrationTransform],
});
```

**Important**: Aliases must be plain URN strings (e.g., `oldUrn`), not objects like `{ urn: oldUrn }`. The Pulumi `Alias` interface does not have a `urn` field.

Ensure `tsconfig.json` has `"resolveJsonModule": true` and `"esModuleInterop": true`.

## Python

```python
import json
import pulumi
from components.vpc import Vpc

with open("migration-aliases.json") as f:
    alias_map = json.load(f)

def migration_transform(args: pulumi.ResourceTransformationArgs):
    old_urn = alias_map.get(args.name)
    if old_urn:
        existing = args.opts.aliases or []
        return pulumi.ResourceTransformationResult(
            props=args.props,
            opts=dataclasses.replace(args.opts, aliases=[*existing, old_urn]),
        )
    return None

vpc = Vpc("vpc", inputs, opts=pulumi.ResourceOptions(
    transformations=[migration_transform],
))
```

**Note**: In Python, aliases are also plain URN strings.

## Post-migration cleanup

1. Run `pulumi up` with transforms in place — state adopts new component URNs
2. Delete `migration-aliases.json`
3. Remove `transformations` from all component instantiations
4. Run `pulumi preview` — must show zero changes
