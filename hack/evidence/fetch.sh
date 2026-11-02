#!/bin/bash
# usage: fetch.sh SOURCES_DIR MANIFEST ; downloads each source into its own directory with metadata
set -u
SRC="$1"; MAN="$2"
while IFS=$'\t' read -r id url kind; do
  d="$SRC/$id"; mkdir -p "$d"
  ext=$kind; [ "$kind" = md ] && ext=md
  code=$(curl -sS -L -m 60 -A "evidence-sweep/1.0 (documentation review)" -o "$d/raw.$ext" -w "%{http_code}|%{url_effective}|%{size_download}" "$url" 2>"$d/curl.err")
  sha=$(shasum -a 256 "$d/raw.$ext" | cut -d' ' -f1)
  printf '%s\t%s\t%s\t%s\t%s\n' "$id" "$code" "$sha" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$url" >> "$SRC/_fetch.log"
done < "$MAN"
