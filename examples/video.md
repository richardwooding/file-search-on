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

## Known limitations

- **Aspect-ratio rotation**: phone-recorded clips often store portrait video as 1920×1080 with a 90° rotation matrix in `tkhd`. We currently report the stored dimensions, not the displayed dimensions. Tracked in [#36](https://github.com/richardwooding/file-search-on/issues/36).
- **HDR**: `colr` (MP4) and `Colour` (MKV) elements aren't parsed; `is_hdr` is a follow-up. Tracked in [#38](https://github.com/richardwooding/file-search-on/issues/38).
- **Subtitle tracks**: presence and language not surfaced. Tracked in [#39](https://github.com/richardwooding/file-search-on/issues/39).
- **`audio_codec` is video-only** in the current build — for the audio track inside a video container. Audio sample_rate / channels for video files are tracked in [#37](https://github.com/richardwooding/file-search-on/issues/37).
