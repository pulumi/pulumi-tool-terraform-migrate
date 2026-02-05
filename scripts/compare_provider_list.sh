#!/usr/bin/env bash

set -euo pipefail

go build -o ./pulumi-tool-terraform-migrate .
./pulumi-tool-terraform-migrate compare-providers pkg/providermap/versions.yaml "$@"
