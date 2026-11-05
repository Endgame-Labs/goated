#!/usr/bin/env bash
set -euo pipefail
export LC_ALL=C

# Work from the repository root, including when invoked from another directory.
script_dir=${BASH_SOURCE[0]%/*}
[[ $script_dir != "${BASH_SOURCE[0]}" ]] || script_dir=.
cd -- "$script_dir/../.."

block_use='^[[:space:]]*(-[[:space:]]+)?["'"'"']?uses["'"'"']?[[:space:]]*:[[:space:]]*'
# Fail closed for flow-style mappings; use block-style `uses:` entries instead.
flow_use='^[[:space:]]*[^#]*[{,][[:space:]]*["'"'"']?uses["'"'"']?[[:space:]]*:'
use_pattern="($block_use|$flow_use)"
ref='(\./[^[:space:]"'"'"'#]+|docker://[^[:space:]"'"'"'#]+|[^[:space:]"'"'"'#@]+@[[:xdigit:]]{40})'
pinned="${block_use}(\"${ref}\"|'${ref}'|${ref})([[:space:]]+(#.*)?)?$"

failed=0
for scope in .github/workflows .; do
  if [[ $scope == .github/workflows ]]; then
    args=(--include='*.yml' --include='*.yaml')
  else
    # Include composite actions anywhere, without scanning dependencies or Git.
    args=(--exclude-dir=.git --exclude-dir=node_modules
      --exclude-dir=.venv --include=action.yml --include=action.yaml)
  fi
  if matches=$(grep -rnE "${args[@]}" "$use_pattern" "$scope"); then
    :
  else
    result=$?
    # grep returns 1 for no matches; other errors must fail the check.
    [[ $result == 1 ]] || exit "$result"
  fi
  [[ -n $matches ]] || continue
  while IFS= read -r match; do
    line=${match#*:}
    line=${line#*:}
    if ! grep -Eq "$pinned" <<< "$line"; then
      printf '%s\n' "$match" >&2
      failed=1
    fi
  done <<< "$matches"
done

if [[ $failed == 1 ]]; then
  printf '%s\n' 'Pin remote actions to a full 40-character commit SHA. Use block-style uses: entries.' >&2
  exit 1
fi
printf '%s\n' 'All remote GitHub Actions are pinned to full commit SHAs.'
