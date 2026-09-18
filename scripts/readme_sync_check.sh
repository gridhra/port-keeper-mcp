#!/bin/sh
# Checks that README.ja.md and README.zh-CN.md mirror README.md section by
# section: same number of h2/h3 headings and fenced code blocks (headings
# inside fences are ignored), and the same sequence of CLI and MCP tool table
# rows (rows whose first cell starts with a backtick; only the backticked
# spans of the first cell are compared, so a translated "or" between them is fine).
#
#   sh scripts/readme_sync_check.sh
set -eu

here="$(cd "$(dirname "$0")/.." && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT INT TERM
status=0

# counts FILE: prints "h2 h3 fences" counted outside fenced code blocks.
counts() {
  awk '
    /^```/ { fence++; infence = !infence; next }
    infence { next }
    /^## / { h2++ }
    /^### / { h3++ }
    END { printf "%d %d %d\n", h2 + 0, h3 + 0, fence + 0 }
  ' "$1"
}

# rows FILE: for every table row whose first cell starts with a backtick (the
# CLI table and the MCP tools table), outside fences, prints the backticked
# spans of that cell joined by a space.
rows() {
  awk -F'|' '
    /^```/ { infence = !infence; next }
    infence { next }
    /^\| `/ {
      cell = $2; out = ""
      while (match(cell, /`[^`]*`/)) {
        out = out (out == "" ? "" : " ") substr(cell, RSTART, RLENGTH)
        cell = substr(cell, RSTART + RLENGTH)
      }
      print out
    }
  ' "$1"
}

ref_counts=""
printf '%-16s %4s %4s %6s %4s\n' file h2 h3 fences rows
for name in README.md README.ja.md README.zh-CN.md; do
  file="$here/$name"
  c="$(counts "$file")"
  h2="${c%% *}"; rest="${c#* }"; h3="${rest%% *}"; fences="${rest#* }"
  rows "$file" > "$tmp/$name.rows"
  nrows=$(wc -l < "$tmp/$name.rows" | tr -d ' ')
  printf '%-16s %4s %4s %6s %4s\n' "$name" "$h2" "$h3" "$fences" "$nrows"
  if [ $((fences % 2)) -ne 0 ]; then
    printf '%s: unbalanced fences\n' "$name" >&2
    exit 1
  fi
  if [ -z "$ref_counts" ]; then
    ref_counts="$h2 $h3 $fences"
    continue
  fi
  if [ "$h2 $h3 $fences" != "$ref_counts" ]; then
    printf '%s: heading or fence counts differ from README.md\n' "$name" >&2
    status=1
  fi
  if ! cmp -s "$tmp/README.md.rows" "$tmp/$name.rows"; then
    printf '%s: table rows differ from README.md (< README.md, > %s):\n' "$name" "$name" >&2
    diff "$tmp/README.md.rows" "$tmp/$name.rows" >&2 || true
    status=1
  fi
done

if [ "$status" -ne 0 ]; then
  echo "README mirrors out of sync: update README.ja.md and README.zh-CN.md in the same commit" >&2
fi
exit "$status"
