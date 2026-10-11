#!/usr/bin/env bash
set -euo pipefail

OUTDIR="qa-frames"
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
  "qa-01-unlock.png|1|Launch Vibe Studio|Unlock screen renders with Open File and Create New buttons"
  "qa-02-dashboard.png|2|Open Dashboard|Stat cards show page/table/query counts and file size"
  "qa-03-new-page-validation.png|3|New Page from Dashboard|#4 fix: ?new=1 opens modal, #6 fix: Create disabled when title empty"
  "qa-03-pages.png|4|Browse Pages|#1 fix: Goldmark renders markdown bold/italic in page content"
  "qa-04-page-editor.png|5|Edit Page|Split editor with markdown source and goldmark-rendered preview"
  "qa-05-tables.png|6|Browse Tables|Table list sidebar with sales table visible"
  "qa-06-table-data.png|7|View Table Data|Data grid renders 3 rows with id, product, amount columns"
  "qa-06b-delete-confirm.png|8|Delete Row Confirm|#4 fix: Custom ConfirmDialog replaces window.confirm()"
  "qa-07-queries.png|9|Open Query Editor|SQL editor with engine selector and Run button"
  "qa-08-query-results.png|10|Execute SQL Query|Results: 3 rows sorted by amount DESC"
  "qa-09-agent.png|11|Open Agent Panel|#2 fix: Description says SQL query only, no NL claim"
  "qa-10-import.png|12|Open Import View|CSV file picker with table name input field"
  "qa-11-settings.png|13|Open Settings|#5 fix: updated_at timestamp present in metadata"
  "qa-12-close-state-sync.png|14|Close via Go Binding|#3 fix: file:closed event syncs UI to unlock screen"
  "qa-13-reopen-state-sync.png|15|Reopen via Go Binding|#3 fix: file:opened event syncs UI back + updated_at changed"
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
    -annotate +60+30 "Vibe Studio QA // $(date +%Y-%m-%d)" \
    \
    -font "Courier-Bold" -fill "#555555" \
    -pointsize 16 -gravity SouthWest \
    -annotate +60+30 "Wails v2 + Vue 3 + SQLCipher + DuckDB" \
    \
    "${OUTDIR}/frame_${PADDED}.png"

done

echo ""
echo "Generating video..."

# Create video: 2.5s per frame, with fade transitions
FRAME_DUR=2.5
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
  qa-video.mp4

echo ""
echo "Done! Output: qa-video.mp4"
echo "Frames: $FRAME_COUNT | Duration: ~$(echo "$FRAME_COUNT * $FRAME_DUR" | bc)s"

# Cleanup
rm -f /tmp/qa_panel_*.png /tmp/qa_shot_*.png
