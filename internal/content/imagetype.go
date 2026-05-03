package content

import (
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
)

func init() {
	Register(&imageType{name: "image/jpeg", exts: []string{".jpg", ".jpeg"}, magic: [][]byte{{0xFF, 0xD8, 0xFF}}})
	Register(&imageType{name: "image/png", exts: []string{".png"}, magic: [][]byte{{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}}})
	Register(&imageType{name: "image/gif", exts: []string{".gif"}, magic: [][]byte{[]byte("GIF87a"), []byte("GIF89a")}})
	Register(&imageType{name: "image/webp", exts: []string{".webp"}, magic: [][]byte{[]byte("RIFF")}})
	Register(&imageType{name: "image/svg+xml", exts: []string{".svg", ".svgz"}, magic: nil})
	Register(&imageType{name: "image/tiff", exts: []string{".tif", ".tiff"}, magic: [][]byte{{0x49, 0x49, 0x2A, 0x00}, {0x4D, 0x4D, 0x00, 0x2A}}})
	Register(&imageType{name: "image/bmp", exts: []string{".bmp"}, magic: [][]byte{{0x42, 0x4D}}})
}

type imageType struct {
	name  string
	exts  []string
	magic [][]byte
}

func (i *imageType) Name() string        { return i.name }
func (i *imageType) Extensions() []string { return i.exts }
func (i *imageType) MagicBytes() [][]byte { return i.magic }

func (i *imageType) Attributes(path string) (Attributes, error) {
	attrs := Attributes{
		"width":  int64(0),
		"height": int64(0),
	}
	switch i.name {
	case "image/jpeg", "image/png", "image/gif":
		if w, h, err := decodeImageConfig(path); err == nil {
			attrs["width"] = int64(w)
			attrs["height"] = int64(h)
		}
	}
	return attrs, nil
}

func decodeImageConfig(path string) (int, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return 0, 0, err
	}
	return cfg.Width, cfg.Height, nil
}
