#!/usr/bin/env bash
# Fail if any Go file is not gofmt-clean.
set -euo pipefail

unformatted=$(gofmt -l . 2>/dev/null | grep -v '^bin/' | grep -v '^dist/' || true)

if [ -n "$unformatted" ]; then
    echo "ERROR: the following files are not gofmt-formatted:" >&2
    echo "$unformatted" >&2
    echo "" >&2
    echo "Run: gofmt -w <files>" >&2
    exit 1
fi
