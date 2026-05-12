package main

import (
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
	IsSource   bool `json:"is_source,omitempty"`
	IsNotebook bool `json:"is_notebook,omitempty"`

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

	LOC        int64 `json:"loc,omitempty"`
	CommentLOC int64 `json:"comment_loc,omitempty"`
	BlankLOC   int64 `json:"blank_loc,omitempty"`

	CellCount         int64  `json:"cell_count,omitempty"`
	CodeCellCount     int64  `json:"code_cell_count,omitempty"`
	MarkdownCellCount int64  `json:"markdown_cell_count,omitempty"`
	Kernel            string `json:"kernel,omitempty"`

	// Snippet is the first N lines of the file body, populated when
	// `--snippet` is set and the content type is text-based. Empty
	// otherwise; rendered inline by verbose mode and surfaced
	// directly in json / template output.
	Snippet string `json:"snippet,omitempty"`
}

// recordFrom projects a search.Result into the wire shape. Falls back to
// a path-only record when r.Attrs is nil.
func recordFrom(r search.Result) Record {
	rec := Record{
		Path:        r.Path,
		ContentType: r.ContentType,
		Size:        r.Size,
		Snippet:     r.Snippet,
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
	rec.IsSource = a.IsSource
	rec.IsNotebook = a.IsNotebook

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
	if v, ok := a.Extra["json_kind"].(string); ok {
		rec.JSONKind = v
	}
	if v, ok := a.Extra["img_width"].(int64); ok {
		rec.ImgWidth = v
	}
	if v, ok := a.Extra["img_height"].(int64); ok {
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
	if v, ok := a.Extra["loc"].(int64); ok {
		rec.LOC = v
	}
	if v, ok := a.Extra["comment_loc"].(int64); ok {
		rec.CommentLOC = v
	}
	if v, ok := a.Extra["blank_loc"].(int64); ok {
		rec.BlankLOC = v
	}
	if v, ok := a.Extra["cell_count"].(int64); ok {
		rec.CellCount = v
	}
	if v, ok := a.Extra["code_cell_count"].(int64); ok {
		rec.CodeCellCount = v
	}
	if v, ok := a.Extra["markdown_cell_count"].(int64); ok {
		rec.MarkdownCellCount = v
	}
	if v, ok := a.Extra["kernel"].(string); ok {
		rec.Kernel = v
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
