package celexpr

// AttributeDoc describes a single CEL attribute available in expressions.
type AttributeDoc struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
}

// SchemaDoc groups the documented CEL attributes by category.
type SchemaDoc struct {
	Common       []AttributeDoc `json:"common"`
	TypeSpecific []AttributeDoc `json:"type_specific"`
	Frontmatter  []AttributeDoc `json:"frontmatter"`
}

// Schema returns the structured documentation for every CEL attribute
// the evaluator declares. Both the CLI's --list output and the MCP
// list_attributes tool format their output from this.
func Schema() SchemaDoc {
	return SchemaDoc{
		Common: []AttributeDoc{
			{"name", "string", "filename"},
			{"path", "string", "full path"},
			{"dir", "string", "parent directory"},
			{"size", "int", "file size in bytes"},
			{"ext", "string", "file extension (e.g. '.md')"},
			{"content_type", "string", "detected content type"},
			{"is_markdown", "bool", "true if markdown file"},
			{"is_json", "bool", "true if JSON file"},
			{"is_xml", "bool", "true if XML file"},
			{"is_html", "bool", "true if HTML file"},
			{"is_pdf", "bool", "true if PDF file"},
			{"is_image", "bool", "true if image file"},
			{"is_text", "bool", "true if plain-text file (.txt, .text, .log)"},
			{"is_csv", "bool", "true if CSV/TSV file"},
			{"is_epub", "bool", "true if EPUB book"},
			{"is_office", "bool", "true if office document (DOCX, XLSX, PPTX, ODT — content_type starts with 'office/')"},
			{"is_audio", "bool", "true if audio file (MP3, M4A, FLAC, OGG — content_type starts with 'audio/')"},
		},
		TypeSpecific: []AttributeDoc{
			{"title", "string", "title (front-matter, markdown h1, HTML title, PDF title, EPUB, office, audio)"},
			{"word_count", "int", "word count (markdown body, plain text)"},
			{"line_count", "int", "line count (plain text)"},
			{"column_count", "int", "column count from header line (CSV/TSV)"},
			{"csv_columns", "list<str>", "header field names from the first CSV/TSV line"},
			{"page_count", "int", "page count (PDF)"},
			{"author", "string", "author (markdown front-matter, PDF, EPUB, office)"},
			{"language", "string", "language code (EPUB, HTML <html lang>, markdown front-matter, PDF /Lang or XMP, office)"},
			{"root_element", "string", "root element name (XML)"},
			{"json_kind", "string", "'object' or 'array' (JSON)"},
			{"img_width", "int", "image width in pixels"},
			{"img_height", "int", "image height in pixels"},
			{"camera_make", "string", "EXIF camera make (JPEG/TIFF/HEIC/PNG)"},
			{"camera_model", "string", "EXIF camera model"},
			{"lens", "string", "EXIF lens model"},
			{"taken_at", "timestamp", "EXIF DateTimeOriginal — image capture time"},
			{"orientation", "int", "EXIF orientation tag (1-8)"},
			{"gps_lat", "double", "GPS latitude in decimal degrees (north positive)"},
			{"gps_lon", "double", "GPS longitude in decimal degrees (east positive)"},
			{"iso", "int", "EXIF ISO sensitivity"},
			{"focal_length", "double", "EXIF focal length in mm"},
			{"f_stop", "double", "EXIF F-number (aperture)"},
			{"exposure_time", "double", "EXIF exposure time in seconds"},
			{"artist", "string", "audio artist tag (ID3v2 TPE1 / Vorbis ARTIST / iTunes ©ART)"},
			{"album", "string", "audio album tag"},
			{"album_artist", "string", "audio album-artist tag (compilations)"},
			{"composer", "string", "audio composer tag"},
			{"year", "int", "audio release year"},
			{"track", "int", "audio track number"},
			{"genre", "string", "audio genre tag"},
		},
		Frontmatter: []AttributeDoc{
			{"frontmatter", "map", "full parsed front-matter, e.g. frontmatter.category"},
			{"frontmatter_format", "string", "'yaml', 'toml', 'json', or '' if none"},
			{"tags", "list<str>", "front-matter tags (single string is wrapped)"},
			{"categories", "list<str>", "front-matter categories"},
			{"draft", "bool", "front-matter draft flag"},
			{"date", "timestamp", "front-matter date"},
		},
	}
}
