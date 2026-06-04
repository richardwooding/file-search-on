package content

import (
	"bytes"
	"context"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"io/fs"
	"strings"

	"github.com/evanoberholster/imagemeta"
	"github.com/evanoberholster/imagemeta/meta/exif"
)

func init() {
	Register(&imageType{name: "image/jpeg", exts: []string{".jpg", ".jpeg"}, magic: [][]byte{{0xFF, 0xD8, 0xFF}}})
	Register(&imageType{name: "image/png", exts: []string{".png"}, magic: [][]byte{{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}}})
	Register(&imageType{name: "image/gif", exts: []string{".gif"}, magic: [][]byte{[]byte("GIF87a"), []byte("GIF89a")}})
	Register(&imageType{name: "image/webp", exts: []string{".webp"}, magic: [][]byte{[]byte("RIFF")}})
	Register(&imageType{name: "image/svg+xml", exts: []string{".svg", ".svgz"}, magic: nil})
	Register(&imageType{name: "image/tiff", exts: []string{".tif", ".tiff"}, magic: [][]byte{{0x49, 0x49, 0x2A, 0x00}, {0x4D, 0x4D, 0x00, 0x2A}}})
	Register(&imageType{name: "image/bmp", exts: []string{".bmp"}, magic: [][]byte{{0x42, 0x4D}}})
	// HEIC/HEIF — extension-only detection. The MP4-style ftyp magic is at
	// offset 4 and the registry's HasPrefix check would miss it; iPhone /
	// modern camera output uses .heic/.heif consistently.
	Register(&imageType{name: "image/heic", exts: []string{".heic", ".heif"}, magic: nil})
}

type imageType struct {
	name  string
	exts  []string
	magic [][]byte
}

func (i *imageType) Name() string         { return i.name }
func (i *imageType) Extensions() []string { return i.exts }
func (i *imageType) MagicBytes() [][]byte { return i.magic }

// MatchMagic disambiguates WebP from the other RIFF-container formats
// (WAV audio, AVI video) that share the bare "RIFF" prefix: a real WebP
// carries "WEBP" at bytes 8..11. Without this, any RIFF file (e.g. a
// .wav) would magic-match image/webp (issue #322). Non-WebP image types
// fall back to the standard prefix match.
func (i *imageType) MatchMagic(head []byte) bool {
	if i.name == "image/webp" {
		return len(head) >= 12 && string(head[0:4]) == "RIFF" && string(head[8:12]) == "WEBP"
	}
	for _, m := range i.magic {
		if bytes.HasPrefix(head, m) {
			return true
		}
	}
	return false
}

func (i *imageType) Attributes(ctx context.Context, fsys fs.FS, path string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	attrs := Attributes{
		"img_width":  int64(0),
		"img_height": int64(0),
	}

	rs, _, closer, err := openReadSeeker(fsys, path)
	if err != nil {
		return attrs, nil
	}
	defer func() { _ = closer() }()

	// EXIF-bearing types: try imagemeta first. Header-only read; sub-ms.
	if supportsEXIF(i.name) {
		extractImageEXIF(rs, attrs)
		// Reset for the stdlib fallback regardless of imagemeta's outcome.
		_, _ = rs.Seek(0, io.SeekStart)
	}

	// stdlib width/height for JPEG/PNG/GIF — fills in if EXIF didn't.
	if attrs["img_width"] == int64(0) {
		switch i.name {
		case "image/jpeg", "image/png", "image/gif":
			if cfg, _, err := image.DecodeConfig(rs); err == nil {
				attrs["img_width"] = int64(cfg.Width)
				attrs["img_height"] = int64(cfg.Height)
			}
		}
	}
	return attrs, nil
}

// supportsEXIF reports whether the named image content type is worth feeding
// to imagemeta. JPEG / TIFF / HEIC always carry EXIF; PNG carries it via the
// optional eXIf chunk. RAW photo formats (`image/raw-*`) are TIFF-based or
// otherwise have embedded EXIF that imagemeta handles natively (#196 follow-up).
func supportsEXIF(name string) bool {
	switch name {
	case "image/jpeg", "image/tiff", "image/heic", "image/png":
		return true
	}
	return strings.HasPrefix(name, "image/raw-")
}

// extractImageEXIF runs imagemeta against rs and copies the curated EXIF
// fields onto attrs. Shared by imageType (JPEG / PNG / TIFF / HEIC) and
// rawImageType (CR2 / NEF / ARW / DNG / etc.) — both go through the same
// imagemeta path. Caller owns the Seeker reset; this function only reads.
func extractImageEXIF(rs io.ReadSeeker, attrs Attributes) {
	if e, err := imagemeta.Decode(rs); err == nil {
		populateEXIF(attrs, e)
	}
}

// populateEXIF copies the curated set of EXIF fields onto attrs. Zero-value
// fields are left out so callers see "absent" rather than "unset" defaults.
//
// imagemeta v1.0.0 splits the flat exif2.Exif into nested IFD0 / ExifIFD / GPS
// sub-structs (see meta/exif/model.go); the field mapping below preserves the
// v0.3.1 attribute set against the new shape.
func populateEXIF(attrs Attributes, e exif.Exif) {
	if e.IFD0.ImageWidth > 0 {
		attrs["img_width"] = int64(e.IFD0.ImageWidth)
	}
	if e.IFD0.ImageHeight > 0 {
		attrs["img_height"] = int64(e.IFD0.ImageHeight)
	}
	if e.IFD0.Make != "" {
		attrs["camera_make"] = e.IFD0.Make
	}
	if e.IFD0.Model != "" {
		attrs["camera_model"] = e.IFD0.Model
	}
	if e.ExifIFD.LensModel != "" {
		attrs["lens"] = e.ExifIFD.LensModel
	}
	// SelectedDate prefers DateTimeOriginal, then CreateDate, then ModifyDate
	// — same precedence as the v0.3.1 manual fallback.
	if t := e.SelectedDate(); !t.IsZero() {
		attrs["taken_at"] = t
	}
	if e.IFD0.Orientation > 0 {
		attrs["orientation"] = int64(e.IFD0.Orientation)
	}
	if lat := e.GPS.Latitude(); lat != 0 {
		attrs["gps_lat"] = lat
	}
	if lon := e.GPS.Longitude(); lon != 0 {
		attrs["gps_lon"] = lon
	}
	if e.ExifIFD.ISOSpeedRatings > 0 {
		attrs["iso"] = int64(e.ExifIFD.ISOSpeedRatings)
	}
	if e.ExifIFD.FocalLength > 0 {
		attrs["focal_length"] = float64(e.ExifIFD.FocalLength)
	}
	if e.ExifIFD.FNumber > 0 {
		attrs["f_stop"] = float64(e.ExifIFD.FNumber)
	}
	if e.ExifIFD.ExposureTime > 0 {
		attrs["exposure_time"] = float64(e.ExifIFD.ExposureTime)
	}
}
