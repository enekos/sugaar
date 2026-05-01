#!/bin/bash
set -euo pipefail
cd "$(dirname "$0")"
go test ./... 2>&1 | tail -20
