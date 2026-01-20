#!/usr/bin/env bash

set -euo pipefail

export PULUMI_ADMIN_COMMANDS=true

go build -o ./pulumi-tool-terraform-migrate .
./pulumi-tool-terraform-migrate update-providermap pkg/providermap/versions.yaml
