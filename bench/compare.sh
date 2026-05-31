#!/usr/bin/env bash
#
# compare.sh measures pixelize against ImageMagick on the same quantization
# task: take one image, reduce it to a fixed palette, write a PNG.
#
# The comparison is apples to apples. Both tools get the same input pixels at
# the same dimensions, neither resizes, and neither dithers. The image is
# pre-resized once with ImageMagick so the resize algorithm is not part of the
# measurement. Both target the same palette: pixelize renders the palette to a
# swatch PNG, and ImageMagick remaps to that same swatch. What is left is each
# tool's nearest-color implementation, which is the thing worth timing.
#
# Requirements: a Go toolchain (to build pixelize), and ImageMagick on PATH
# (the `magick` binary on v7, or `convert`/`compare`/`identify` on v6).
#
# Usage:
#   bench/compare.sh [IMAGE] [PALETTE] [RUNS]
# Defaults: docs/demo/inputs/starry.jpg, nes, 50 runs.

set -euo pipefail

SRC="${1:-docs/demo/inputs/starry.jpg}"
PALETTE="${2:-nes}"
RUNS="${3:-50}"
SIZES=(256 512)

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

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

echo "building pixelize..."
go build -o "$work/pixelize" ./cmd/pixelize
PX="$work/pixelize"

swatch="$work/palette.png"
"$PX" palette "$PALETTE" -show "$swatch" >/dev/null 2>&1

# run_n CMD... times RUNS executions and prints two whole-millisecond numbers
# per run: wall-clock and CPU (user + system).
run_n() {
  local out real user sys
  out=$(
    TIMEFORMAT='%R %U %S'
    { time ( for ((i = 0; i < RUNS; i++)); do "$@" >/dev/null 2>&1; done ); } 2>&1
  )
  read -r real user sys <<<"$out"
  # bash prints seconds with a decimal; strip the dot to get integer ms.
  local wall_ms cpu_ms
  wall_ms=$(awk -v r="$real" 'BEGIN { printf "%d", r * 1000 / '"$RUNS"' }')
  cpu_ms=$(awk -v u="$user" -v s="$sys" 'BEGIN { printf "%d", (u + s) * 1000 / '"$RUNS"' }')
  echo "$wall_ms $cpu_ms"
}

# Hardware and tool versions, so the output says where it ran. Numbers are
# only meaningful next to the machine that produced them.
cpu="$(grep -m1 'model name' /proc/cpuinfo 2>/dev/null | cut -d: -f2 | sed 's/^ *//' || true)"
[ -z "$cpu" ] && cpu="$(sysctl -n machdep.cpu.brand_string 2>/dev/null || echo unknown)"
cores="$(getconf _NPROCESSORS_ONLN 2>/dev/null || echo '?')"

echo
echo "machine:     $(uname -sm), ${cores} cores"
echo "cpu:         $cpu"
echo "image:       $SRC"
echo "palette:     $PALETTE ($(identify -format '%k' "$swatch") unique colors)"
echo "runs:        $RUNS per cell"
echo "imagemagick: $($IM -version | head -1 | sed 's/Version: //')"
echo "go:          $(go version | awk '{print $3}')"
echo "date:        $(date -u +%Y-%m-%dT%H:%M:%SZ)"
echo
echo "ms per run, lower is better. wall is elapsed time; cpu is user + system."
echo
printf '%-9s  %-21s  %-21s  %s\n' "size" "pixelize wall/cpu" "imagemagick wall/cpu" "pixels differing"

for s in "${SIZES[@]}"; do
  in="$work/in_${s}.png"
  px="$work/px_${s}.png"
  im="$work/im_${s}.png"

  # Pre-resize once to exact dimensions so resize is not measured.
  "$IM" "$SRC" -resize "${s}x${s}!" "$in"

  # Same input, no resize (no -size), no dither.
  read -r px_wall px_cpu < <(run_n "$PX" "$in" -palette "$PALETTE" -o "$px")
  read -r im_wall im_cpu < <(run_n "$IM" "$in" +dither -remap "$swatch" "$im")

  # How close are the two results, on identical input.
  diff=$(compare -metric AE "$px" "$im" "$work/diff_${s}.png" 2>&1 || true)
  total=$((s * s))

  printf '%-9s  %-21s  %-21s  %s / %s\n' "${s}x${s}" \
    "${px_wall}/${px_cpu}" "${im_wall}/${im_cpu}" "$diff" "$total"
done
