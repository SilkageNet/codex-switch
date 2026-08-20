#!/usr/bin/env sh
set -eu

report_file=$(mktemp)
trap 'rm -f "$report_file"' EXIT INT TERM

set +e
govulncheck ./... >"$report_file" 2>&1
scan_status=$?
set -e
cat "$report_file"

if [ "$scan_status" -eq 0 ]; then
  exit 0
fi
if [ "$scan_status" -ne 3 ]; then
  exit "$scan_status"
fi

found_ids=$(sed -n 's/^[[:space:]]*Vulnerability #[0-9][0-9]*: \(GO-[0-9-]*\)$/\1/p' "$report_file" | sort -u)
allowed_ids='GO-2026-5026
GO-2026-5972
GO-2026-6090
GO-2026-6218'

if [ "$found_ids" = "$allowed_ids" ]; then
  echo "Recognized Go 1.26.5 standard-library advisories awaiting the published Go 1.26.6 fix."
  exit 0
fi

echo "Vulnerability findings differ from the narrowly documented temporary exception." >&2
exit "$scan_status"
