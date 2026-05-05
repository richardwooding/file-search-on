#!/usr/bin/env bash
# Regenerate the public-domain fixture bank under fixtures/.
#
# This script is **dev-time only**. CI does NOT run it. Fixtures are
# committed binaries; re-running this only matters when you add a new
# format or want to refresh them.
#
# Tools required on PATH:
#
#   ffmpeg     — audio + video synthesis (Homebrew: brew install ffmpeg)
#   pandoc     — EPUB / DOCX / ODT generation (brew install pandoc)
#   magick     — ImageMagick 7 for image fixtures (brew install imagemagick)
#   cwebp      — WebP encoder (brew install webp)
#   heif-enc   — HEIC encoder (brew install libheif)
#   uvx        — to run reportlab / openpyxl / python-pptx in ephemeral
#                Python venvs (brew install uv)
#
# All output is CC0 / public domain — every byte is generated from
# trivially-original prose, solid colours, sine-wave silence, or lavfi
# test patterns.

set -euo pipefail

cd "$(dirname "$0")/fixtures"

# ─── Hand-written text fixtures (committed; not regenerated) ───────────────
# sample.md, sample.html, sample.xml, sample.json, sample.csv, sample.tsv,
# sample.txt, sample.svg are edited by hand. The script doesn't touch them.

# ─── Images ────────────────────────────────────────────────────────────────
echo "→ images"
magick -size 16x16 xc:'#0d1117' \
  -define exif:DateTimeOriginal='2026:01:15 12:00:00' \
  -define exif:Make='TestCam' -define exif:Model='F100' \
  sample.jpg
magick -size 16x16 xc:'#7d8590' sample.png
magick -size 16x16 xc:'#3fb950' sample.gif
magick -size 16x16 xc:'#79c0ff' sample.tiff
magick -size 16x16 xc:'#ffa657' BMP3:sample.bmp
cwebp -quiet -q 80 sample.png -o sample.webp
heif-enc -q 50 sample.jpg -o sample.heic >/dev/null

# ─── Audio (1 s silence) ───────────────────────────────────────────────────
echo "→ audio"
common_audio_meta=(
  -metadata title='Sample Audio Fixture'
  -metadata artist='file-search-on'
  -metadata album='Test Fixtures'
  -metadata date='2026'
)

ffmpeg -y -loglevel error -f lavfi -i 'anullsrc=r=44100:cl=mono' -t 1 \
  "${common_audio_meta[@]}" \
  -id3v2_version 3 -c:a libmp3lame -b:a 64k sample.mp3

ffmpeg -y -loglevel error -f lavfi -i 'anullsrc=r=44100:cl=mono' -t 1 \
  "${common_audio_meta[@]}" -c:a aac -b:a 64k sample.m4a

ffmpeg -y -loglevel error -f lavfi -i 'anullsrc=r=44100:cl=mono' -t 1 \
  "${common_audio_meta[@]}" -c:a flac sample.flac

# Vorbis encoder requires stereo input.
ffmpeg -y -loglevel error -f lavfi -i 'anullsrc=r=44100:cl=stereo' -t 1 \
  "${common_audio_meta[@]}" -c:a vorbis -strict -2 sample.ogg

# ─── Video (1 s lavfi testsrc + 1 s sine audio) ────────────────────────────
echo "→ video"
# 64x48 testsrc video + a 1-second 440 Hz sine audio track. The audio track
# exercises sample_rate / channels / audio_codec extraction from the video
# container parsers (MP4 stsd, MKV Audio element, AVI WAVEFORMATEX in strf).
# MP4 / MOV: mono AAC; MKV: stereo Vorbis (its only encoder); WebM: stereo
# Opus; AVI: mono MP3. All resampled to 44.1 kHz (48 kHz for WebM/Opus).
ffmpeg -y -loglevel error \
  -f lavfi -i 'testsrc=size=64x48:duration=1:rate=10' \
  -f lavfi -i 'sine=frequency=440:duration=1' \
  -c:v libx264 -preset ultrafast -pix_fmt yuv420p \
  -c:a aac -b:a 64k -ar 44100 \
  sample.mp4
ffmpeg -y -loglevel error \
  -f lavfi -i 'testsrc=size=64x48:duration=1:rate=10' \
  -f lavfi -i 'sine=frequency=440:duration=1' \
  -c:v libx264 -preset ultrafast -pix_fmt yuv420p \
  -c:a aac -b:a 64k -ar 44100 \
  -f mov sample.mov
ffmpeg -y -loglevel error \
  -f lavfi -i 'testsrc=size=64x48:duration=1:rate=10' \
  -f lavfi -i 'sine=frequency=440:duration=1' \
  -c:v libx264 -preset ultrafast -pix_fmt yuv420p \
  -c:a vorbis -strict -2 -ac 2 -ar 44100 \
  -f matroska sample.mkv
ffmpeg -y -loglevel error \
  -f lavfi -i 'testsrc=size=64x48:duration=1:rate=10' \
  -f lavfi -i 'sine=frequency=440:duration=1' \
  -c:v libvpx-vp9 -b:v 50k -c:a libopus -ac 2 -ar 48000 \
  -f webm sample.webm
ffmpeg -y -loglevel error \
  -f lavfi -i 'testsrc=size=64x48:duration=1:rate=10' \
  -f lavfi -i 'sine=frequency=440:duration=1' \
  -c:v mpeg4 -c:a libmp3lame -b:a 64k -ar 44100 \
  -f avi sample.avi

# ─── PDF (reportlab) ───────────────────────────────────────────────────────
echo "→ pdf"
uvx --quiet --with reportlab python3 - <<'PY'
from reportlab.pdfgen import canvas
from reportlab.lib.pagesizes import letter
c = canvas.Canvas("sample.pdf", pagesize=letter, lang="en")
c.setTitle("Sample PDF Fixture")
c.setAuthor("file-search-on test fixtures")
c.drawString(100, 750, "Sample PDF Fixture")
c.drawString(100, 730, "Generated for the content-type test suite.")
c.showPage()
c.save()
PY

# ─── EPUB / DOCX / ODT (pandoc) ────────────────────────────────────────────
echo "→ epub / docx / odt"
tmp_md=$(mktemp).md
arc_tmp=$(mktemp -d)
trap 'rm -f "$tmp_md"; rm -rf "$arc_tmp"' EXIT

cat > "$tmp_md" <<'EOF'
---
title: Sample Office Fixture
author: file-search-on test fixtures
lang: en
---

# Sample Office Fixture

Generated for the content-type test suite.
EOF

pandoc -f markdown -t epub "$tmp_md" -o sample.epub
pandoc -f markdown -t docx "$tmp_md" -o sample.docx
pandoc -f markdown -t odt  "$tmp_md" -o sample.odt

# ─── XLSX (openpyxl) ───────────────────────────────────────────────────────
echo "→ xlsx"
uvx --quiet --with openpyxl python3 - <<'PY'
import openpyxl
wb = openpyxl.Workbook()
wb.properties.title = "Sample XLSX Fixture"
wb.properties.creator = "file-search-on test fixtures"
wb.properties.language = "en"
ws = wb.active
ws.title = "Sheet1"
ws.append(["id", "name", "revenue"])
ws.append([1, "Alpha", 1234.56])
ws.append([2, "Beta", 2345.67])
wb.save("sample.xlsx")
PY

# ─── PPTX (python-pptx) ────────────────────────────────────────────────────
echo "→ pptx"
uvx --quiet --with python-pptx python3 - <<'PY'
from pptx import Presentation
p = Presentation()
p.core_properties.title = "Sample PPTX Fixture"
p.core_properties.author = "file-search-on test fixtures"
p.core_properties.language = "en"
slide = p.slides.add_slide(p.slide_layouts[0])
slide.shapes.title.text = "Sample PPTX Fixture"
slide.placeholders[1].text = "Generated for the content-type test suite."
p.save("sample.pptx")
PY

# ─── Archives ──────────────────────────────────────────────────────────────
# Five fixtures cover the four registered archive families plus the .jar
# extension alias of archive/zip. All entries live under a single top-level
# directory `sample/` so has_root_dir=true on every multi-entry fixture.
#
# COPYFILE_DISABLE=1 + --no-xattrs are mandatory on macOS — without them tar
# injects AppleDouble `._*` metadata files that pollute top_level_entries.
echo "→ archives"
fixtures_dir="$PWD"
mkdir -p "$arc_tmp/sample"
printf 'hello\n' > "$arc_tmp/sample/README.txt"
printf 'x'       > "$arc_tmp/sample/data.txt"
printf 'y'       > "$arc_tmp/sample/more.txt"

rm -f sample.zip sample.tar sample.tar.gz sample.gz sample.jar
(cd "$arc_tmp" && zip -qr "$fixtures_dir/sample.zip" sample)
COPYFILE_DISABLE=1 tar --no-xattrs -cf sample.tar -C "$arc_tmp" sample
COPYFILE_DISABLE=1 tar --no-xattrs -czf sample.tar.gz -C "$arc_tmp" sample

# Standalone gzip — single stream over a small text payload.
printf 'gzip standalone payload\n' > "$arc_tmp/sample.txt"
gzip -c "$arc_tmp/sample.txt" > sample.gz

# .jar is a ZIP under another extension — same bytes work.
cp sample.zip sample.jar

echo
echo "Done. Inventory:"
ls -la
