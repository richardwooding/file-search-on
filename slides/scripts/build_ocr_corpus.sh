#!/usr/bin/env bash
# Generate the OCR demo corpus.
#
# Produces 12 synthetic text-bearing JPGs in ~/Pictures/ocr-demo/ using
# ImageMagick. No real screenshots or private content. Re-runnable;
# overwrites in place.
#
# Why JPG and not PNG: file-search-on's image_similar_to / EXIF / OCR
# pipeline detects JPEGs uniformly via the `image/jpeg` content type,
# matching the existing south-africa-holiday corpus.

set -euo pipefail

DEST="${HOME}/Pictures/ocr-demo"
mkdir -p "$DEST"

# Pick a monospace + a sans-serif font that ship with macOS. ImageMagick
# on Homebrew can't resolve font names without a fontconfig file, so we
# point at the .ttc files directly. Bold is selected via the SAME .ttc
# with -weight 700 (Helvetica.ttc is a collection containing Bold).
MONO="/System/Library/Fonts/Menlo.ttc"
SANS="/System/Library/Fonts/Helvetica.ttc"
BOLD="/System/Library/Fonts/Helvetica.ttc"

# Helper: render a multi-line caption onto a coloured rectangle, save as
# JPG. Args: filename bg-colour fg-colour font pointsize text(literal \n).
render() {
  local out="$1" bg="$2" fg="$3" font="$4" size="$5" text="$6"
  convert -size 1200x800 "xc:${bg}" \
    -font "$font" -pointsize "$size" -fill "$fg" \
    -gravity center -annotate +0+0 "$text" \
    -quality 88 "${DEST}/${out}"
}

# 1. Terminal error — "ERROR ... Connection refused ..."
render error_terminal.jpg black "#5fff5f" "$MONO" 36 \
  "ERROR: Connection refused
at db.internal:5432
retry attempt 3/5"

# 2. Log entry — also contains "ERROR" but in different formatting
render log_entry.jpg white black "$MONO" 28 \
  "[2026-05-28 09:14:32] ERROR
worker.go:47
timeout exceeded after 30s"

# 3. Receipt — "TOTAL"
render receipt.jpg "#fafafa" black "$SANS" 38 \
  "Pick n Pay
Greenpoint Mall

2 x bread       R 24.99
1 x milk        R 18.50
1 x rooibos tea R 84.01

TOTAL: R 127.50"

# 4. Invoice — "INVOICE"
render invoice.jpg white black "$SANS" 44 \
  "INVOICE
Number: 2026-0042
Due: 2026-06-15
Amount: ZAR 8,450.00

Span Digital (Pty) Ltd"

# 5. Welcome sign — "WELCOME TO CAPE TOWN"
render welcome_sign.jpg "#f5c518" black "$BOLD" 90 \
  "WELCOME TO
CAPE TOWN"

# 6. Meeting notes — "Athena" (the unique-keyword test)
render meeting_notes.jpg white black "$SANS" 38 \
  "MEETING NOTES

Project Athena Q3

Action items:
1. Migrate primary DB
2. Update onboarding docs
3. Audit feature flags"

# 7. Meetup poster — pretty colour, big text
render meetup_poster.jpg "#5b2a86" white "$BOLD" 72 \
  "GoLang Meetup

Thursday 7pm
The Workshop, Salt River

RSVP on meetup.com"

# 8. Road sign — "JOHANNESBURG" / "DURBAN"
render road_sign.jpg "#117030" white "$BOLD" 70 \
  "N1 -> JOHANNESBURG
N2 -> DURBAN"

# 9. Whiteboard — bullets
render whiteboard.jpg "#fdf6e3" "#222222" "$SANS" 40 \
  "Decisions

- Adopt OpenTelemetry
- Defer GraphQL migration
- Postpone Q4 launch
- Hire 2 SRE engineers"

# 10. Conference slide — "Why CEL?"
render conf_slide.jpg white black "$SANS" 44 \
  "Why CEL?

- Sandboxed
- Fast
- Declarative

Used by GCP, Kubernetes, Istio"

# 11. Printed email
render printed_email.jpg white black "$SANS" 32 \
  "From: alice@example.com
To: bob@example.com
Subject: Urgent payment overdue

Dear Bob,
Please remit invoice 2026-0042
by Friday or interest applies."

# 12. Code screenshot — Go panic
render code_screenshot.jpg "#1e1e1e" "#dcdcdc" "$MONO" 32 \
  "func main() {
    err := db.Connect()
    if err != nil {
        log.Fatal(err)
    }
    panic(\"unreachable\")
}"

ls -lh "$DEST"/*.jpg | awk '{print $9, $5}'
echo "[done] $(ls "$DEST"/*.jpg | wc -l | tr -d ' ') images in $DEST"
