# AWS Import Diff Fields Reference

Background documentation for `aws-import-diff-fields.json`. The JSON file contains only the fields consumed by `patch-state` (`default`, `asset`, `assetKind`, `archiveFormat`, `hashField`). This document describes the categories, root causes, and verification guidance for each field.

## References

- Bridge default diff: [pulumi-terraform-bridge#2436](https://github.com/pulumi/pulumi-terraform-bridge/issues/2436)
- Bridge TypeSet ordering: [pulumi-terraform-bridge#3324](https://github.com/pulumi/pulumi-terraform-bridge/issues/3324)
- Display bug (null rendering): [pulumi/pulumi#23067](https://github.com/pulumi/pulumi/issues/23067)
- Falsy default suppression: [pulumi-terraform-bridge#3398](https://github.com/pulumi/pulumi-terraform-bridge/issues/3398) (bridge >= v3.127.0, AWS provider >= v7.27.0)

## Categories

### not_read

Read doesn't populate this field. Import has null. Diff is null -> value.

**Root causes:**

| Root Cause | Description |
|------------|-------------|
| `bridge_default` | Field has a TF SDK Default. Bridge applies it during PlanResourceChange on null import state. Program doesn't set it. |
| `aws_api_limitation` | AWS API does not return this data. Program must provide the value explicitly. |
| `provider_design` | AWS API returns this data but provider Read doesn't populate it in state. Program must provide the value explicitly. |

**Field metadata (for human review, not consumed by patch-state):**

- `read_from_aws` — Whether any AWS API returns this value (false for all not_read fields)
- `sent_to_aws_on_create` — Whether provider sends this to an AWS API during Create
- `sent_to_aws_on_update` — Whether provider sends this to an AWS API during Update (true, false, or conditional)
- `sent_to_aws_on_delete` — Whether provider sends this to an AWS API during Delete
- `provider_side_effect` — Provider behavior that doesn't involve an AWS API call (e.g. polling, waiting)
- `triggers_api` — If the field conditionally triggers a separate AWS API call, which API and under what conditions

**Verification:**

- `check_tf_code` — Verify the default/value matches TF code
- `check_tf_state` — Verify against tf-digest (TF state persists this value from initial create)
- `check_aws` — Verify against deployed AWS resource via CLI
- `aws_cli` — AWS CLI command to check the deployed value

### read_filtered

Read populates but filters results. Import has partial data.

Root cause: `provider_design`. Example: RDS ClusterParameterGroup `parameters` — Read filters by `Source=parameterSourceUser`, discarding system defaults.

### provider_normalized

Read populates but normalizes format. Import has different structure, same semantics.

Root cause: `provider_design`. Example: LB Listener `defaultActions` — AWS normalizes `targetGroupArn` into forward block with `stickiness.duration=0`.

### typeset_ordering

Read populates but bridge compares positionally. Import has different order, same values.

Root cause: bridge bug ([#3324](https://github.com/pulumi/pulumi-terraform-bridge/issues/3324)). Resolves after first `pulumi up`. Only recurs on `pulumi refresh`.

Examples: SecurityGroup `ingress`/`egress`, WAF WebAcl `rules`.

### computed_cascade

Output changes because an upstream field changed. Not directly settable.

Root cause: consequence. Examples: Lambda `qualifiedArn`/`version`, S3 `versionId`, ECS Service `taskDefinition`.

### default_tags_migration

TF `default_tags` writes to explicit `tags`; Pulumi `defaultTags` writes to `tagsAll`. No tag lost. Applies to ANY resource type — detected by field name (`tags`/`tagsAll`), not per-type.

## Per-Resource Field Details

### aws:ecs/service:Service

| Field | Category | Default | Sent to AWS | Notes |
|-------|----------|---------|-------------|-------|
| `waitForSteadyState` | not_read (bridge_default) | `false` | never | Provider polls ECS until service reaches steady state. No AWS API call. |
| `taskDefinition` | computed_cascade | — | — | Includes revision number which changes on any task def update |

### aws:ec2/securityGroup:SecurityGroup

| Field | Category | Default | Sent to AWS | Notes |
|-------|----------|---------|-------------|-------|
| `revokeRulesOnDelete` | not_read (bridge_default) | `false` | delete only | Revokes all ingress/egress rules before deleting the SG |
| `ingress` | typeset_ordering | — | — | TypeSet positional comparison |
| `egress` | typeset_ordering | — | — | TypeSet positional comparison |

### aws:s3/bucketObject:BucketObject

| Field | Category | Default | Sent to AWS | Notes |
|-------|----------|---------|-------------|-------|
| `forceDestroy` | not_read (bridge_default) | `false` | delete only | Forces object version deletion |
| `content` | not_read (provider_design) | null | create/update | HeadObject (Read) doesn't return body. PutObject on update. |
| `source` | not_read (provider_design) | null (FileAsset) | create/update | Local file path, write-only. Verify: `s3 cp s3://BUCKET/KEY -` |
| `overrideProvider` | not_read (provider_design) | null | never | TF-only config controlling tag behavior |

### aws:s3/bucket:Bucket

| Field | Category | Default | Sent to AWS | Notes |
|-------|----------|---------|-------------|-------|
| `forceDestroy` | not_read (bridge_default) | `false` | delete only | Empties bucket before deletion |

### aws:secretsmanager/secretVersion:SecretVersion

| Field | Category | Default | Sent to AWS | Notes |
|-------|----------|---------|-------------|-------|
| `secretString` | not_read (provider_design) | null | create/update | GetSecretValue API returns value but Read doesn't populate. Verify: `secretsmanager get-secret-value --secret-id ID` |

### aws:secretsmanager/secret:Secret

| Field | Category | Default | Sent to AWS | Notes |
|-------|----------|---------|-------------|-------|
| `forceOverwriteReplicaSecret` | not_read (bridge_default) | `false` | conditional | Only sent when replica regions are being added |
| `recoveryWindowInDays` | not_read (bridge_default) | `30` | delete only | Sent to DeleteSecret API. Value persists in TF state from initial create. |

### aws:rds/cluster:Cluster

| Field | Category | Default | Sent to AWS | Notes |
|-------|----------|---------|-------------|-------|
| `applyImmediately` | not_read (bridge_default) | `false` | update | Sent to ModifyDBCluster on every update. false = deferred to maintenance window. |
| `allowMajorVersionUpgrade` | not_read (aws_api_limitation) | null | update | Write-only modifier. Not returned by DescribeDBClusters. |
| `enableGlobalWriteForwarding` | not_read (bridge_default) | `false` | create/update | Sent to ModifyDBCluster |
| `enableLocalWriteForwarding` | not_read (bridge_default) | `false` | create/update | Sent to ModifyDBCluster |
| `masterPassword` | not_read (aws_api_limitation) | null | create/update | Never returned by any AWS API. Verify via tf-digest config secret. |
| `restoreToPointInTime` | not_read (aws_api_limitation) | null | create only | Create-only input block for PITR restore. ForceNew. |
| `s3Import` | not_read (aws_api_limitation) | null | create only | Create-only input block for S3 data import. |

### aws:rds/clusterInstance:ClusterInstance

| Field | Category | Default | Sent to AWS | Notes |
|-------|----------|---------|-------------|-------|
| `applyImmediately` | not_read (bridge_default) | `false` | update | Sent to ModifyDBInstance |
| `forceDestroy` | not_read (bridge_default) | `false` | delete only | Forces instance deletion |

### aws:rds/clusterParameterGroup:ClusterParameterGroup

| Field | Category | Default | Sent to AWS | Notes |
|-------|----------|---------|-------------|-------|
| `parameters` | not_read (provider_design) | null | — | Read filters by Source=parameterSourceUser. Digest has TF state with snake_case keys. |

### aws:cloudfront/distribution:Distribution

| Field | Category | Default | Sent to AWS | Notes |
|-------|----------|---------|-------------|-------|
| `retainOnDelete` | not_read (bridge_default) | `false` | delete only | Disables distribution instead of deleting. Importer sets false. |
| `isIpv6Enabled` | not_read (bridge_default) | `false` | create/update | Read populates from API, but bridge applies Default:false when program omits. |
| `staging` | not_read (bridge_default) | `false` | create only | ForceNew. Read populates from API. |
| `defaultCacheBehavior` | computed_cascade | — | — | lambdaFunctionAssociations[0].lambdaArn cascades from Lambda qualifiedArn |

### aws:acm/certificate:Certificate

| Field | Category | Default | Sent to AWS | Notes |
|-------|----------|---------|-------------|-------|
| `certificateBody` | not_read (provider_design) | null | create/update | GetCertificate returns cert but Read doesn't fetch. Verify: `acm get-certificate --certificate-arn ARN` |
| `certificateChain` | not_read (provider_design) | null | create/update | Same as certificateBody |
| `privateKey` | not_read (aws_api_limitation) | null | create/update | Never returned by any AWS API |

### aws:sns/topicSubscription:TopicSubscription

| Field | Category | Default | Sent to AWS | Notes |
|-------|----------|---------|-------------|-------|
| `confirmationTimeoutInMinutes` | not_read (bridge_default) | `1` | never | Provider-side timeout only |
| `endpointAutoConfirms` | not_read (bridge_default) | `false` | never | Provider-side behavior only |

### aws:lambda/function:Function

| Field | Category | Default | Sent to AWS | Notes |
|-------|----------|---------|-------------|-------|
| `publish` | not_read (bridge_default) | `false` | conditional | PublishVersion called when true AND code/config changes |
| `skipDestroy` | not_read (bridge_default) | `false` | delete only | Skips DeleteFunction. Read sets from prior state, not AWS. |
| `sourceCodeHash` | not_read (provider_design) | null | never | Read sets from prior state. Used for change detection (DiffSuppressFunc). |
| `code` | not_read (aws_api_limitation) | null (FileArchive) | create/update | GetFunction returns presigned URL, not content. Verify: download from Code.Location URL |
| `qualifiedArn` | computed_cascade | — | — | Changes when new version published |
| `qualifiedInvokeArn` | computed_cascade | — | — | Changes when new version published |
| `lastModified` | computed_cascade | — | — | Timestamp updated on any change |
| `version` | computed_cascade | — | — | Increments on new version |

### aws:lb/listener:Listener

| Field | Category | Default | Sent to AWS | Notes |
|-------|----------|---------|-------------|-------|
| `defaultActions` | provider_normalized | — | — | AWS normalizes targetGroupArn into forward block with stickiness.duration=0 |

### aws:ecs/taskDefinition:TaskDefinition

| Field | Category | Default | Sent to AWS | Notes |
|-------|----------|---------|-------------|-------|
| `skipDestroy` | not_read (bridge_default) | `false` | delete only | Skips DeregisterTaskDefinition |

### aws:wafv2/webAcl:WebAcl

| Field | Category | Default | Sent to AWS | Notes |
|-------|----------|---------|-------------|-------|
| `rules` | typeset_ordering | — | — | Deeply nested blocks fail element matching |
