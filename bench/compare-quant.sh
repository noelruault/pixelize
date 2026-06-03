#!/usr/bin/env bash
#
# compare-quant.sh measures pixelize's palette *derivation* (-palette auto:N)
# against pngquant and ImageMagick on the same task: derive an N-color palette,
# reduce the image, write a PNG — same input pixels, no dithering — so only
# palette quality is compared.
#
# This script uses the SHIPPED pixelize binary, so it validates the feature as
# released. It reports RMSE (via ImageMagick's compare), a quality proxy that is
# reproducible anywhere with just these three tools. The authoritative,
# perceptual metric for the README's claim is mean CIEDE2000, computed by the
# research harness (which beats RMSE for color fidelity); the full CIEDE2000
# comparison, the method, and the kept/discarded experiments live in:
#   noelruault/research/quantization  (bench/compare-quant.sh + emit/score)
#
# Requirements: a Go toolchain (to build pixelize), pngquant, and ImageMagick
# (`magick` on v7, or `convert`/`compare` on v6) on PATH.
#
# Usage: bench/compare-quant.sh "16 64 256" /path/to/images
set -euo pipefail

NS="${1:-16 64 256}"
SRC="${2:-docs/demo/inputs}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# Resolve ImageMagick v6/v7 entrypoints.
if command -v magick >/dev/null 2>&1; then IM=magick; CMP="magick compare"; else IM=convert; CMP=compare; fi

echo "# pixelize -palette auto vs pngquant vs ImageMagick"
echo "# metric: RMSE (normalized 0..1, lower=better); no dither; mean over corpus"
echo "# CIEDE2000 numbers + method: noelruault/research/quantization"
go build -o "$WORK/pixelize" "$ROOT/cmd/pixelize"

# normalized RMSE of b vs a, parsed from ImageMagick compare's "(x)". compare
# exits non-zero whenever the images differ, which is always here, so swallow it.
rmse() {
  local out
  out=$($CMP -metric RMSE "$1" "$2" null: 2>&1 || true)
  echo "$out" | sed -n 's/.*(\([0-9.]*\)).*/\1/p'
}

printf "%-5s %12s %12s %12s\n" N pixelize pngquant imagemagick
for N in $NS; do
  declare -A S=([px]=0 [pq]=0 [im]=0); C=0
  for f in "$SRC"/*.jpg "$SRC"/*.png; do
    [ -e "$f" ] || continue
    o="$WORK/o.png"; $IM "$f" "$o"
    "$WORK/pixelize" "$f" -palette "auto:$N" -o "$WORK/px.png" >/dev/null 2>&1
    pngquant --force --nofs --output "$WORK/pq.png" "$N" -- "$o" 2>/dev/null || cp "$o" "$WORK/pq.png"
    $IM "$o" -dither None -colors "$N" "$WORK/im.png"
    for k in px pq im; do
      v=$(rmse "$o" "$WORK/$k.png"); S[$k]=$(awk -v s="${S[$k]}" -v x="${v:-0}" 'BEGIN{print s+x}')
    done
    C=$((C+1))
  done
  printf "%-5s %12.5f %12.5f %12.5f\n" "$N" \
    "$(awk -v s="${S[px]}" -v c="$C" 'BEGIN{print s/c}')" \
    "$(awk -v s="${S[pq]}" -v c="$C" 'BEGIN{print s/c}')" \
    "$(awk -v s="${S[im]}" -v c="$C" 'BEGIN{print s/c}')"
done

cat <<'NOTE'

# Note: this is RGB RMSE. At N>48 pixelize derives in OKLab, optimizing
# PERCEPTUAL error (CIEDE2000), so it may trail pngquant on RGB-RMSE at larger N
# while beating it on CIEDE2000 — the metric that tracks what the eye sees, and
# the one the README table and the research record report. RMSE here is only a
# dependency-free sanity proxy.
NOTE
