# Recipes — Video

Video content types: `video/mp4` (.mp4 / .m4v), `video/quicktime` (.mov / .qt), `video/x-matroska` (.mkv), `video/webm`, `video/x-msvideo` (.avi). Umbrella boolean `is_video`.

Hand-rolled binary parsers — no CGO, no `ffmpeg` dependency.

## Codec

Find HEVC encodes (smaller files, modern phones):

```sh
file-search-on 'is_video && video_codec == "h265"' -d ~/Videos
```

Find AV1 encodes (newest open codec):

```sh
file-search-on 'is_video && video_codec == "av1"'
```

Find legacy H.264:

```sh
file-search-on 'is_video && video_codec == "h264"'
```

The codec strings are normalised across containers — `avc1` / `avc3` (MP4) and `V_MPEG4/ISO/AVC` (MKV) both report as `"h264"`.

## Audio track in a video

The audio codec inside a video container is captured as `audio_codec`:

```sh
file-search-on 'is_video && audio_codec == "aac"'              # most MP4 / phone clips
file-search-on 'is_video && audio_codec == "opus"'             # WebM / modern MKV
file-search-on 'is_video && audio_codec == "ac3"'              # Blu-ray rips
```

The first audio track's `sample_rate` and `channels` (the same keys used by standalone audio) populate from the video container too:

```sh
file-search-on 'is_video && sample_rate >= 48000'              # 48 kHz audio (broadcast / cinema)
file-search-on 'is_video && channels > 2'                      # surround mixes (5.1, 7.1)
file-search-on 'is_video && channels == 1'                     # mono / interview clips
```

## Resolution

```sh
file-search-on 'is_video && video_height >= 2160'                 # 4K and above
file-search-on 'is_video && video_height >= 1080'                 # 1080p+
file-search-on 'is_video && video_width <= 720'                   # SD / low-res
file-search-on 'is_video && video_width >= 7680'                  # 8K (rare)
```

Aspect ratio (vertical / portrait):

```sh
file-search-on 'is_video && video_height > video_width'           # portrait, e.g. phone clips
```

## Frame rate

```sh
file-search-on 'is_video && frame_rate > 30'                      # high-FPS (slo-mo, gaming)
file-search-on 'is_video && frame_rate >= 60'
file-search-on 'is_video && frame_rate < 25'                      # film transfer / animation
```

The frame rate is computed from the first stts entry (MP4) or DefaultDuration (MKV) / microSecPerFrame (AVI). For VBR content the first entry is taken as the dominant rate.

## Duration

```sh
file-search-on 'is_video && duration > 1800'                      # over 30 minutes
file-search-on 'is_video && duration < 60'                        # short clips
file-search-on 'is_video && duration > 7200'                      # over 2 hours (films)
```

## Bitrate

`bitrate` is computed average (file_size × 8 / duration / 1000), shared with audio. Useful for spotting compressed files:

```sh
file-search-on 'is_video && bitrate < 1000 && video_height >= 1080'   # over-compressed 1080p
file-search-on 'is_video && bitrate > 50000'                          # uncompressed / RAW captures
```

`nominal_bitrate` is the codec/container-declared value. MP4 reads it from the `btrt` box's avgBitrate (a video-track-only number); MKV reads `Bitrate` (0x4FB1); AVI reads `avih.maxBytesPerSec` (a whole-file ceiling — not strictly video-only but the closest stored value). Useful for filtering streaming targets:

```sh
file-search-on 'is_video && nominal_bitrate >= 5000'   # ≥ 5 Mbps — Blu-ray / streaming masters
file-search-on 'is_video && nominal_bitrate < 1000'    # streaming-friendly low-bitrate
```

## Rotation

Phone-recorded clips often store portrait video as 1920×1080 with a 90° rotation matrix in MP4 `tkhd`. The pixel dimensions stay landscape; `rotation` carries the displayed orientation:

```sh
file-search-on 'is_video && rotation == 90' -d ~/Movies          # portrait phone clips
file-search-on 'is_video && rotation > 0'                         # any non-default rotation
file-search-on 'is_video && rotation == 0 && video_height > video_width'   # genuinely-shot portrait
```

`rotation` is one of 0 / 90 / 180 / 270, decoded from MP4 `tkhd`'s 3×3 display matrix when it's a pure axis-aligned rotation. Non-MP4 containers and arbitrary affine matrices stay at 0.

## HDR and colour-space

Three attributes — `is_hdr`, `color_primaries`, `color_transfer` — decoded from MP4 `colr` (nclx form) and MKV `Colour` (0x55B0):

```sh
file-search-on 'is_video && is_hdr' -d ~/Movies                  # all HDR content
file-search-on 'is_video && color_transfer == "pq"'              # HDR10 / Dolby Vision base layer
file-search-on 'is_video && color_transfer == "hlg"'             # broadcast HDR (BBC iPlayer, NHK, etc.)
file-search-on 'is_video && color_primaries == "bt2020"'         # wide-gamut SDR or HDR
file-search-on 'is_video && color_primaries == "bt709"'          # standard HD
file-search-on 'is_video && color_primaries == "p3"'             # DCI-P3 / Display P3
```

`is_hdr` is true when transfer is PQ (SMPTE ST 2084) or HLG. `color_primaries` and `color_transfer` map H.273 enum values to friendly short names (`bt709`, `bt2020`, `p3`, `pq`, `hlg`); unknown enums fall through to `""`. AVI carries no colour metadata.

## Subtitles

Detect embedded subtitle / closed-caption tracks and their languages:

```sh
file-search-on 'is_video && subtitles' -d ~/Movies                # any sub track present
file-search-on 'is_video && "eng" in subtitle_languages'          # has English subs
file-search-on 'is_video && subtitles &&
                !("eng" in subtitle_languages)'                    # foreign-only — needs translation
file-search-on 'is_video && size(subtitle_languages) > 1'         # multi-language releases
```

Sources: MP4 / MOV `trak` with handler `text` / `subt` / `sbtl` / `clcp`; MKV TrackEntry with `TrackType=17`. Languages are ISO 639-2 codes from MP4 `mdhd` or MKV `Language`; an empty string in the list means the track had no language tag (or was tagged `und`). AVI doesn't carry subtitles in any standard form.

## Format-specific filters

```sh
file-search-on 'is_video && content_type == "video/x-matroska"'   # MKV only
file-search-on 'is_video && content_type == "video/mp4"'          # MP4 / M4V
file-search-on 'is_video && content_type == "video/webm"'         # WebM (Vorbis/VP9 typical)
```

## Combined queries

The "find good archive material" query — high-resolution, modern codec, decent bitrate, longer than a clip:

```sh
file-search-on 'is_video && video_height >= 1080 && (video_codec == "h265" || video_codec == "av1") && bitrate > 4000 && duration > 300'
```

The "find space-wasters" query — old codec, big file, low resolution:

```sh
file-search-on 'is_video && video_codec == "h264" && size > 5000000000 && video_height < 1080'
```

Phone clips (portrait-rotated MP4 from a phone, > 30 fps):

```sh
file-search-on 'is_video && content_type == "video/mp4" && frame_rate >= 30 && video_height > video_width'
```

## Useful output formats

```sh
# A summary table: path, codec, dims, fps, duration in minutes
file-search-on 'is_video' --format '{{.Path}}\t{{.VideoCodec}}\t{{.VideoWidth}}x{{.VideoHeight}}\t{{printf "%.1f" .FrameRate}}fps\t{{printf "%.0f" .Duration}}s'

# Total minutes of HEVC content
file-search-on 'is_video && video_codec == "h265"' -o json | jq -s 'map(.duration) | add / 60'

# All 4K content as a flat list
file-search-on 'is_video && video_height >= 2160' -o bare
```

## Combined queries — film discovery

Find HDR films with English subtitles, modern codec, ≥ 1080p, longer than 30 minutes:

```sh
file-search-on '
  is_video &&
  is_hdr &&
  "eng" in subtitle_languages &&
  (video_codec == "h265" || video_codec == "av1") &&
  video_height >= 1080 &&
  duration > 1800
' -d ~/Movies
```

Multi-language streaming targets (multiple subtitle tracks, ≤ 5 Mbps, AAC audio):

```sh
file-search-on '
  is_video &&
  size(subtitle_languages) > 1 &&
  nominal_bitrate <= 5000 &&
  audio_codec == "aac"
' -d ~/Library
```
