# DNS-to-DB Manual Test Results

**Date:** 2026-04-03
**Branch:** `feat/mc-22-discovery-acceptance` (top of stack: mc-20 → mc-21-remaining-eval-gaps → mc-22-discovery-acceptance)
**Commit:** e3f3b92

## Command

```bash
go run . stack \
  --from pkg/testdata/tf_dns_to_db \
  --state-file pkg/testdata/tofu_state_dns_to_db.json \
  --pulumi-stack dev \
  --pulumi-project dns-to-db \
  --to /tmp/dns-to-db-results \
  --out /tmp/dns-to-db-results/pulumi-state.json \
  --plugins /tmp/dns-to-db-results/required-plugins.json
```

## Output Files

| File | Size | Description |
|------|------|-------------|
| `pulumi-state.json` | 323 KB | Translated Pulumi stack state (111 resources) |
| `component-schemas.json` | 457 KB | Component interface metadata (15 components) |
| `required-plugins.json` | 70 B | Required Pulumi plugins (aws@7.23.0, null@0.0.15) |
| `stderr.log` | 31 KB | Full stderr output |

## Results Summary

- **Exit code:** 0 (success)
- **Resources translated:** 111
- **Components discovered:** 15
- **Required plugins:** aws@7.23.0, null@0.0.15

### Diagnostics

| Category | Count | Description |
|----------|-------|-------------|
| Warnings | 2 | Both are `templatefile("app3-ums-install.tmpl")` — fixture file missing from testdata (not a code bug) |
| Notes | 244 | Informational: missing resources defaulted to null (count=0 conditional resources) |
| Debug | 14 | HCL expression panics caught by panic handler (upstream HCL library limitations) |

### Warning Progression

| Milestone | Warning Count |
|-----------|--------------|
| Before eval context work (mc-12) | 68 |
| After nested var scope (mc-20) | ~12 |
| After remaining eval gaps (mc-21) | **2** |

The 2 remaining warnings are both `templatefile("app3-ums-install.tmpl")` which references a user template file not included in the test fixture. These cannot be eliminated without adding the template file to testdata.
