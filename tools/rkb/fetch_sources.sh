#!/usr/bin/env bash
# Fetch external reconciliation sources into data/external/ (git-ignored).
# Requires `gh` authenticated against github.com.
set -euo pipefail
mkdir -p "$(dirname "$0")/data/external/dumps"
cd "$(dirname "$0")/data/external"
gh api "repos/ddm999/gt7info/contents/_data/db/course.csv?ref=web-new" --jq '.content' | base64 -d > course.csv
gh api repos/Bornhall/gt7telemetry/contents/gt7trackdetect.csv --jq '.content' | base64 -d > gt7trackdetect.csv
mkdir -p dumps
for id in $(gh api "repos/vthinsel/GT7Tracks/git/trees/master?recursive=1" --jq '.tree[].path' | grep '^dumps/'); do
  gh api "repos/vthinsel/GT7Tracks/contents/$id" --jq '.content' | base64 -d > "$(basename "$id")" || true
done
echo "fetched sources"
