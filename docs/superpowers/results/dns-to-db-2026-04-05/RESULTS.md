# DNS-to-DB Manual Test Results

**Date:** 2026-04-06 (updated from mc-24)
**Branch:** `feat/mc-24-templatefile-basedir` (top of stack)
**Commit:** fb65bde

## Command

```bash
go run . stack \
  --from pkg/testdata/tf_dns_to_db \
  --state-file pkg/testdata/tofu_state_dns_to_db.json \
  --pulumi-stack dev \
  --pulumi-project dns-to-db \
  --to /tmp/dns-to-db-results \
  --out /tmp/dns-to-db-results/pulumi-state.json
```

## Output Files

| File | Size | Description |
|------|------|-------------|
| `pulumi-state.json` | 218 KB | Translated Pulumi stack state (111 resources) |
| `component-schemas.json` | 457 KB | Component interface metadata (15 components) |
| `required-plugins.json` | 70 B | Required Pulumi plugins (aws@7.23.0, null@0.0.15) |
| `stderr.log` | 28 KB | Full stderr output |

## Results Summary

- **Exit code:** 0 (success)
- **Resources translated:** 111 (1 stack + 2 providers + 18 components + 90 custom)
- **Components discovered:** 18 (15 top-level + 3 nested under rdsdb)
- **Required plugins:** aws@7.23.0, null@0.0.15

### Diagnostics

| Category | Count | Description |
|----------|-------|-------------|
| Warnings | 0 | None — all expressions evaluate successfully |
| Notes | 244 | Informational: missing resources defaulted to null (count=0 conditional resources) |

### Warning Progression

| Milestone | Warning Count |
|-----------|--------------|
| Before eval context work (mc-12) | 68 |
| After nested var scope (mc-20) | ~12 |
| After remaining eval gaps (mc-21) | 2 |
| After null-defaults removal (mc-23) | 2 (unchanged) |
| After templatefile basedir fix (mc-24) | **0** |

## mc-23: Variable Default Removal from State

The key change in mc-23 is removing the merge of module variable defaults into component state inputs. Defaults now live only in `component-schemas.json` (as variable declarations) and in the output eval context (so output expressions can still resolve `var.*` refs).

### Component Input Counts (Before vs After)

| Component | Before (mc-22) | After (mc-23) | Reduction |
|-----------|----------------|---------------|-----------|
| vpc | 222 inputs (31 null) | 17 inputs (0 null) | -92% |
| rdsdb | 90 inputs (31 null) | 30 inputs (0 null) | -67% |
| ec2_* (each) | ~80 inputs (51 null) | 7-8 inputs (0 null) | -90% |
| acm | 20 inputs (14 null) | 6 inputs (0 null) | -70% |
| alb | ~40 inputs | 9 inputs (1 null) | -78% |
| SG modules (each) | ~30 inputs | 6-8 inputs (1 null) | -77% |

### Totals

| Metric | Before (mc-22) | After (mc-23) |
|--------|----------------|---------------|
| Total component inputs | ~1,200+ | 166 |
| Null inputs | ~200+ | 5 |
| State file size | 323 KB | 217 KB |

### Current Component Inputs (call-site only)

| Component | Inputs | Null |
|-----------|--------|------|
| acm | 6 | 0 |
| alb | 9 | 1 |
| ec2_private_app1 (x2) | 8 | 0 |
| ec2_private_app2 (x2) | 8 | 0 |
| ec2_private_app3 (x2) | 7 | 0 |
| ec2_public | 8 | 0 |
| loadbalancer_sg | 8 | 1 |
| private_sg | 7 | 1 |
| public_bastion_sg | 7 | 1 |
| rdsdb | 30 | 0 |
| db_option_group | 9 | 0 |
| db_parameter_group | 7 | 0 |
| db_subnet_group | 6 | 0 |
| rdsdb_sg | 6 | 1 |
| vpc | 17 | 0 |

The 5 remaining null inputs are explicitly passed as `null` at call sites (SG modules ingress/egress rules), not from variable defaults.
