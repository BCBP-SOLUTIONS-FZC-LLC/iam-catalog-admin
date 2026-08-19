#!/usr/bin/env bash
# Installs go-arch-lint if missing, then enforces .go-arch-lint.yml rules
# (LLD §4.2's hexagonal dependency rules — domain/port/service/adapter).
set -euo pipefail

if ! command -v go-arch-lint >/dev/null; then
  go install github.com/fe3dback/go-arch-lint@v1.15.0
fi
go-arch-lint check --project-path .
