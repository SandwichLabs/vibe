#!/usr/bin/env bash
set -euo pipefail

OUTDIR="qa-frames-grid"
CANVAS_W=1920
CANVAS_H=1080
SCREENSHOT_MAX_W=520
SCREENSHOT_MAX_H=1000
BG="#1A1A1A"
PANEL_BG="#FFD700"
BORDER="#000000"
TEXT_COLOR="#000000"
STEP_COLOR="#000000"

rm -rf "$OUTDIR"
mkdir -p "$OUTDIR"

# Define frames: filename|step|action|test
declare -a FRAMES=(
  "grid-01-initial.png|1|Launch Vibe Studio|Welcome screen with Open File and Create New buttons"
  "grid-02-dashboard.png|2|Create QA File|Dashboard: Grid Layout QA file with 0 pages, 0 tables"
  "grid-05-hd-editor.png|3|Sales Dashboard with Grids|3-col metrics row + 2-col table/chart row + full-width table"
  "grid-08-toolbar-clean.png|4|New Toolbar Buttons|8 toolbar buttons: SQL, charts, table, metric, 2-col, 3-col"
  "grid-10-2col-row-inserted.png|5|Insert 2-Column Layout|Toolbar inserts {row: cols=2}...{/row} snippet, preview shows 2-col grid"
  "grid-11-3col-row-inserted.png|6|Insert 3-Column Layout|Both 2-col and 3-col rows render side by side in preview"
  "grid-12-sales-dashboard-final.png|7|Final Dashboard View|Metrics, regional table, and sales data all in grid layouts"
)

TOTAL=${#FRAMES[@]}
IDX=0

for entry in "${FRAMES[@]}"; do
  IFS='|' read -r FILE STEP ACTION TEST <<< "$entry"
  SRC="qa-screenshots/$FILE"
  IDX=$((IDX + 1))
  PADDED=$(printf "%03d" "$IDX")

  if [[ ! -f "$SRC" ]]; then
    echo "SKIP: $SRC not found"
    continue
  fi

  echo "[$IDX/$TOTAL] $FILE"

  # Panel dimensions
  PANEL_W=1160
  PANEL_H=700

  # Build the text panel with ImageMagick
  convert -size "${PANEL_W}x${PANEL_H}" xc:"${PANEL_BG}" \
    -stroke "$BORDER" -strokewidth 4 \
    -fill none -draw "rectangle 2,2 $((PANEL_W-3)),$((PANEL_H-3))" \
    \
    -font "Courier-Bold" -fill "$STEP_COLOR" \
    -strokewidth 0 -stroke none \
    -pointsize 28 -gravity NorthWest \
    -annotate +40+35 "STEP $STEP of $TOTAL" \
    \
    -font "Helvetica-Bold" -fill "$TEXT_COLOR" \
    -pointsize 52 \
    -annotate +40+90 "$ACTION" \
    \
    -stroke "$BORDER" -strokewidth 3 \
    -draw "line 40,170 $((PANEL_W-40)),170" \
    \
    -font "Courier" -fill "$TEXT_COLOR" \
    -strokewidth 0 -stroke none \
    -pointsize 22 \
    -annotate +40+200 "TEST:" \
    \
    -font "Helvetica" -fill "$TEXT_COLOR" \
    -pointsize 30 \
    -size "$((PANEL_W-100))x" \
    -annotate +40+240 "$TEST" \
    \
    -stroke "$BORDER" -strokewidth 3 \
    -fill none -draw "roundrectangle 30,185 $((PANEL_W-30)),320 0,0" \
    \
    "/tmp/qa_panel_${PADDED}.png"

  # Scale screenshot to fit, add border
  convert "$SRC" \
    -resize "${SCREENSHOT_MAX_W}x${SCREENSHOT_MAX_H}>" \
    -background "$BG" -gravity center -extent "${SCREENSHOT_MAX_W}x${SCREENSHOT_MAX_H}" \
    -bordercolor "$BORDER" -border 4 \
    "/tmp/qa_shot_${PADDED}.png"

  # Compose final frame
  convert -size "${CANVAS_W}x${CANVAS_H}" xc:"${BG}" \
    "/tmp/qa_shot_${PADDED}.png" -gravity West -geometry "+50+0" -composite \
    "/tmp/qa_panel_${PADDED}.png" -gravity East -geometry "+50+0" -composite \
    \
    -font "Courier-Bold" -fill "#FFD700" \
    -pointsize 18 -gravity SouthEast \
    -annotate +60+30 "Vibe Studio QA — Grid Layouts // $(date +%Y-%m-%d)" \
    \
    -font "Courier-Bold" -fill "#555555" \
    -pointsize 16 -gravity SouthWest \
    -annotate +60+30 "Wails v2 + Vue 3 + SQLCipher + DuckDB" \
    \
    "${OUTDIR}/frame_${PADDED}.png"

done

echo ""
echo "Generating video..."

# Create video: 3s per frame, with fade transitions
FRAME_DUR=3
FRAME_COUNT=$(ls "$OUTDIR"/frame_*.png | wc -l)

if [[ "$FRAME_COUNT" -lt 2 ]]; then
  echo "Not enough frames"
  exit 1
fi

# Use concat demuxer with frame duration
LISTFILE="/tmp/qa_ffmpeg_list.txt"
: > "$LISTFILE"
for f in "$OUTDIR"/frame_*.png; do
  echo "file '$(realpath "$f")'" >> "$LISTFILE"
  echo "duration $FRAME_DUR" >> "$LISTFILE"
done
# Repeat last frame (ffmpeg concat quirk)
LAST=$(ls "$OUTDIR"/frame_*.png | tail -1)
echo "file '$(realpath "$LAST")'" >> "$LISTFILE"

ffmpeg -y -f concat -safe 0 -i "$LISTFILE" \
  -vf "format=yuv420p,fade=t=in:st=0:d=0.5,fade=t=out:st=$(echo "$FRAME_COUNT * $FRAME_DUR - 0.5" | bc):d=0.5" \
  -r 30 -c:v libx264 -preset medium -crf 18 \
  -movflags +faststart \
  qa-grid-video.mp4

echo ""
echo "Done! Output: qa-grid-video.mp4"
echo "Frames: $FRAME_COUNT | Duration: ~$(echo "$FRAME_COUNT * $FRAME_DUR" | bc)s"

# Cleanup
rm -f /tmp/qa_panel_*.png /tmp/qa_shot_*.png
