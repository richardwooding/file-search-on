# Test fixtures

## `exif-light.jpg`

A 50×50 JPEG with a partial EXIF segment (resolution + DateTime; no camera or
lens info). Used to verify the EXIF dispatch in `imagetype.go` extracts
`taken_at` and confirms the absence of unset fields like `camera_make`.

Origin: [`evanoberholster/imagemeta`](https://github.com/evanoberholster/imagemeta)
`testImages/NoExif.jpg`. MIT licensed (Copyright 2019-2023 Evan Oberholster);
redistributed here under the same terms.
