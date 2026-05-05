package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/template"
	"time"

	"github.com/richardwooding/file-search-on/internal/search"
)

// Record is the JSON-friendly projection of a single search match. It is the
// shape rendered by `-o json` and the data context for `--format` templates.
// All zero-valued optional fields are omitted from JSON output.
type Record struct {
	Path        string `json:"path"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`

	Title    string `json:"title,omitempty"`
	Author   string `json:"author,omitempty"`
	Language string `json:"language,omitempty"`

	WordCount   int64 `json:"word_count,omitempty"   yaml:"word_count,omitempty"`
	LineCount   int64 `json:"line_count,omitempty"`
	PageCount   int64 `json:"page_count,omitempty"`
	ColumnCount int64 `json:"column_count,omitempty"`

	CSVColumns  []string `json:"csv_columns,omitempty"`
	RootElement string   `json:"root_element,omitempty"`
	JSONKind    string   `json:"json_kind,omitempty"`

	ImgWidth  int64 `json:"img_width,omitempty"`
	ImgHeight int64 `json:"img_height,omitempty"`

	CameraMake   string  `json:"camera_make,omitempty"`
	CameraModel  string  `json:"camera_model,omitempty"`
	Lens         string  `json:"lens,omitempty"`
	TakenAt      string  `json:"taken_at,omitempty"` // RFC3339 when set
	Orientation  int64   `json:"orientation,omitempty"`
	GPSLat       float64 `json:"gps_lat,omitempty"`
	GPSLon       float64 `json:"gps_lon,omitempty"`
	ISO          int64   `json:"iso,omitempty"`
	FocalLength  float64 `json:"focal_length,omitempty"`
	FStop        float64 `json:"f_stop,omitempty"`
	ExposureTime float64 `json:"exposure_time,omitempty"`

	FrontmatterFormat string         `json:"frontmatter_format,omitempty"`
	Frontmatter       map[string]any `json:"frontmatter,omitempty"`
	Tags              []string       `json:"tags,omitempty"`
	Categories        []string       `json:"categories,omitempty"`
	Draft             bool           `json:"draft,omitempty"`
	Date              string         `json:"date,omitempty"`

	IsMarkdown bool `json:"is_markdown,omitempty"`
	IsJSON     bool `json:"is_json,omitempty"`
	IsXML      bool `json:"is_xml,omitempty"`
	IsHTML     bool `json:"is_html,omitempty"`
	IsPDF      bool `json:"is_pdf,omitempty"`
	IsImage    bool `json:"is_image,omitempty"`
	IsText     bool `json:"is_text,omitempty"`
	IsCSV      bool `json:"is_csv,omitempty"`
	IsEPUB     bool `json:"is_epub,omitempty"`
	IsOffice   bool `json:"is_office,omitempty"`
	IsAudio    bool `json:"is_audio,omitempty"`
	IsVideo    bool `json:"is_video,omitempty"`
	IsArchive  bool `json:"is_archive,omitempty"`
	IsBinary   bool `json:"is_binary,omitempty"`
	IsEmail    bool `json:"is_email,omitempty"`

	Artist      string `json:"artist,omitempty"`
	Album       string `json:"album,omitempty"`
	AlbumArtist string `json:"album_artist,omitempty"`
	Composer    string `json:"composer,omitempty"`
	Year        int64  `json:"year,omitempty"`
	Track       int64  `json:"track,omitempty"`
	Genre       string `json:"genre,omitempty"`

	Duration       float64 `json:"duration,omitempty"`
	Bitrate        int64   `json:"bitrate,omitempty"`
	NominalBitrate int64   `json:"nominal_bitrate,omitempty"`
	SampleRate     int64   `json:"sample_rate,omitempty"`
	Channels       int64   `json:"channels,omitempty"`
	BitDepth       int64   `json:"bit_depth,omitempty"`

	VideoCodec  string  `json:"video_codec,omitempty"`
	AudioCodec  string  `json:"audio_codec,omitempty"`
	VideoWidth  int64   `json:"video_width,omitempty"`
	VideoHeight int64   `json:"video_height,omitempty"`
	FrameRate   float64 `json:"frame_rate,omitempty"`
	Rotation    int64   `json:"rotation,omitempty"`

	ColorPrimaries string `json:"color_primaries,omitempty"`
	ColorTransfer  string `json:"color_transfer,omitempty"`
	IsHDR          bool   `json:"is_hdr,omitempty"`

	Subtitles         bool     `json:"subtitles,omitempty"`
	SubtitleLanguages []string `json:"subtitle_languages,omitempty"`

	ReplayGainTrackGain float64 `json:"replaygain_track_gain,omitempty"`
	ReplayGainAlbumGain float64 `json:"replaygain_album_gain,omitempty"`

	EntryCount       int64    `json:"entry_count,omitempty"`
	UncompressedSize int64    `json:"uncompressed_size,omitempty"`
	TopLevelEntries  []string `json:"top_level_entries,omitempty"`
	HasRootDir       bool     `json:"has_root_dir,omitempty"`

	Architectures       []string `json:"architectures,omitempty"`
	Bitness             int64    `json:"bitness,omitempty"`
	BinaryFormat        string   `json:"binary_format,omitempty"`
	BinaryType          string   `json:"binary_type,omitempty"`
	IsDynamicallyLinked bool     `json:"is_dynamically_linked,omitempty"`
	IsStripped          bool     `json:"is_stripped,omitempty"`
	EntryPoint          int64    `json:"entry_point,omitempty"`

	EmailTo         []string `json:"email_to,omitempty"`
	EmailCc         []string `json:"email_cc,omitempty"`
	EmailMessageID  string   `json:"email_message_id,omitempty"`
	EmailInReplyTo  string   `json:"email_in_reply_to,omitempty"`
	SentAt          string   `json:"sent_at,omitempty"`
	AttachmentCount int64    `json:"attachment_count,omitempty"`
	EmailCount      int64    `json:"email_count,omitempty"`
}

// recordFrom projects a search.Result into the wire shape. Falls back to
// a path-only record when r.Attrs is nil.
func recordFrom(r search.Result) Record {
	rec := Record{
		Path:        r.Path,
		ContentType: r.ContentType,
		Size:        r.Size,
	}
	if r.ContentType == "" {
		rec.ContentType = "unknown"
	}
	if r.Attrs == nil {
		return rec
	}
	a := r.Attrs
	rec.IsMarkdown = a.IsMarkdown
	rec.IsJSON = a.IsJSON
	rec.IsXML = a.IsXML
	rec.IsHTML = a.IsHTML
	rec.IsPDF = a.IsPDF
	rec.IsImage = a.IsImage
	rec.IsText = a.IsText
	rec.IsCSV = a.IsCSV
	rec.IsEPUB = a.IsEPUB
	rec.IsOffice = a.IsOffice
	rec.IsAudio = a.IsAudio
	rec.IsVideo = a.IsVideo
	rec.IsArchive = a.IsArchive
	rec.IsBinary = a.IsBinary
	rec.IsEmail = a.IsEmail

	if a.Extra == nil {
		return rec
	}
	if v, ok := a.Extra["title"].(string); ok {
		rec.Title = v
	}
	if v, ok := a.Extra["author"].(string); ok {
		rec.Author = v
	}
	if v, ok := a.Extra["language"].(string); ok {
		rec.Language = v
	}
	if v, ok := a.Extra["word_count"].(int64); ok {
		rec.WordCount = v
	}
	if v, ok := a.Extra["line_count"].(int64); ok {
		rec.LineCount = v
	}
	if v, ok := a.Extra["page_count"].(int64); ok {
		rec.PageCount = v
	}
	if v, ok := a.Extra["column_count"].(int64); ok {
		rec.ColumnCount = v
	}
	if v, ok := a.Extra["csv_columns"].([]string); ok {
		rec.CSVColumns = v
	}
	if v, ok := a.Extra["root_element"].(string); ok {
		rec.RootElement = v
	}
	if v, ok := a.Extra["kind"].(string); ok {
		rec.JSONKind = v
	}
	if v, ok := a.Extra["width"].(int64); ok {
		rec.ImgWidth = v
	}
	if v, ok := a.Extra["height"].(int64); ok {
		rec.ImgHeight = v
	}
	if v, ok := a.Extra["camera_make"].(string); ok {
		rec.CameraMake = v
	}
	if v, ok := a.Extra["camera_model"].(string); ok {
		rec.CameraModel = v
	}
	if v, ok := a.Extra["lens"].(string); ok {
		rec.Lens = v
	}
	if v, ok := a.Extra["taken_at"].(time.Time); ok && !v.IsZero() {
		rec.TakenAt = v.Format(time.RFC3339)
	}
	if v, ok := a.Extra["orientation"].(int64); ok {
		rec.Orientation = v
	}
	if v, ok := a.Extra["gps_lat"].(float64); ok {
		rec.GPSLat = v
	}
	if v, ok := a.Extra["gps_lon"].(float64); ok {
		rec.GPSLon = v
	}
	if v, ok := a.Extra["iso"].(int64); ok {
		rec.ISO = v
	}
	if v, ok := a.Extra["focal_length"].(float64); ok {
		rec.FocalLength = v
	}
	if v, ok := a.Extra["f_stop"].(float64); ok {
		rec.FStop = v
	}
	if v, ok := a.Extra["exposure_time"].(float64); ok {
		rec.ExposureTime = v
	}
	if v, ok := a.Extra["artist"].(string); ok {
		rec.Artist = v
	}
	if v, ok := a.Extra["album"].(string); ok {
		rec.Album = v
	}
	if v, ok := a.Extra["album_artist"].(string); ok {
		rec.AlbumArtist = v
	}
	if v, ok := a.Extra["composer"].(string); ok {
		rec.Composer = v
	}
	if v, ok := a.Extra["year"].(int64); ok {
		rec.Year = v
	}
	if v, ok := a.Extra["track"].(int64); ok {
		rec.Track = v
	}
	if v, ok := a.Extra["genre"].(string); ok {
		rec.Genre = v
	}
	if v, ok := a.Extra["duration"].(float64); ok {
		rec.Duration = v
	}
	if v, ok := a.Extra["bitrate"].(int64); ok {
		rec.Bitrate = v
	}
	if v, ok := a.Extra["sample_rate"].(int64); ok {
		rec.SampleRate = v
	}
	if v, ok := a.Extra["channels"].(int64); ok {
		rec.Channels = v
	}
	if v, ok := a.Extra["bit_depth"].(int64); ok {
		rec.BitDepth = v
	}
	if v, ok := a.Extra["nominal_bitrate"].(int64); ok {
		rec.NominalBitrate = v
	}
	if v, ok := a.Extra["video_codec"].(string); ok {
		rec.VideoCodec = v
	}
	if v, ok := a.Extra["audio_codec"].(string); ok {
		rec.AudioCodec = v
	}
	if v, ok := a.Extra["video_width"].(int64); ok {
		rec.VideoWidth = v
	}
	if v, ok := a.Extra["video_height"].(int64); ok {
		rec.VideoHeight = v
	}
	if v, ok := a.Extra["frame_rate"].(float64); ok {
		rec.FrameRate = v
	}
	if v, ok := a.Extra["rotation"].(int64); ok {
		rec.Rotation = v
	}
	if v, ok := a.Extra["color_primaries"].(string); ok {
		rec.ColorPrimaries = v
	}
	if v, ok := a.Extra["color_transfer"].(string); ok {
		rec.ColorTransfer = v
	}
	if v, ok := a.Extra["is_hdr"].(bool); ok {
		rec.IsHDR = v
	}
	if v, ok := a.Extra["subtitles"].(bool); ok {
		rec.Subtitles = v
	}
	if v, ok := a.Extra["subtitle_languages"].([]string); ok && len(v) > 0 {
		rec.SubtitleLanguages = v
	}
	if v, ok := a.Extra["replaygain_track_gain"].(float64); ok {
		rec.ReplayGainTrackGain = v
	}
	if v, ok := a.Extra["replaygain_album_gain"].(float64); ok {
		rec.ReplayGainAlbumGain = v
	}
	if v, ok := a.Extra["entry_count"].(int64); ok {
		rec.EntryCount = v
	}
	if v, ok := a.Extra["uncompressed_size"].(int64); ok {
		rec.UncompressedSize = v
	}
	if v, ok := a.Extra["top_level_entries"].([]string); ok && len(v) > 0 {
		rec.TopLevelEntries = v
	}
	if v, ok := a.Extra["has_root_dir"].(bool); ok {
		rec.HasRootDir = v
	}
	if v, ok := a.Extra["architectures"].([]string); ok && len(v) > 0 {
		rec.Architectures = v
	}
	if v, ok := a.Extra["bitness"].(int64); ok {
		rec.Bitness = v
	}
	if v, ok := a.Extra["binary_format"].(string); ok {
		rec.BinaryFormat = v
	}
	if v, ok := a.Extra["binary_type"].(string); ok {
		rec.BinaryType = v
	}
	if v, ok := a.Extra["is_dynamically_linked"].(bool); ok {
		rec.IsDynamicallyLinked = v
	}
	if v, ok := a.Extra["is_stripped"].(bool); ok {
		rec.IsStripped = v
	}
	if v, ok := a.Extra["entry_point"].(int64); ok {
		rec.EntryPoint = v
	}
	if v, ok := a.Extra["email_to"].([]string); ok && len(v) > 0 {
		rec.EmailTo = v
	}
	if v, ok := a.Extra["email_cc"].([]string); ok && len(v) > 0 {
		rec.EmailCc = v
	}
	if v, ok := a.Extra["email_message_id"].(string); ok {
		rec.EmailMessageID = v
	}
	if v, ok := a.Extra["email_in_reply_to"].(string); ok {
		rec.EmailInReplyTo = v
	}
	if v, ok := a.Extra["sent_at"].(time.Time); ok && !v.IsZero() {
		rec.SentAt = v.Format(time.RFC3339)
	}
	if v, ok := a.Extra["attachment_count"].(int64); ok {
		rec.AttachmentCount = v
	}
	if v, ok := a.Extra["email_count"].(int64); ok {
		rec.EmailCount = v
	}
	if v, ok := a.Extra["frontmatter_format"].(string); ok {
		rec.FrontmatterFormat = v
	}
	if v, ok := a.Extra["frontmatter"].(map[string]any); ok && len(v) > 0 {
		rec.Frontmatter = v
	}
	if v, ok := a.Extra["tags"].([]string); ok && len(v) > 0 {
		rec.Tags = v
	}
	if v, ok := a.Extra["categories"].([]string); ok && len(v) > 0 {
		rec.Categories = v
	}
	if v, ok := a.Extra["draft"].(bool); ok {
		rec.Draft = v
	}
	if v, ok := a.Extra["date"].(time.Time); ok && !v.IsZero() {
		rec.Date = v.Format(time.RFC3339)
	}
	return rec
}

// fp / fpn wrap fmt.Fprintf / Fprintln and discard the write error. Printers
// always send to stdout (or a *bytes.Buffer in tests) where checking each
// write is noise — failure surfaces at the next call, or via the test's own
// assertions.
func fp(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}

func fpn(w io.Writer, args ...any) {
	_, _ = fmt.Fprintln(w, args...)
}

// printBare writes one path per line.
func printBare(w io.Writer, results []search.Result) {
	for _, r := range results {
		fpn(w, r.Path)
	}
}

// printDefault writes the historical "<path>\t[<content-type>]\t<size> bytes" format.
func printDefault(w io.Writer, results []search.Result) {
	for _, r := range results {
		ct := r.ContentType
		if ct == "" {
			ct = "unknown"
		}
		fp(w, "%s\t[%s]\t%d bytes\n", r.Path, ct, r.Size)
	}
}

// printVerbose writes a multi-line indented record per match. Every
// populated attribute is surfaced — common fields first (path, type,
// size), then per-family blocks (markup, EXIF, audio tags, audio
// playback, video, frontmatter). Zero-valued fields are skipped so the
// output stays compact for files that only carry a subset.
func printVerbose(w io.Writer, results []search.Result) {
	for i, r := range results {
		if i > 0 {
			fpn(w)
		}
		rec := recordFrom(r)
		fpn(w, rec.Path)
		fp(w, "  content_type   %s\n", rec.ContentType)
		fp(w, "  size           %s bytes\n", commafy(rec.Size))

		// Common metadata.
		printIfStr(w, "title", rec.Title)
		printIfStr(w, "author", rec.Author)
		printIfStr(w, "language", rec.Language)

		// Markup / text / data shape.
		printIfInt(w, "word_count", rec.WordCount)
		printIfInt(w, "line_count", rec.LineCount)
		printIfInt(w, "page_count", rec.PageCount)
		printIfInt(w, "column_count", rec.ColumnCount)
		printIfStr(w, "root_element", rec.RootElement)
		printIfStr(w, "json_kind", rec.JSONKind)

		// Image dimensions + EXIF.
		printIfInt(w, "img_width", rec.ImgWidth)
		printIfInt(w, "img_height", rec.ImgHeight)
		printIfStr(w, "camera_make", rec.CameraMake)
		printIfStr(w, "camera_model", rec.CameraModel)
		printIfStr(w, "lens", rec.Lens)
		printIfStr(w, "taken_at", rec.TakenAt)
		printIfInt(w, "orientation", rec.Orientation)
		printIfFloat(w, "gps_lat", rec.GPSLat)
		printIfFloat(w, "gps_lon", rec.GPSLon)
		printIfInt(w, "iso", rec.ISO)
		printIfFloat(w, "focal_length", rec.FocalLength)
		printIfFloat(w, "f_stop", rec.FStop)
		printIfFloat(w, "exposure_time", rec.ExposureTime)

		// Audio tags + playback.
		printIfStr(w, "artist", rec.Artist)
		printIfStr(w, "album", rec.Album)
		printIfStr(w, "album_artist", rec.AlbumArtist)
		printIfStr(w, "composer", rec.Composer)
		printIfStr(w, "genre", rec.Genre)
		printIfInt(w, "year", rec.Year)
		printIfInt(w, "track", rec.Track)
		printIfFloat(w, "duration", rec.Duration)
		printIfInt(w, "bitrate", rec.Bitrate)
		printIfInt(w, "nominal_bitrate", rec.NominalBitrate)
		printIfInt(w, "sample_rate", rec.SampleRate)
		printIfInt(w, "channels", rec.Channels)
		printIfInt(w, "bit_depth", rec.BitDepth)
		printIfFloat(w, "rg_track_gain", rec.ReplayGainTrackGain)
		printIfFloat(w, "rg_album_gain", rec.ReplayGainAlbumGain)

		// Video.
		printIfStr(w, "video_codec", rec.VideoCodec)
		printIfStr(w, "audio_codec", rec.AudioCodec)
		printIfInt(w, "video_width", rec.VideoWidth)
		printIfInt(w, "video_height", rec.VideoHeight)
		printIfFloat(w, "frame_rate", rec.FrameRate)
		printIfInt(w, "rotation", rec.Rotation)
		printIfStr(w, "color_primaries", rec.ColorPrimaries)
		printIfStr(w, "color_transfer", rec.ColorTransfer)
		if rec.IsHDR {
			fp(w, "  %-13s %v\n", "is_hdr", true)
		}
		if rec.Subtitles {
			fp(w, "  %-13s %v\n", "subtitles", true)
		}
		if len(rec.SubtitleLanguages) > 0 {
			fp(w, "  %-13s %s\n", "sub_langs", strings.Join(rec.SubtitleLanguages, ", "))
		}

		// Archive metadata.
		printIfInt(w, "entry_count", rec.EntryCount)
		printIfInt(w, "uncomp_size", rec.UncompressedSize)
		if len(rec.TopLevelEntries) > 0 {
			fp(w, "  %-13s %s\n", "top_entries", strings.Join(rec.TopLevelEntries, ", "))
		}
		if rec.HasRootDir {
			fp(w, "  %-13s %v\n", "has_root_dir", true)
		}

		// Email metadata.
		if len(rec.EmailTo) > 0 {
			fp(w, "  %-13s %s\n", "to", strings.Join(rec.EmailTo, ", "))
		}
		if len(rec.EmailCc) > 0 {
			fp(w, "  %-13s %s\n", "cc", strings.Join(rec.EmailCc, ", "))
		}
		printIfStr(w, "msg_id", rec.EmailMessageID)
		printIfStr(w, "in_reply_to", rec.EmailInReplyTo)
		printIfStr(w, "sent_at", rec.SentAt)
		printIfInt(w, "attachments", rec.AttachmentCount)
		printIfInt(w, "email_count", rec.EmailCount)

		// Binary metadata.
		if len(rec.Architectures) > 0 {
			fp(w, "  %-13s %s\n", "archs", strings.Join(rec.Architectures, ", "))
		}
		printIfInt(w, "bitness", rec.Bitness)
		printIfStr(w, "bin_format", rec.BinaryFormat)
		printIfStr(w, "bin_type", rec.BinaryType)
		if rec.IsDynamicallyLinked {
			fp(w, "  %-13s %v\n", "dynamic_link", true)
		}
		if rec.IsStripped {
			fp(w, "  %-13s %v\n", "stripped", true)
		}
		printIfInt(w, "entry_point", rec.EntryPoint)

		// Frontmatter shape + lists + date.
		if rec.FrontmatterFormat != "" {
			fp(w, "  %-13s %s (%d keys)\n", "frontmatter", rec.FrontmatterFormat, len(rec.Frontmatter))
		}
		if len(rec.Tags) > 0 {
			fp(w, "  %-13s %s\n", "tags", strings.Join(rec.Tags, ", "))
		}
		if len(rec.Categories) > 0 {
			fp(w, "  %-13s %s\n", "categories", strings.Join(rec.Categories, ", "))
		}
		if len(rec.CSVColumns) > 0 {
			fp(w, "  %-13s %s\n", "csv_columns", strings.Join(rec.CSVColumns, ", "))
		}
		printIfStr(w, "date", rec.Date)
	}
}

func printIfStr(w io.Writer, label, v string) {
	if v != "" {
		fp(w, "  %-13s %s\n", label, v)
	}
}

func printIfInt(w io.Writer, label string, v int64) {
	if v != 0 {
		fp(w, "  %-13s %s\n", label, commafy(v))
	}
}

func printIfFloat(w io.Writer, label string, v float64) {
	if v != 0 {
		fp(w, "  %-13s %g\n", label, v)
	}
}

// printJSON writes one JSON object per line (NDJSON / JSON Lines).
func printJSON(w io.Writer, results []search.Result) error {
	enc := json.NewEncoder(w)
	for _, r := range results {
		if err := enc.Encode(recordFrom(r)); err != nil {
			return fmt.Errorf("json encode %s: %w", r.Path, err)
		}
	}
	return nil
}

// printTemplate renders each match through a parsed text/template against a Record.
func printTemplate(w io.Writer, results []search.Result, tmpl *template.Template) error {
	for _, r := range results {
		if err := tmpl.Execute(w, recordFrom(r)); err != nil {
			return fmt.Errorf("template execute %s: %w", r.Path, err)
		}
		fpn(w)
	}
	return nil
}

// parseFormatTemplate parses a user-supplied template, translating `\t` and
// `\n` escapes so shell users can write tab-separated formats from the CLI.
func parseFormatTemplate(s string) (*template.Template, error) {
	expanded := strings.NewReplacer(`\t`, "\t", `\n`, "\n", `\r`, "\r").Replace(s)
	return template.New("format").Parse(expanded)
}

// commafy formats an int64 with thousands separators.
func commafy(n int64) string {
	if n < 0 {
		return "-" + commafy(-n)
	}
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	pre := len(s) % 3
	if pre > 0 {
		b.WriteString(s[:pre])
		if len(s) > pre {
			b.WriteByte(',')
		}
	}
	for i := pre; i < len(s); i += 3 {
		b.WriteString(s[i : i+3])
		if i+3 < len(s) {
			b.WriteByte(',')
		}
	}
	return b.String()
}

