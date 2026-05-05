# Recipes — Audio

Audio content types: `audio/mpeg` (MP3), `audio/mp4` (M4A/AAC), `audio/flac`, `audio/ogg`. Umbrella boolean `is_audio`.

Tags are extracted via [`dhowden/tag`](https://github.com/dhowden/tag) — handles ID3v1/v2 (MP3), MP4 atoms (M4A), and Vorbis comments (FLAC, OGG) under one API. Playback metadata (duration, bitrate, sample rate, channels) is hand-rolled per format.

## Tags

By artist:

```sh
file-search-on 'is_audio && artist == "Radiohead"' -d ~/Music
```

By album:

```sh
file-search-on 'is_audio && album == "OK Computer"' --format '{{.Track}}\t{{.Title}}'
```

By genre and year:

```sh
file-search-on 'is_audio && genre == "Jazz" && year > 2000' -d ~/Music
file-search-on 'is_audio && genre.contains("Electronic")'        # any genre containing "Electronic"
```

By composer (often used for classical music libraries):

```sh
file-search-on 'is_audio && composer == "J.S. Bach"' -d ~/Music/Classical
```

Find compilation albums (where `album_artist` differs from `artist`):

```sh
file-search-on 'is_audio && album_artist != "" && album_artist != artist'
```

Untagged audio files — useful for triage:

```sh
file-search-on 'is_audio && artist == "" && album == ""'
```

## Duration and bitrate

Long tracks (10+ minutes — typical for jazz, classical, podcasts):

```sh
file-search-on 'is_audio && duration > 600' -d ~/Music
```

Short tracks (skits, intros):

```sh
file-search-on 'is_audio && duration < 60'
```

By bitrate buckets — find low-quality files in a library:

```sh
file-search-on 'is_audio && bitrate > 0 && bitrate < 192' -d ~/Music     # below 192 kbps
file-search-on 'is_audio && bitrate >= 320'                              # 320 kbps+ MP3 / lossless
```

`bitrate` is the computed average (file_size × 8 / duration / 1000). The codec/container-stored value lives in `nominal_bitrate` — useful when the average is skewed by an oversized header / cover art:

```sh
file-search-on 'is_audio && nominal_bitrate >= 256'   # MP3 first-frame ≥ 256 kbps
file-search-on 'is_audio && nominal_bitrate < 128'    # encoder-declared low quality

# Spot CBR-vs-VBR mismatch — average and nominal disagree by more than 20%.
file-search-on 'is_audio && bitrate > 0 && nominal_bitrate > 0 &&
                (bitrate * 100 / nominal_bitrate < 80 ||
                 bitrate * 100 / nominal_bitrate > 120)'
```

`nominal_bitrate` is populated for MP3 (first-frame index), OGG Vorbis (`bitrate_nominal` in the identification header), and M4A (esds Elementary Stream Descriptor — `avgBitrate` preferred, `maxBitrate` fallback). FLAC leaves it 0 because `max_frame_size` doesn't translate cleanly to kbps without frame timing.

## Hi-res audio

Sample rate ≥ 96 kHz indicates hi-res content:

```sh
file-search-on 'is_audio && sample_rate >= 96000' -d ~/Music
file-search-on 'is_audio && sample_rate >= 192000'   # ultra-hi-res, rare outside studio masters
```

Bit depth is the second hi-res signal — 24-bit FLAC / ALAC versus 16-bit CD-quality:

```sh
file-search-on 'is_audio && bit_depth >= 24' -d ~/Music     # hi-res lossless
file-search-on 'is_audio && bit_depth == 16'                # CD-quality

# True hi-res = both ≥ 24-bit AND ≥ 96 kHz.
file-search-on 'is_audio && bit_depth >= 24 && sample_rate >= 96000'
```

`bit_depth` is populated for FLAC (STREAMINFO) and MP4 audio (mp4a sample entry). MP3 and OGG don't store it — `bit_depth` stays 0.

By channel count — surround mixes:

```sh
file-search-on 'is_audio && channels > 2'             # 5.1, 7.1, etc.
file-search-on 'is_audio && channels == 1'            # mono
```

## ReplayGain / loudness

Tracks tagged with ReplayGain carry a per-track and per-album gain in dB. Filter for untagged tracks (where playback software can't normalise volume):

```sh
file-search-on 'is_audio && replaygain_track_gain == 0' -d ~/Music
```

Loud-mastered tracks (very negative track gain — louder than reference, will be turned DOWN by ReplayGain-aware players):

```sh
file-search-on 'is_audio && replaygain_track_gain < -10'
```

Album-tagged vs track-only:

```sh
file-search-on 'is_audio && replaygain_album_gain != 0'        # album-aware libraries
file-search-on 'is_audio && replaygain_track_gain != 0 &&
                replaygain_album_gain == 0'                    # track-only tagging
```

ReplayGain is populated for FLAC and OGG (Vorbis comments) and MP3 (ID3v2 TXXX user-defined-text frames). M4A iTunes-style atoms aren't covered yet (most Apple Music libraries use Sound Check instead).

## Format-specific filters

```sh
file-search-on 'is_audio && content_type == "audio/flac"'        # lossless only
file-search-on 'is_audio && content_type == "audio/mpeg"'        # MP3 only
file-search-on 'is_audio && content_type == "audio/mp4"'         # M4A / AAC only
```

Combined with bitrate to find lossy hi-res nonsense (high bitrate MP3 isn't *truly* hi-res):

```sh
file-search-on 'is_audio && content_type == "audio/mpeg" && bitrate > 320'
```

## Combined queries

A DJ's set-prep query — full-length electronic tracks at 320+:

```sh
file-search-on 'is_audio && genre.contains("Electronic") && duration > 240 && bitrate >= 320'
```

A library audit — tracks tagged but lacking dimensions or duration (broken files):

```sh
file-search-on 'is_audio && artist != "" && duration == 0.0'
```

Find new additions to a specific artist's catalogue:

```sh
file-search-on 'is_audio && artist == "Aphex Twin" && year >= 2020'
```

## Useful output formats

```sh
# Path + artist + album + track, ready for spreadsheet import
file-search-on 'is_audio' --format '{{.Path}}\t{{.Artist}}\t{{.Album}}\t{{.Track}}\t{{.Title}}'

# Just paths to all of a band's catalogue
file-search-on 'is_audio && artist == "Radiohead"' -o bare > radiohead.list

# JSON for analytics — duration histogram
file-search-on 'is_audio' -o json | jq '.duration | floor / 60 | floor' | sort -n | uniq -c
```

## Fuzzy matching for messy tags

```sh
# Catch artist-name typos within 2 edits — useful when tags came from scrapers.
file-search-on 'is_audio && levenshtein(artist, "Radiohead") <= 2'

# Phonetic match — "Smith", "Smyth", "Smithe" all encode to S530.
file-search-on 'is_audio && soundex(artist) == soundex("Smith")'

# Albums similar to a target — Jaccard over n-gram sets.
file-search-on 'is_audio && ngram_similarity(album, "OK Computer", 2) > 0.7'
```

See [`fuzzy-search.md`](./fuzzy-search.md) for the full set of fuzzy / phonetic recipes.
