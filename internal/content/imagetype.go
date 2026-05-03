package content

import (
	"context"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"

	"github.com/evanoberholster/imagemeta"
	"github.com/evanoberholster/imagemeta/exif2"
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

func (i *imageType) Attributes(ctx context.Context, path string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	attrs := Attributes{
		"width":  int64(0),
		"height": int64(0),
	}

	f, err := os.Open(path)
	if err != nil {
		return attrs, nil
	}
	defer func() { _ = f.Close() }()

	// EXIF-bearing types: try imagemeta first. Header-only read; sub-ms.
	if supportsEXIF(i.name) {
		if e, err := imagemeta.Decode(f); err == nil {
			populateEXIF(attrs, e)
		}
		// Reset for the stdlib fallback regardless of imagemeta's outcome.
		_, _ = f.Seek(0, io.SeekStart)
	}

	// stdlib width/height for JPEG/PNG/GIF — fills in if EXIF didn't.
	if attrs["width"] == int64(0) {
		switch i.name {
		case "image/jpeg", "image/png", "image/gif":
			if cfg, _, err := image.DecodeConfig(f); err == nil {
				attrs["width"] = int64(cfg.Width)
				attrs["height"] = int64(cfg.Height)
			}
		}
	}
	return attrs, nil
}

// supportsEXIF reports whether the named image content type is worth feeding
// to imagemeta. JPEG / TIFF / HEIC always carry EXIF; PNG carries it via the
// optional eXIf chunk.
func supportsEXIF(name string) bool {
	switch name {
	case "image/jpeg", "image/tiff", "image/heic", "image/png":
		return true
	}
	return false
}

// populateEXIF copies the curated set of EXIF fields onto attrs. Zero-value
// fields are left out so callers see "absent" rather than "unset" defaults.
func populateEXIF(attrs Attributes, e exif2.Exif) {
	if e.ImageWidth > 0 {
		attrs["width"] = int64(e.ImageWidth)
	}
	if e.ImageHeight > 0 {
		attrs["height"] = int64(e.ImageHeight)
	}
	if e.Make != "" {
		attrs["camera_make"] = e.Make
	}
	if e.Model != "" {
		attrs["camera_model"] = e.Model
	}
	if e.LensModel != "" {
		attrs["lens"] = e.LensModel
	}
	// Prefer DateTimeOriginal (capture time); fall back to CreateDate, then
	// ModifyDate. Real photos usually set all three identically; scanned or
	// edited images may carry only ModifyDate.
	if t := e.DateTimeOriginal(); !t.IsZero() {
		attrs["taken_at"] = t
	} else if t := e.CreateDate(); !t.IsZero() {
		attrs["taken_at"] = t
	} else if t := e.ModifyDate(); !t.IsZero() {
		attrs["taken_at"] = t
	}
	if e.Orientation > 0 {
		attrs["orientation"] = int64(e.Orientation)
	}
	if lat := e.GPS.Latitude(); lat != 0 {
		attrs["gps_lat"] = lat
	}
	if lon := e.GPS.Longitude(); lon != 0 {
		attrs["gps_lon"] = lon
	}
	if e.ISOSpeed > 0 {
		attrs["iso"] = int64(e.ISOSpeed)
	} else if e.ISO > 0 {
		attrs["iso"] = int64(e.ISO)
	}
	if e.FocalLength > 0 {
		attrs["focal_length"] = float64(e.FocalLength)
	}
	if e.FNumber > 0 {
		attrs["f_stop"] = float64(e.FNumber)
	}
	if e.ExposureTime > 0 {
		attrs["exposure_time"] = float64(e.ExposureTime)
	}
}
