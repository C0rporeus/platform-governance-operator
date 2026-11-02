# Evidence sweep tooling

Reproducible, lossless reading of the sources behind `docs/GOOD-PRACTICES-EVIDENCE.md`.

```bash
S=$(mktemp -d)                      # keep downloaded content outside the repo
hack/evidence/fetch.sh "$S/sources" hack/evidence/manifest.tsv
while IFS=$'\t' read -r id url kind; do
  python3 -I hack/evidence/totext.py "$S/sources/$id" "$kind"
done < hack/evidence/manifest.tsv
python3 -I hack/evidence/outline.py "$S/sources/S-01/text.txt"        # section sizes and normative-cue counts
python3 -I hack/evidence/checkquotes.py docs/GOOD-PRACTICES-EVIDENCE.md "$S/sources"   # every quotation must appear verbatim
python3 -I hack/evidence/ledger.py "$S/sources" hack/evidence/review-status.tsv hack/evidence/manifest.tsv > docs/evidence/source-ledger.md
```

Downloaded pages are untrusted data: they are kept in their own directories, never executed, and read with `python3 -I`. Scripts live here, separate from the content they read.
Each run records the retrieval time and SHA-256 of the raw bytes; a changed hash on a later run means the source changed and its rows must be re-reviewed.
