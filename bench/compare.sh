#!/usr/bin/env bash
#
# compare.sh measures pixelize against ImageMagick on the same quantization
# task: take an image, reduce it to a fixed palette, write a PNG.
#
# The comparison is apples to apples. Both tools get the same input pixels at
# the same dimensions, neither resizes, and neither dithers. Each image is
# pre-resized once with ImageMagick so the resize algorithm is not part of the
# measurement. Both target the same palette: pixelize renders the palette to a
# swatch PNG, and ImageMagick remaps to that same swatch. What is left is each
# tool's nearest-color implementation, which is the thing worth timing.
#
# Numbers are averaged over every image in the input set and reported per run
# in milliseconds, for two sizes and three palette sizes (small, medium, large).
#
# Requirements: a Go toolchain (to build pixelize), and ImageMagick on PATH
# (the `magick` binary on v7, or `convert`/`compare`/`identify` on v6).
#
# Optional second ImageMagick, for example a Q8 build, compared alongside the
# one on PATH:
#   IM_ALT=/path/to/convert IM_ALT_LABEL="ImageMagick Q8" bench/compare.sh
#
# Usage:
#   RUNS=10 bench/compare.sh
# RUNS is per image per cell (default 10).

set -euo pipefail

SRC_DIR="docs/demo/inputs"
IMAGES=(athens liberty monet napoleon pearl starry)
PALETTES=(gameboy nes lego)   # small, medium, large
SIZES=(256 512)
RUNS="${RUNS:-10}"

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo_root"

# Resolve ImageMagick. v7 ships `magick`; v6 ships `convert`.
if command -v magick >/dev/null 2>&1; then
  IM=(magick)
elif command -v convert >/dev/null 2>&1; then
  IM=(convert)
else
  echo "error: ImageMagick not found (need magick or convert on PATH)" >&2
  exit 1
fi
IM_LABEL="${IM_LABEL:-ImageMagick}"
IM_ALT="${IM_ALT:-}"
IM_ALT_LABEL="${IM_ALT_LABEL:-ImageMagick (alt)}"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

echo "building pixelize..."
go build -o "$work/pixelize" ./cmd/pixelize
PX="$work/pixelize"

# Globals the runners read, set inside the loops below.
CUR_PAL=""
CUR_SWATCH=""

run_px()    { "$PX" "$1" -palette "$CUR_PAL" -o "$work/px.png" >/dev/null 2>&1; }
run_im()    { "${IM[@]}" "$1" +dither -remap "$CUR_SWATCH" "$work/im.png" >/dev/null 2>&1; }
run_imalt() { "$IM_ALT" "$1" +dither -remap "$CUR_SWATCH" "$work/imalt.png" >/dev/null 2>&1; }

# time_runner FN INPUT... runs FN on every input RUNS times, and prints
# "wall_ms cpu_ms" averaged over all executions.
time_runner() {
  local fn="$1"; shift
  local ins=("$@") out real user sys n
  out=$(
    TIMEFORMAT='%R %U %S'
    { time ( for in in "${ins[@]}"; do for ((i = 0; i < RUNS; i++)); do "$fn" "$in"; done; done ); } 2>&1
  )
  read -r real user sys <<<"$out"
  n=$((${#ins[@]} * RUNS))
  awk -v r="$real" -v u="$user" -v s="$sys" -v n="$n" \
    'BEGIN { printf "%d %d\n", r * 1000 / n, (u + s) * 1000 / n }'
}

# Hardware and tool versions, so the output says where it ran. Numbers are
# only meaningful next to the machine that produced them.
cpu="$(grep -m1 'model name' /proc/cpuinfo 2>/dev/null | cut -d: -f2 | sed 's/^ *//' || true)"
[ -z "$cpu" ] && cpu="$(sysctl -n machdep.cpu.brand_string 2>/dev/null || echo unknown)"
cores="$(getconf _NPROCESSORS_ONLN 2>/dev/null || echo '?')"

echo
echo "machine:     $(uname -sm), ${cores} cores"
echo "cpu:         $cpu"
echo "images:      ${#IMAGES[@]} from $SRC_DIR"
echo "runs:        $RUNS per image per cell"
echo "$IM_LABEL: $($IM -version | head -1 | sed 's/Version: //')"
[ -n "$IM_ALT" ] && echo "$IM_ALT_LABEL: $($IM_ALT -version | head -1 | sed 's/Version: //')"
echo "go:          $(go version | awk '{print $3}')"
echo "date:        $(date -u +%Y-%m-%dT%H:%M:%SZ)"
echo
echo "ms per run, averaged over all images, lower is better. wall is elapsed; cpu is user + system."
alt_head=""
[ -n "$IM_ALT" ] && alt_head=$(printf '  %-18s' "$IM_ALT_LABEL")
printf '%-9s %-7s %-9s  %-18s  %-18s%s  %s\n' \
  "palette" "colors" "size" "pixelize wall/cpu" "$IM_LABEL wall/cpu" "$alt_head" "px vs $IM_LABEL diff"

for pal in "${PALETTES[@]}"; do
  CUR_PAL="$pal"
  CUR_SWATCH="$work/sw_${pal}.png"
  "$PX" palette "$pal" -show "$CUR_SWATCH" >/dev/null 2>&1
  colors="$(identify -format '%k' "$CUR_SWATCH")"

  for s in "${SIZES[@]}"; do
    # Pre-resize every image once to exact dimensions; resize is not measured.
    ins=()
    for name in "${IMAGES[@]}"; do
      in="$work/${name}_${s}.png"
      "$IM" "$SRC_DIR/${name}.jpg" -resize "${s}x${s}!" "$in"
      ins+=("$in")
    done

    read -r px_wall px_cpu < <(time_runner run_px "${ins[@]}")
    read -r im_wall im_cpu < <(time_runner run_im "${ins[@]}")
    alt_cell=""
    if [ -n "$IM_ALT" ]; then
      read -r alt_wall alt_cpu < <(time_runner run_imalt "${ins[@]}")
      alt_cell=$(printf '  %-18s' "${alt_wall}/${alt_cpu}")
    fi

    # Mean percent of pixels where pixelize and ImageMagick pick a different
    # color, on identical input.
    sum=0
    for in in "${ins[@]}"; do
      run_px "$in"; run_im "$in"
      ae=$(compare -metric AE "$work/px.png" "$work/im.png" "$work/diff.png" 2>&1 || true)
      sum=$(awk -v a="$sum" -v b="${ae%%.*}" 'BEGIN { print a + b }')
    done
    total=$((s * s * ${#ins[@]}))
    pct=$(awk -v sum="$sum" -v t="$total" 'BEGIN { printf "%.1f%%", 100 * sum / t }')

    printf '%-9s %-7s %-9s  %-18s  %-18s%s  %s\n' \
      "$pal" "$colors" "${s}x${s}" "${px_wall}/${px_cpu}" "${im_wall}/${im_cpu}" "$alt_cell" "$pct"
  done
done
