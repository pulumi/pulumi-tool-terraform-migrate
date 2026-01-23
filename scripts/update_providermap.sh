#!/usr/bin/env bash

set -euo pipefail

go build -o ./pulumi-tool-terraform-migrate .
./pulumi-tool-terraform-migrate update-providermap pkg/providermap/versions.yaml "$@"
