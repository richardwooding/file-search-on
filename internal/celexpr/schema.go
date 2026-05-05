package celexpr

// AttributeDoc describes a single CEL attribute available in expressions.
type AttributeDoc struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
}

// FunctionDoc describes a CEL-callable function registered on the
// environment. Unlike attributes, functions have a signature (the formal
// argument list and return type), so the schema needs a richer doc type.
type FunctionDoc struct {
	Name        string `json:"name"`
	Signature   string `json:"signature"`
	Description string `json:"description"`
	Example     string `json:"example,omitempty"`
}

// SchemaDoc groups the documented CEL attributes by category, plus the
// callable built-in functions registered on the environment.
type SchemaDoc struct {
	Common       []AttributeDoc `json:"common"`
	TypeSpecific []AttributeDoc `json:"type_specific"`
	Frontmatter  []AttributeDoc `json:"frontmatter"`
	Functions    []FunctionDoc  `json:"functions"`
}

// Schema returns the structured documentation for every CEL attribute
// and function the evaluator declares. Both the CLI's --list output and
// the MCP list_attributes tool format their output from this.
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
			{"is_video", "bool", "true if video file (MP4, MOV, MKV, WebM, AVI — content_type starts with 'video/')"},
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
			{"duration", "double", "audio duration in seconds (FLAC STREAMINFO / MP3 Xing / OGG granule / MP4 mvhd)"},
			{"bitrate", "int", "audio average bitrate in kbps (computed file_size * 8 / duration / 1000)"},
			{"sample_rate", "int", "audio sample rate in Hz"},
			{"channels", "int", "audio channel count (1 = mono, 2 = stereo, etc.)"},
			{"bit_depth", "int", "audio bits per sample (FLAC STREAMINFO / MP4 stsd sample_size; zero for MP3 / OGG which don't store it)"},
			{"video_codec", "string", "video codec (h264, h265, av1, vp9, vp8, mpeg4, etc.)"},
			{"audio_codec", "string", "audio codec for the audio track in a video container (aac, mp3, opus, vorbis, etc.)"},
			{"video_width", "int", "video frame width in pixels"},
			{"video_height", "int", "video frame height in pixels"},
			{"frame_rate", "double", "video frame rate in fps"},
			{"rotation", "int", "video display rotation in degrees (0 / 90 / 180 / 270) decoded from the MP4 tkhd matrix; 0 for non-rotated, non-MP4 formats, or non-pure-rotation matrices"},
		},
		Frontmatter: []AttributeDoc{
			{"frontmatter", "map", "full parsed front-matter, e.g. frontmatter.category"},
			{"frontmatter_format", "string", "'yaml', 'toml', 'json', or '' if none"},
			{"tags", "list<str>", "front-matter tags (single string is wrapped)"},
			{"categories", "list<str>", "front-matter categories"},
			{"draft", "bool", "front-matter draft flag"},
			{"date", "timestamp", "front-matter date"},
		},
		Functions: []FunctionDoc{
			{
				Name:        "levenshtein",
				Signature:   "levenshtein(string, string) -> int",
				Description: "Edit distance (rune-aware, case-sensitive). Counts insertions, deletions, and substitutions needed to turn the first string into the second.",
				Example:     `is_audio && levenshtein(artist, "Radiohead") <= 2`,
			},
			{
				Name:        "soundex",
				Signature:   "soundex(string) -> string",
				Description: "American Soundex phonetic code (4-character ASCII, e.g. 'Robert' -> 'R163'). Useful for matching name spellings that sound alike.",
				Example:     `is_image && soundex(camera_make) == soundex("Nikon")`,
			},
			{
				Name:        "ngrams",
				Signature:   "ngrams(string, int) -> list<string>",
				Description: "Character-level n-grams of the input (sliding window, length n). Empty list when n <= 0 or n exceeds the rune length of the string.",
				Example:     `ngrams("kubernetes", 3).size() > 5`,
			},
			{
				Name:        "ngram_similarity",
				Signature:   "ngram_similarity(string, string, int) -> double",
				Description: "Jaccard similarity over character n-gram sets, ranging 0.0 (no overlap) to 1.0 (identical sets). Both empty -> 1.0; only one empty -> 0.0.",
				Example:     `is_markdown && ngram_similarity(title, "kubernetes", 2) > 0.6`,
			},
			{
				Name:        "point_in_polygon",
				Signature:   "point_in_polygon(double, double, list<double>) -> bool",
				Description: "Test whether (lat, lon) lies inside an arbitrary polygon. The third argument is a flat list of alternating lat,lon pairs: [lat0, lon0, lat1, lon1, ...]. Polygon need not be explicitly closed; planar ray-casting (good for neighbourhoods / cities / small countries).",
				Example:     `is_image && point_in_polygon(gps_lat, gps_lon, [-34.10, 18.30, -34.10, 18.50, -33.90, 18.50, -33.90, 18.30])`,
			},
		},
	}
}
