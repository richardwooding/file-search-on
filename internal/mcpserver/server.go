// Package mcpserver exposes file-search-on as a Model Context Protocol server.
package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/richardwooding/file-search-on/internal/celexpr"
	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/index"
	"github.com/richardwooding/file-search-on/internal/search"
)

// ReadAttributesInput is the JSON-schema input for the `read_attributes`
// tool. Path can be absolute or relative to the server's working
// directory; agents should prefer absolute paths.
type ReadAttributesInput struct {
	Path string `json:"path" jsonschema:"Filesystem path of a single file to extract attributes from. Absolute paths are preferred; relative paths resolve against the server's working directory."`
}

// SearchInput is the JSON-schema input for the `search` tool.
type SearchInput struct {
	Expr             string   `json:"expr,omitempty" jsonschema:"CEL expression matched against file attributes. Boolean type predicates: is_markdown, is_pdf, is_html, is_xml, is_json, is_csv, is_text, is_image, is_audio, is_video, is_office, is_epub, is_archive, is_binary, is_email, is_source. Common attributes: size (int, bytes), name/path/dir/ext (string), word_count/line_count/page_count (int), title/author/language (string). Examples: 'is_markdown && word_count > 500'; 'is_pdf && page_count > 10'; 'is_image && iso > 1600'; 'is_audio && sample_rate >= 96000'; 'is_video && duration > 1800'; 'is_source && language == \"go\" && loc > 100'; 'size > 1000000 && !is_binary'. Empty means match all. Call list_attributes for the full schema."`
	Dir              string   `json:"dir,omitempty" jsonschema:"Directory to search in. Defaults to '.'. Ignored when 'dirs' is non-empty."`
	Dirs             []string `json:"dirs,omitempty" jsonschema:"Multiple directories to search in one call. When non-empty, takes precedence over 'dir'. Each root's .gitignore is honoured independently when respect_gitignore is set."`
	Workers          int      `json:"workers,omitempty" jsonschema:"Number of parallel workers. Defaults to runtime.NumCPU()."`
	MaxLineBytes     int      `json:"max_line_bytes,omitempty" jsonschema:"Per-line scanner buffer cap for text/CSV/HTML (bytes). 0 uses the 1 MiB default; raise for very long log lines."`
	TimeoutSeconds   *float64 `json:"timeout_seconds,omitempty" jsonschema:"Override the server's default per-call timeout for this invocation (in seconds; fractions allowed). Omit to use the server default (set when the MCP server was started). Pass 0 to disable the timeout for this call. On expiry the walk is cancelled and the partial result set is returned with cancelled=true."`
	SortBy           string   `json:"sort_by,omitempty" jsonschema:"Sort matches by attribute. Recognised keys: size, name, path, mod_time, word_count, line_count, page_count, duration, bitrate, sample_rate, video_height, video_width, frame_rate, iso, focal_length, taken_at, sent_at, year, entry_count, uncompressed_size, loc, attachment_count, email_count. Files missing the attribute group at the end. Sorting buffers the full result set (top-K is incoherent with streaming)."`
	Order            string   `json:"order,omitempty" jsonschema:"Sort direction when sort_by is set: 'asc' (default) or 'desc'."`
	Limit            int      `json:"limit,omitempty" jsonschema:"Cap the returned match count. With sort_by, returns the top-N (after sorting). Without sort_by, returns the first N in walk order. 0 = unlimited."`
	IncludeSnippet   bool     `json:"include_snippet,omitempty" jsonschema:"When true, populate each match's 'snippet' field with the first N lines of the file body (see snippet_lines). Only text-based content types (markdown / text / html / csv / json / xml / source/*) populate; binary families leave snippet empty."`
	SnippetLines     int      `json:"snippet_lines,omitempty" jsonschema:"How many lines to include per snippet (default 10). Ignored when include_snippet is false."`
	IncludeBody      bool     `json:"include_body,omitempty" jsonschema:"When true, the full file body is exposed to the CEL expression as the 'body' string variable, so filters like body.contains(\"transformer\") or body.matches(\"\\\\bAPI\\\\b\") run at search time. Only text-based content types populate body; capped at body_max_bytes (default 1 MiB). Expensive: reads every candidate file's body, not just headers — pair with tight expr / excludes / timeout."`
	BodyMaxBytes     int      `json:"body_max_bytes,omitempty" jsonschema:"Cap on the body string in bytes (default 1 MiB). Files larger than the cap are silently truncated; the prefix still participates in body.contains / body.matches. Ignored when include_body is false."`
	Excludes         []string `json:"excludes,omitempty" jsonschema:"Glob patterns matched against the basename of each file/directory; matched directories are pruned. Example: ['node_modules', '.git', 'target', '*.bak']. Use respect_gitignore for path-aware patterns."`
	RespectGitignore bool     `json:"respect_gitignore,omitempty" jsonschema:"When true, parse a .gitignore at the walk root (if present) and skip matching paths. Honours standard gitignore semantics. Nested .gitignore files in subdirectories are NOT honoured in this version."`
}

// SearchMatch is one match returned by the `search` tool. Beyond path /
// content_type / size, every CEL-visible attribute is included when the
// matched content type emits it; absent fields are omitted from the JSON
// payload so simple consumers see a compact shape.
type SearchMatch struct {
	Path        string `json:"path"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`

	Title    string `json:"title,omitempty"`
	Author   string `json:"author,omitempty"`
	Language string `json:"language,omitempty"`

	WordCount   int64 `json:"word_count,omitempty"`
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
	Date              string         `json:"date,omitempty"` // RFC3339 when set

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
	SentAt          string   `json:"sent_at,omitempty"` // RFC3339 when set
	AttachmentCount int64    `json:"attachment_count,omitempty"`
	EmailCount      int64    `json:"email_count,omitempty"`

	LOC        int64 `json:"loc,omitempty"`
	CommentLOC int64 `json:"comment_loc,omitempty"`
	BlankLOC   int64 `json:"blank_loc,omitempty"`

	CellCount         int64  `json:"cell_count,omitempty"`
	CodeCellCount     int64  `json:"code_cell_count,omitempty"`
	MarkdownCellCount int64  `json:"markdown_cell_count,omitempty"`
	Kernel            string `json:"kernel,omitempty"`

	// Snippet is the first N lines of the file body when the search
	// call had include_snippet=true and the content type is
	// text-based. Empty otherwise. Lets an agent decide whether a
	// match is relevant without a separate read_attributes round trip.
	Snippet string `json:"snippet,omitempty"`
}

// matchFrom projects a search.Result (with Attrs populated) into a
// SearchMatch wire object.
func matchFrom(r search.Result) SearchMatch {
	m := SearchMatch{
		Path:        r.Path,
		ContentType: r.ContentType,
		Size:        r.Size,
		Snippet:     r.Snippet,
	}
	if r.Attrs == nil {
		return m
	}
	a := r.Attrs
	m.IsMarkdown, m.IsJSON, m.IsXML, m.IsHTML = a.IsMarkdown, a.IsJSON, a.IsXML, a.IsHTML
	m.IsPDF, m.IsImage = a.IsPDF, a.IsImage
	m.IsText, m.IsCSV, m.IsEPUB, m.IsOffice = a.IsText, a.IsCSV, a.IsEPUB, a.IsOffice
	m.IsAudio = a.IsAudio
	m.IsVideo = a.IsVideo
	m.IsArchive = a.IsArchive
	m.IsBinary = a.IsBinary
	m.IsEmail = a.IsEmail
	m.IsSource = a.IsSource
	m.IsNotebook = a.IsNotebook

	if a.Extra == nil {
		return m
	}
	if v, ok := a.Extra["title"].(string); ok {
		m.Title = v
	}
	if v, ok := a.Extra["author"].(string); ok {
		m.Author = v
	}
	if v, ok := a.Extra["language"].(string); ok {
		m.Language = v
	}
	if v, ok := a.Extra["word_count"].(int64); ok {
		m.WordCount = v
	}
	if v, ok := a.Extra["line_count"].(int64); ok {
		m.LineCount = v
	}
	if v, ok := a.Extra["page_count"].(int64); ok {
		m.PageCount = v
	}
	if v, ok := a.Extra["column_count"].(int64); ok {
		m.ColumnCount = v
	}
	if v, ok := a.Extra["csv_columns"].([]string); ok {
		m.CSVColumns = v
	}
	if v, ok := a.Extra["root_element"].(string); ok {
		m.RootElement = v
	}
	if v, ok := a.Extra["kind"].(string); ok {
		m.JSONKind = v
	}
	if v, ok := a.Extra["width"].(int64); ok {
		m.ImgWidth = v
	}
	if v, ok := a.Extra["height"].(int64); ok {
		m.ImgHeight = v
	}
	if v, ok := a.Extra["camera_make"].(string); ok {
		m.CameraMake = v
	}
	if v, ok := a.Extra["camera_model"].(string); ok {
		m.CameraModel = v
	}
	if v, ok := a.Extra["lens"].(string); ok {
		m.Lens = v
	}
	if v, ok := a.Extra["taken_at"].(time.Time); ok && !v.IsZero() {
		m.TakenAt = v.Format(time.RFC3339)
	}
	if v, ok := a.Extra["orientation"].(int64); ok {
		m.Orientation = v
	}
	if v, ok := a.Extra["gps_lat"].(float64); ok {
		m.GPSLat = v
	}
	if v, ok := a.Extra["gps_lon"].(float64); ok {
		m.GPSLon = v
	}
	if v, ok := a.Extra["iso"].(int64); ok {
		m.ISO = v
	}
	if v, ok := a.Extra["focal_length"].(float64); ok {
		m.FocalLength = v
	}
	if v, ok := a.Extra["f_stop"].(float64); ok {
		m.FStop = v
	}
	if v, ok := a.Extra["exposure_time"].(float64); ok {
		m.ExposureTime = v
	}
	if v, ok := a.Extra["artist"].(string); ok {
		m.Artist = v
	}
	if v, ok := a.Extra["album"].(string); ok {
		m.Album = v
	}
	if v, ok := a.Extra["album_artist"].(string); ok {
		m.AlbumArtist = v
	}
	if v, ok := a.Extra["composer"].(string); ok {
		m.Composer = v
	}
	if v, ok := a.Extra["year"].(int64); ok {
		m.Year = v
	}
	if v, ok := a.Extra["track"].(int64); ok {
		m.Track = v
	}
	if v, ok := a.Extra["genre"].(string); ok {
		m.Genre = v
	}
	if v, ok := a.Extra["duration"].(float64); ok {
		m.Duration = v
	}
	if v, ok := a.Extra["bitrate"].(int64); ok {
		m.Bitrate = v
	}
	if v, ok := a.Extra["sample_rate"].(int64); ok {
		m.SampleRate = v
	}
	if v, ok := a.Extra["channels"].(int64); ok {
		m.Channels = v
	}
	if v, ok := a.Extra["bit_depth"].(int64); ok {
		m.BitDepth = v
	}
	if v, ok := a.Extra["nominal_bitrate"].(int64); ok {
		m.NominalBitrate = v
	}
	if v, ok := a.Extra["video_codec"].(string); ok {
		m.VideoCodec = v
	}
	if v, ok := a.Extra["audio_codec"].(string); ok {
		m.AudioCodec = v
	}
	if v, ok := a.Extra["video_width"].(int64); ok {
		m.VideoWidth = v
	}
	if v, ok := a.Extra["video_height"].(int64); ok {
		m.VideoHeight = v
	}
	if v, ok := a.Extra["frame_rate"].(float64); ok {
		m.FrameRate = v
	}
	if v, ok := a.Extra["rotation"].(int64); ok {
		m.Rotation = v
	}
	if v, ok := a.Extra["color_primaries"].(string); ok {
		m.ColorPrimaries = v
	}
	if v, ok := a.Extra["color_transfer"].(string); ok {
		m.ColorTransfer = v
	}
	if v, ok := a.Extra["is_hdr"].(bool); ok {
		m.IsHDR = v
	}
	if v, ok := a.Extra["subtitles"].(bool); ok {
		m.Subtitles = v
	}
	if v, ok := a.Extra["subtitle_languages"].([]string); ok && len(v) > 0 {
		m.SubtitleLanguages = v
	}
	if v, ok := a.Extra["replaygain_track_gain"].(float64); ok {
		m.ReplayGainTrackGain = v
	}
	if v, ok := a.Extra["replaygain_album_gain"].(float64); ok {
		m.ReplayGainAlbumGain = v
	}
	if v, ok := a.Extra["entry_count"].(int64); ok {
		m.EntryCount = v
	}
	if v, ok := a.Extra["uncompressed_size"].(int64); ok {
		m.UncompressedSize = v
	}
	if v, ok := a.Extra["top_level_entries"].([]string); ok && len(v) > 0 {
		m.TopLevelEntries = v
	}
	if v, ok := a.Extra["has_root_dir"].(bool); ok {
		m.HasRootDir = v
	}
	if v, ok := a.Extra["architectures"].([]string); ok && len(v) > 0 {
		m.Architectures = v
	}
	if v, ok := a.Extra["bitness"].(int64); ok {
		m.Bitness = v
	}
	if v, ok := a.Extra["binary_format"].(string); ok {
		m.BinaryFormat = v
	}
	if v, ok := a.Extra["binary_type"].(string); ok {
		m.BinaryType = v
	}
	if v, ok := a.Extra["is_dynamically_linked"].(bool); ok {
		m.IsDynamicallyLinked = v
	}
	if v, ok := a.Extra["is_stripped"].(bool); ok {
		m.IsStripped = v
	}
	if v, ok := a.Extra["entry_point"].(int64); ok {
		m.EntryPoint = v
	}
	if v, ok := a.Extra["email_to"].([]string); ok && len(v) > 0 {
		m.EmailTo = v
	}
	if v, ok := a.Extra["email_cc"].([]string); ok && len(v) > 0 {
		m.EmailCc = v
	}
	if v, ok := a.Extra["email_message_id"].(string); ok {
		m.EmailMessageID = v
	}
	if v, ok := a.Extra["email_in_reply_to"].(string); ok {
		m.EmailInReplyTo = v
	}
	if v, ok := a.Extra["sent_at"].(time.Time); ok && !v.IsZero() {
		m.SentAt = v.Format(time.RFC3339)
	}
	if v, ok := a.Extra["attachment_count"].(int64); ok {
		m.AttachmentCount = v
	}
	if v, ok := a.Extra["email_count"].(int64); ok {
		m.EmailCount = v
	}
	if v, ok := a.Extra["loc"].(int64); ok {
		m.LOC = v
	}
	if v, ok := a.Extra["comment_loc"].(int64); ok {
		m.CommentLOC = v
	}
	if v, ok := a.Extra["blank_loc"].(int64); ok {
		m.BlankLOC = v
	}
	if v, ok := a.Extra["cell_count"].(int64); ok {
		m.CellCount = v
	}
	if v, ok := a.Extra["code_cell_count"].(int64); ok {
		m.CodeCellCount = v
	}
	if v, ok := a.Extra["markdown_cell_count"].(int64); ok {
		m.MarkdownCellCount = v
	}
	if v, ok := a.Extra["kernel"].(string); ok {
		m.Kernel = v
	}
	if v, ok := a.Extra["frontmatter_format"].(string); ok {
		m.FrontmatterFormat = v
	}
	if v, ok := a.Extra["frontmatter"].(map[string]any); ok && len(v) > 0 {
		m.Frontmatter = v
	}
	if v, ok := a.Extra["tags"].([]string); ok && len(v) > 0 {
		m.Tags = v
	}
	if v, ok := a.Extra["categories"].([]string); ok && len(v) > 0 {
		m.Categories = v
	}
	if v, ok := a.Extra["draft"].(bool); ok {
		m.Draft = v
	}
	if v, ok := a.Extra["date"].(time.Time); ok && !v.IsZero() {
		m.Date = v.Format(time.RFC3339)
	}
	return m
}

// SearchOutput is the structured output of the `search` tool.
//
// When Cancelled is true, the walk did not complete; Matches contains
// every result that was emitted by the walker before the deadline /
// signal fired. CancellationReason distinguishes "timeout" (server
// default or per-call timeout_seconds expired) from "client_cancel"
// (the MCP client closed the request or the parent ctx was cancelled
// for some other reason). ElapsedSeconds reports wall-clock time spent
// inside the search handler — useful for tuning timeouts.
type SearchOutput struct {
	Matches            []SearchMatch `json:"matches"`
	Count              int           `json:"count"`
	Cancelled          bool          `json:"cancelled,omitempty"`
	CancellationReason string        `json:"cancellation_reason,omitempty"`
	ElapsedSeconds     float64       `json:"elapsed_seconds,omitempty"`
}

// ContentTypeDoc describes a registered content type.
type ContentTypeDoc struct {
	Name       string   `json:"name"`
	Extensions []string `json:"extensions"`
}

// ListAttributesOutput is the structured output of the `list_attributes` tool.
type ListAttributesOutput struct {
	Schema       celexpr.SchemaDoc `json:"schema"`
	ContentTypes []ContentTypeDoc  `json:"content_types"`
}

// IndexStatsOutput is the structured output of the `index_stats` tool.
// Counters are monotonic for the server process lifetime; restart resets.
type IndexStatsOutput struct {
	Hits   uint64 `json:"hits"`
	Misses uint64 `json:"misses"`
	Puts   uint64 `json:"puts"`
	Stales uint64 `json:"stales"`
	Errors uint64 `json:"errors"`
}

// StatsInput is the JSON-schema input for the `stats` tool.
type StatsInput struct {
	Expr             string   `json:"expr,omitempty" jsonschema:"Optional CEL expression to scope the histogram (e.g. 'is_markdown' counts only markdown files). Empty means every file. Same CEL surface as the search tool."`
	Dir              string   `json:"dir,omitempty" jsonschema:"Directory to walk. Defaults to '.'. Ignored when 'dirs' is non-empty."`
	Dirs             []string `json:"dirs,omitempty" jsonschema:"Multiple directories to aggregate stats across in one call. When non-empty, takes precedence over 'dir'."`
	Workers          int      `json:"workers,omitempty" jsonschema:"Parallel workers. Defaults to runtime.NumCPU()."`
	MaxLineBytes     int      `json:"max_line_bytes,omitempty" jsonschema:"Per-line scanner buffer cap for text/CSV/HTML (bytes). 0 uses the 1 MiB default."`
	TimeoutSeconds   *float64 `json:"timeout_seconds,omitempty" jsonschema:"Override the server's default per-call timeout. Same semantics as the search tool: positive = seconds, 0 = no timeout, omitted = server default. On timeout the partial histogram is returned with cancelled=true."`
	Excludes         []string `json:"excludes,omitempty" jsonschema:"Glob patterns matched against file/dir basenames; matches are pruned. Same as the search tool."`
	RespectGitignore bool     `json:"respect_gitignore,omitempty" jsonschema:"When true, parse a .gitignore at the walk root and skip matching paths."`
	GroupBy          string   `json:"group_by,omitempty" jsonschema:"Bucket key. Default 'content_type'. Recognised: content_type, ext, dir, language, camera_make, camera_model, lens, artist, album, genre, kernel, binary_format, binary_type, frontmatter_format. Unknown values fall back to content_type. Use group_by=ext to histogram by file extension, group_by=language to count source files per language, group_by=camera_make to bucket photos by camera, etc."`
}

// StatsOutput is the structured output of the `stats` tool — a
// histogram + totals + the standard partial-result fields
// (cancelled, cancellation_reason, elapsed_seconds) shared with
// the search tool.
//
// Groups is the bucket list keyed by the resolved group_by;
// ContentTypes is the legacy v0.20-shaped field, populated
// alongside Groups only when group_by is "content_type" / unset
// for back-compat with older agent integrations.
type StatsOutput struct {
	TotalCount         int64                    `json:"total_count"`
	TotalSize          int64                    `json:"total_size"`
	GroupBy            string                   `json:"group_by,omitempty"`
	Groups             []StatsBucket            `json:"groups"`
	ContentTypes       []StatsContentTypeBucket `json:"content_types,omitempty"`
	Cancelled          bool                     `json:"cancelled,omitempty"`
	CancellationReason string                   `json:"cancellation_reason,omitempty"`
	ElapsedSeconds     float64                  `json:"elapsed_seconds,omitempty"`
}

// StatsBucket is one row of the stats histogram. ContentTypeBucket
// is a back-compat alias for the same shape.
type StatsBucket struct {
	Name      string `json:"name"`
	Count     int64  `json:"count"`
	TotalSize int64  `json:"total_size"`
}

// StatsContentTypeBucket is the legacy bucket type kept for
// back-compat. Same shape as StatsBucket.
type StatsContentTypeBucket = StatsBucket

// FindDuplicatesInput is the JSON-schema input for the
// `find_duplicates` tool.
type FindDuplicatesInput struct {
	Expr             string   `json:"expr,omitempty" jsonschema:"Optional CEL expression to scope candidates (e.g. 'is_image' for photo dedup, 'is_archive' for archive dedup). Same CEL surface as search. Empty means every file."`
	Dir              string   `json:"dir,omitempty" jsonschema:"Directory to walk. Defaults to '.'. Ignored when 'dirs' is non-empty."`
	Dirs             []string `json:"dirs,omitempty" jsonschema:"Multiple directories to dedup across in one call."`
	Workers          int      `json:"workers,omitempty" jsonschema:"Parallel workers. Defaults to runtime.NumCPU()."`
	MaxLineBytes     int      `json:"max_line_bytes,omitempty" jsonschema:"Per-line scanner buffer cap (bytes). 0 uses the 1 MiB default."`
	TimeoutSeconds   *float64 `json:"timeout_seconds,omitempty" jsonschema:"Override the server's default per-call timeout. Same semantics as the search tool. Duplicate detection can be expensive on cold caches — pair with a generous timeout for first runs."`
	Excludes         []string `json:"excludes,omitempty" jsonschema:"Glob patterns matched against file/dir basenames; matches are pruned."`
	RespectGitignore bool     `json:"respect_gitignore,omitempty" jsonschema:"When true, parse a .gitignore at each walk root and skip matching paths."`
	MinSize          int64    `json:"min_size,omitempty" jsonschema:"Skip files smaller than this many bytes. Raise to e.g. 4096 to ignore tiny duplicates."`
}

// FindDuplicatesOutput is the structured output of `find_duplicates`.
type FindDuplicatesOutput struct {
	TotalFiles         int64           `json:"total_files"`
	DuplicateGroups    int64           `json:"duplicate_groups"`
	WastedBytes        int64           `json:"wasted_bytes"`
	Duplicates         []DuplicateGroup `json:"duplicates"`
	Cancelled          bool            `json:"cancelled,omitempty"`
	CancellationReason string          `json:"cancellation_reason,omitempty"`
	ElapsedSeconds     float64         `json:"elapsed_seconds,omitempty"`
}

// DuplicateGroup is one row of the duplicates output. Hash is the
// sha256 hex; Size is the per-file byte count (same for every
// file in the group); WastedBytes = (Count-1) * Size — the bytes
// a dedupe would reclaim.
type DuplicateGroup struct {
	Hash        string   `json:"hash"`
	Size        int64    `json:"size"`
	Count       int      `json:"count"`
	WastedBytes int64    `json:"wasted_bytes"`
	Paths       []string `json:"paths"`
}

// ReadLinesInput is the JSON-schema input for the `read_lines` tool.
type ReadLinesInput struct {
	Path      string `json:"path" jsonschema:"Filesystem path of the file to read. Absolute paths preferred; relative resolves against the server's working directory."`
	StartLine int    `json:"start_line,omitempty" jsonschema:"First line to return (1-indexed, inclusive). Defaults to 1."`
	EndLine   int    `json:"end_line,omitempty" jsonschema:"Last line to return (1-indexed, inclusive). 0 means 'to end of file'. Defaults to 0."`
	MaxLines  int    `json:"max_lines,omitempty" jsonschema:"Cap on lines returned. Defaults to 1000. When the requested range exceeds the cap, truncated=true and only the first max_lines of the range are returned."`
}

// ReadLinesOutput is the structured output of `read_lines`. Lines
// excludes trailing newlines; TotalLines is always populated so
// agents can decide whether to fetch additional ranges.
type ReadLinesOutput struct {
	Path       string   `json:"path"`
	StartLine  int      `json:"start_line"`
	EndLine    int      `json:"end_line"`
	TotalLines int      `json:"total_lines"`
	Lines      []string `json:"lines"`
	Truncated  bool     `json:"truncated,omitempty"`
}

// handlers wraps tool handlers so they can share an index reference
// and the server-level default timeout across the server's lifetime.
// The MCP SDK requires plain functions for AddTool, so we use closures
// to inject this shared state.
type handlers struct {
	idx            index.Index
	defaultTimeout time.Duration
}

// serverInstructions is the text sent to MCP clients during initialize
// (via ServerOptions.Instructions). Clients like Claude Code surface
// this as system context, so the agent knows the predicate vocabulary
// without having to call list_attributes first. Keep it dense but
// scan-friendly: a paragraph of intent, the boolean predicate list, the
// common-attribute list, and a handful of CEL recipes covering the main
// content families.
const serverInstructions = `file-search-on is a content-type-aware file search. The 'search' tool takes a CEL expression evaluated over per-file attributes and returns matching paths plus structured metadata.

Use these boolean type predicates directly in your CEL expression — no need to call list_attributes first for them:

  is_markdown   .md, .markdown
  is_pdf        .pdf
  is_html       .html, .htm
  is_xml        .xml
  is_json       .json
  is_csv        .csv, .tsv
  is_text       plain text and log files
  is_image      .jpg, .jpeg, .png, .gif, .tif, .tiff, .heic, .webp
  is_audio      .mp3, .m4a, .flac, .ogg, .wav
  is_video      .mp4, .mov, .m4v, .mkv, .webm, .avi
  is_office     .docx, .xlsx, .pptx, .odt
  is_epub       .epub
  is_archive    .zip, .tar, .tar.gz, .gz
  is_binary     ELF / Mach-O / PE compiled binaries
  is_email      .eml, .mbox
  is_source     Go / Python / JS / TS / Rust / C / C++ / Java / Ruby / Swift / Kotlin / Shell / Lua / Elixir / Clojure / Haskell / OCaml / Zig

Common attributes available on every file: name, path, dir, ext, size (bytes, int), content_type. Per-family attributes the parser populates when the file matches:

  documents:  title, author, language, word_count, line_count, page_count
  data:       json_kind ("object"/"array"), csv_columns (list<string>), root_element
  markdown:   tags, categories, draft, date, frontmatter (map<string,dyn>), frontmatter_format
  images:     img_width, img_height, camera_make, camera_model, lens, taken_at, iso, focal_length, f_stop, exposure_time, gps_lat, gps_lon, orientation
  audio:      artist, album, album_artist, composer, year, track, genre, duration, bitrate, sample_rate, channels, bit_depth
  video:      video_codec, audio_codec, video_width, video_height, frame_rate, duration, is_hdr, subtitles
  archives:   entry_count, uncompressed_size, top_level_entries, has_root_dir
  binaries:   architectures (list<string>), bitness, binary_format, binary_type, is_dynamically_linked, is_stripped, entry_point
  email:      email_to, email_cc, email_message_id, email_in_reply_to, sent_at, attachment_count, email_count
  source:     language, line_count, loc, comment_loc, blank_loc

Recipe expressions:

  is_markdown && word_count > 500
  is_pdf && page_count > 10
  is_image && camera_make == "SONY" && iso > 1600
  is_image && taken_at > timestamp("2024-01-01T00:00:00Z")
  is_audio && sample_rate >= 96000
  is_video && video_height >= 2160 && duration > 1800
  is_csv && csv_columns.exists(c, c == "revenue")
  is_office && language == "fr"
  is_archive && uncompressed_size > 100000000
  is_binary && "x86_64" in architectures
  is_source && language == "go" && loc > 200
  is_email && size > 0 && sent_at > timestamp("2025-01-01T00:00:00Z")
  size > 10000000 && !is_video                                  // large non-video files
  is_markdown && tags.exists(t, t == "draft") && !draft
  levenshtein(artist, "Radiohead") <= 2 && is_audio             // fuzzy: typo-tolerant
  soundex(author) == soundex("Smith") && is_markdown            // phonetic
  point_in_polygon(gps_lat, gps_lon, [[51.5,-0.2],[51.6,-0.2],[51.6,0.0],[51.5,0.0]])  // images inside London bbox

Tools:
  search           run a CEL expression against a directory; returns matches[] and count
  read_attributes  same SearchMatch shape for one path; use when you already have the file
  read_lines       print a specific line range from a file — for context around a search match
  stats            histogram + totals for a directory tree, bucketed by any attribute via group_by
  find_duplicates  groups of byte-identical files keyed by sha256 — "what's eating my disk?"
  list_attributes  full schema (every attribute, every built-in function); call when the recipes above don't cover what you need
  index_stats      cache hit/miss counters for this server process

Performance: an attribute cache lives for the server's lifetime; repeated calls against the same files skip the per-file parse step. Empty 'expr' matches all files; empty 'dir' defaults to '.'.

Top-K and pagination: pass 'sort_by' to order results by an attribute, and 'limit' to cap the response. Recognised sort keys: size, name, path, mod_time, word_count, line_count, page_count, duration, bitrate, sample_rate, video_height, video_width, frame_rate, iso, focal_length, taken_at, sent_at, year, entry_count, uncompressed_size, loc, attachment_count, email_count. 'order' is 'asc' (default) or 'desc'. Example for "10 most recent photos": {"expr": "is_image", "dir": "~/Pictures", "sort_by": "taken_at", "order": "desc", "limit": 10}. Without sort_by, limit returns the first N in walk order. With sort_by, the full result set is sorted then truncated to the top-K.

Snippets: pass 'include_snippet': true to populate each match's 'snippet' field with the first N lines of the file body (controlled by snippet_lines, default 10). Only text-based content types (markdown / text / html / csv / json / xml / source/*) populate; binary families leave snippet empty. Useful for "show me what these files are about" without a follow-up read.

Body-content filters: pass 'include_body': true to expose the full file body to the CEL expression as the 'body' string variable. CEL's built-in string methods then act as content filters — body.contains("transformer"), body.matches("\\bAPI\\b") (RE2 regex), body.startsWith("Once upon"), size(body) > 5000. Only text-based content types populate body; capped at body_max_bytes (default 1 MiB). EXPENSIVE — reads every candidate file, not just headers. Pair with a tight expr (e.g. 'is_markdown && body.contains(...)') so the type predicate prunes most candidates before the body read. Note: CEL's 'matches' uses RE2 (Google's regex syntax), the same engine Go's regexp/re2 package uses.

Stats / reconnaissance: the 'stats' tool aggregates a histogram + total counts + total sizes for a directory tree, optionally scoped by a CEL expr. Default bucket is content_type; pass 'group_by' to bucket by another attribute — ext, dir, language, camera_make, camera_model, lens, artist, album, genre, kernel, binary_format, binary_type, frontmatter_format. Example: {expr:'is_image', group_by:'camera_make'} for photos-by-camera. Output's groups[] is the resolved histogram; content_types[] is populated alongside only for the default group_by (back-compat with v0.20 clients). Same excludes / respect_gitignore / timeout_seconds semantics as search; returns cancelled=true on timeout with the partial histogram intact.

Multi-directory search: both 'search' and 'stats' accept 'dirs': []string. When non-empty it overrides 'dir' and walks all roots in one call (each root's .gitignore is honoured independently). Useful when an agent needs to search across, say, ~/Documents AND ~/Downloads without two round-trips.

Read line ranges: the 'read_lines' tool returns lines [start_line, end_line] of a single file (1-indexed, inclusive). Useful as the second step after search — find matches via search, then call read_lines for context around each match without a separate read tool. max_lines caps the response (default 1000); the truncated flag tells you when the cap was hit.

Duplicate detection: 'find_duplicates' returns groups of byte-identical files keyed by sha256. Two-pass for performance: files with unique sizes are skipped (cheaper than computing a hash you don't need). Pair with expr to scope (e.g. expr='is_image' to dedup photos only) and min_size to skip tiny duplicates. Hashes are cached in the attribute index alongside (size, mtime); first runs on large trees can be slow but subsequent calls on unchanged files are free. Output's duplicates[] is sorted by wasted_bytes descending so the biggest reclamation candidates show first.

Time-bucket aggregation: 'stats' group_by accepts mtime_year, mtime_month, mtime_day, taken_at_year/month/day, sent_at_year/month/day, and date_year/month/day in addition to the string-attribute keys. Files with zero timestamps bucket as "(no date)" so they don't collide with "1970-01-01". Example: {expr:'is_image', group_by:'taken_at_year'} for "photos per year".

Excluding directories: pass 'excludes' to skip directories and files by basename glob. Common values: ['node_modules', '.git', 'target', 'dist', '__pycache__', '*.bak']. Matched directories are pruned (their entire subtree is skipped). For path-aware semantics like 'src/build', set 'respect_gitignore': true and the server will parse a .gitignore at the walk root.

Timeouts and partial results: every tool call is wrapped with a server-default timeout (typically 60s; configured at server startup via --timeout). The 'search' tool also accepts 'timeout_seconds' on input — pass a positive number to override, or 0 to disable for that call. On expiry, the search tool DOES NOT return an error; it returns the partial match set with cancelled=true, cancellation_reason="timeout" (or "client_cancel" for transport-side cancellation), and elapsed_seconds set. Always inspect 'cancelled' in the response — a partial result set may be exactly what you want, or you may want to retry with a tighter expression / larger timeout / smaller dir. read_attributes is bounded by the same default timeout but returns an error on cancellation (no partial-result semantics for one file).`

// New builds an MCP server with file-search-on's tools registered. The
// server is not connected to a transport; callers either pass it to
// (*mcp.Server).Run for stdio service or (*mcp.Server).Connect for
// in-memory tests.
//
// idx is the attribute cache used by every search and read_attributes
// call. It is intended to be created once per server process and live
// for the server's lifetime — e.g. an in-memory index for stdio MCP,
// or a bbolt-backed index opened via index.Open(path) when the user
// wants persistence across restarts. nil idx disables caching; callers
// almost always want index.NewMemory() at the very least.
//
// defaultTimeout is the per-call ceiling applied to every tool
// invocation. <= 0 disables the default (calls inherit the parent ctx
// only). The search tool's input also accepts a per-call
// timeout_seconds override. A bounded default is strongly recommended
// because MCP clients have their own read deadlines; a runaway server
// walk would otherwise wedge the client.
func New(version string, idx index.Index, defaultTimeout time.Duration) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{
		Name:    "file-search-on",
		Version: version,
	}, &mcp.ServerOptions{
		Instructions: serverInstructions,
	})

	h := &handlers{idx: idx, defaultTimeout: defaultTimeout}

	mcp.AddTool(s, &mcp.Tool{
		Name:        "search",
		Description: "Recursively search a directory for files matching a CEL expression evaluated over file metadata and content-type-specific attributes. Boolean type predicates you can use directly in expr: is_markdown, is_pdf, is_html, is_xml, is_json, is_csv, is_text, is_image, is_audio, is_video, is_office, is_epub, is_archive, is_binary, is_email, is_source. Common scalar attributes: size (int bytes), name, path, dir, ext, content_type, title, author, language, word_count, line_count, page_count. Per-family attributes (image EXIF, audio tags, video codec, frontmatter, archive sizes, binary architectures, email headers, source-LOC) populate when the matching family fires — see list_attributes for the full schema. Built-in functions: levenshtein(a, b), soundex(s), ngrams(s, n), ngram_similarity(a, b, n) for fuzzy / phonetic matching; point_in_polygon(lat, lon, polygon) for GPS-bbox filtering. Example exprs: 'is_markdown && word_count > 500'; 'is_pdf && page_count > 10'; 'is_image && iso > 1600'; 'is_video && video_height >= 2160 && duration > 1800'; 'is_source && language == \"go\" && loc > 200'. Top-K queries: pass sort_by + limit, e.g. {expr:'is_video', sort_by:'duration', order:'desc', limit:5} for the 5 longest videos. Sort keys: size, name, path, mod_time, word_count, line_count, page_count, duration, bitrate, sample_rate, video_height, video_width, frame_rate, iso, focal_length, taken_at, sent_at, year, entry_count, uncompressed_size, loc, attachment_count, email_count. Snippets: pass include_snippet=true to populate match.snippet with the first N lines of body text (text content types only). Body filters: pass include_body=true to expose the full file body as the CEL 'body' variable, then use built-in string methods to filter: body.contains(\"X\"), body.matches(\"\\\\bX\\\\b\") (RE2 regex). Body reads every candidate file — pair with a tight type predicate (e.g. is_markdown). Exclusions: pass excludes (basename globs like ['node_modules', '.git', '*.bak']) and/or respect_gitignore=true to prune the walk. Repeated searches reuse a per-process attribute cache so unchanged files skip the parse step (see index_stats). Timeouts: every call is bounded by the server's default timeout (set at startup via --timeout, typically 60s); pass timeout_seconds in the input to override (positive = seconds, 0 = no timeout). On timeout the call DOES NOT error — it returns the partial match set with cancelled=true, cancellation_reason set, and elapsed_seconds populated. Always check 'cancelled' before treating the result as exhaustive.",
	}, h.searchHandler)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_attributes",
		Description: "List every CEL attribute available to the search tool, the built-in functions (levenshtein, soundex, ngrams, ngram_similarity, point_in_polygon) with their signatures, and the registered content types.",
	}, listAttributesHandler)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "read_attributes",
		Description: "Extract content-type-specific attributes for a single file path. Use when the agent already knows the path and wants metadata without running a CEL filter or walking a directory. Returns the same SearchMatch shape as the search tool — title, author, EXIF, audio tags, video codec, frontmatter, etc., depending on the detected content type.",
	}, h.readAttributesHandler)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "index_stats",
		Description: "Return cumulative attribute-cache counters (hits, misses, puts, stales, errors) for the running MCP server. Counters reset on server restart.",
	}, h.indexStatsHandler)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "stats",
		Description: "Aggregate counts and total sizes for a directory tree, bucketed by an attribute. Default bucket is content_type; pass group_by to bucket by ext, dir, language, camera_make, camera_model, lens, artist, album, genre, kernel, binary_format, binary_type, or frontmatter_format. Useful for 'what's in this folder?' and 'how many photos per camera?' style reconnaissance without retrieving individual paths. Accepts an optional CEL expr to scope the histogram (e.g. expr='is_image' + group_by='camera_make' for photos-by-camera). Multi-dir: pass 'dirs' to aggregate across multiple roots in one call. Honours the same excludes / respect_gitignore / timeout_seconds semantics as the search tool, including partial-result returns on cancellation. Output's `groups[]` is the histogram keyed by the resolved group_by; `content_types[]` is populated alongside only for the default group_by, kept for back-compat with older clients.",
	}, h.statsHandler)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "read_lines",
		Description: "Print a specific line range from a single file. Completes the search-then-inspect loop without a separate read tool — agent flow: search for matches, then call read_lines for context around each match. Inputs: path (required), start_line (1-indexed inclusive; default 1), end_line (1-indexed inclusive; 0 = end of file), max_lines (cap; default 1000). Returns lines[] (no trailing newlines), total_lines, and truncated:true when the requested range exceeds max_lines. Errors only on missing/unreadable files or invalid ranges (start_line > end_line); pathological lines (huge / non-UTF-8) are truncated at 64 KiB per line and the scan continues.",
	}, h.readLinesHandler)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "find_duplicates",
		Description: "Find groups of byte-identical files keyed by sha256. Useful for 'what's eating my disk?' and 'find redundant copies' workflows. Two-pass for performance: files with unique sizes are skipped entirely (cheaper than computing their hash). Pair with expr to scope (e.g. expr='is_image' for photo dedup) and min_size to skip tiny duplicates. Hashes are cached in the attribute index alongside (size, mtime) — first run on a large tree can be slow (every candidate file is read in full), but subsequent runs are free for unchanged files. Output: duplicates[] sorted by wasted_bytes descending — biggest reclamation candidates first.",
	}, h.findDuplicatesHandler)

	return s
}

// Run starts an MCP server on stdio with the given index and default
// per-call timeout, and blocks until the transport closes or ctx is
// cancelled.
func Run(ctx context.Context, version string, idx index.Index, defaultTimeout time.Duration) error {
	return New(version, idx, defaultTimeout).Run(ctx, &mcp.StdioTransport{})
}

// progressNotifyStride is the number of matches between two
// notifications/progress messages. Smaller searches (< stride matches)
// emit zero notifications and just land in one final response. Tunable
// later via an Options field if a client needs finer granularity.
const progressNotifyStride = 50

func (h *handlers) searchHandler(ctx context.Context, req *mcp.CallToolRequest, in SearchInput) (*mcp.CallToolResult, SearchOutput, error) {
	expr := in.Expr
	if expr == "" {
		expr = "true"
	}
	dir := in.Dir
	if dir == "" {
		dir = "."
	}

	// Resolve the effective timeout: per-call override > server default.
	// A nil pointer inherits the default; an explicit 0 means "no
	// timeout for this call" (the parent ctx still applies). We track
	// the parent ctx separately so we can distinguish a server-level
	// cancellation (transport close, parent ctx) from our own
	// timeout firing.
	parentCtx := ctx
	timeout := h.defaultTimeout
	if in.TimeoutSeconds != nil {
		timeout = time.Duration(*in.TimeoutSeconds * float64(time.Second))
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	start := time.Now()

	out := make(chan search.Result, 64)
	var walkErr error
	done := make(chan struct{})
	// The MCP handler always buffers results (it sorts by path
	// before returning) so we route sort/limit through search.Walk
	// rather than re-implementing the post-sort here. But progress
	// notifications + cancellation handling still want streaming —
	// so we feed the channel ourselves and sort/limit the collected
	// matches post-stream using the same sortAndLimit helper.
	// Multi-dir: in.Dirs wins when non-empty; else fall back to
	// the single 'dir' field (with default "." applied above).
	walkOpts := search.Options{
		Root:              dir,
		Roots:             in.Dirs,
		Expr:              expr,
		Workers:           in.Workers,
		MaxLineBytes:      in.MaxLineBytes,
		IncludeAttributes: true,
		Index:             h.idx,
		SnippetLines:      in.SnippetLines,
		IncludeSnippet:    in.IncludeSnippet,
		IncludeBody:       in.IncludeBody,
		BodyMaxBytes:      in.BodyMaxBytes,
		Excludes:          in.Excludes,
		RespectGitignore:  in.RespectGitignore,
		// Sort, Order, Limit are applied via sortAndLimit AFTER we
		// collect — see end of handler. We don't pass them to
		// WalkStream because WalkStream doesn't honour them.
	}
	go func() {
		walkErr = search.WalkStream(ctx, walkOpts, content.DefaultRegistry(), out)
		close(done)
	}()

	// Drain the channel as matches arrive. Emit a progress notification
	// every `progressNotifyStride` matches when the client passed a
	// progressToken — the SDK's NotifyProgress is a no-op for clients
	// that didn't request progress.
	//
	// We collect raw search.Results here (not the projected
	// SearchMatch wire shape) so sort_by has access to the full
	// FileAttributes for per-family scalar keys. Projection happens
	// after the sort.
	token := req.Params.GetProgressToken()
	var results []search.Result
	processed := int64(0)
	for r := range out {
		results = append(results, r)
		processed++
		if token != nil && processed%progressNotifyStride == 0 {
			_ = req.Session.NotifyProgress(ctx, &mcp.ProgressNotificationParams{
				ProgressToken: token,
				Progress:      float64(processed),
				Message:       fmt.Sprintf("%d matches so far", processed),
			})
		}
	}
	<-done

	elapsed := time.Since(start).Seconds()

	cancelled := errors.Is(walkErr, context.Canceled) || errors.Is(walkErr, context.DeadlineExceeded)
	if walkErr != nil && !cancelled {
		return nil, SearchOutput{}, fmt.Errorf("walk: %w", walkErr)
	}

	// Order: explicit sort_by > legacy path-sort default. Limit is
	// applied AFTER sorting so the response respects top-K
	// semantics. sortAndLimit lives in internal/search next to the
	// type-aware compareByKey so this stays the single source of
	// truth for sort/limit logic.
	if in.SortBy != "" || in.Limit > 0 {
		sortOpts := search.Options{Sort: in.SortBy, Order: in.Order, Limit: in.Limit}
		results = search.SortAndLimit(results, sortOpts)
	} else {
		// Historical contract: matches sorted by path. Preserve it
		// when the caller didn't request a specific sort.
		sort.Slice(results, func(i, j int) bool { return results[i].Path < results[j].Path })
	}

	matches := make([]SearchMatch, len(results))
	for i, r := range results {
		matches[i] = matchFrom(r)
	}

	output := SearchOutput{
		Matches:        matches,
		Count:          len(matches),
		ElapsedSeconds: elapsed,
	}
	if cancelled {
		output.Cancelled = true
		// "timeout" when our deadline fired and the parent ctx is
		// still healthy; otherwise the parent (transport / client /
		// process signal) is the cause.
		if errors.Is(walkErr, context.DeadlineExceeded) && parentCtx.Err() == nil {
			output.CancellationReason = "timeout"
		} else {
			output.CancellationReason = "client_cancel"
		}
	}
	return nil, output, nil
}

func (h *handlers) readAttributesHandler(ctx context.Context, _ *mcp.CallToolRequest, in ReadAttributesInput) (*mcp.CallToolResult, SearchMatch, error) {
	if in.Path == "" {
		return nil, SearchMatch{}, fmt.Errorf("path is required")
	}
	abs, err := filepath.Abs(in.Path)
	if err != nil {
		return nil, SearchMatch{}, fmt.Errorf("resolve path: %w", err)
	}
	dir := filepath.Dir(abs)
	base := filepath.Base(abs)

	// Single-file extraction is bounded but not free (markdown reads
	// the whole file; PDFs / EXIF are header-only). Apply the server
	// default timeout so a pathological file can't wedge the server.
	if h.defaultTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, h.defaultTimeout)
		defer cancel()
	}

	attrs, err := celexpr.BuildAttributesWith(ctx, os.DirFS(dir), base, abs, content.DefaultRegistry(), celexpr.BuildOptions{Index: h.idx})
	if err != nil {
		return nil, SearchMatch{}, fmt.Errorf("read attributes: %w", err)
	}
	return nil, matchFrom(search.Result{
		Path:        abs,
		ContentType: attrs.ContentType,
		Size:        attrs.Size,
		Attrs:       attrs,
	}), nil
}

func (h *handlers) readLinesHandler(ctx context.Context, _ *mcp.CallToolRequest, in ReadLinesInput) (*mcp.CallToolResult, ReadLinesOutput, error) {
	if in.Path == "" {
		return nil, ReadLinesOutput{}, fmt.Errorf("path is required")
	}
	abs, err := filepath.Abs(in.Path)
	if err != nil {
		return nil, ReadLinesOutput{}, fmt.Errorf("resolve path: %w", err)
	}
	dir := filepath.Dir(abs)
	base := filepath.Base(abs)

	// Honour the server's default timeout so a pathological file
	// (multi-gigabyte log) can't wedge the server. read_lines is
	// bounded by max_lines too, but the line scanner can still
	// take real time on huge files.
	if h.defaultTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, h.defaultTimeout)
		defer cancel()
	}

	res, err := search.ReadLines(ctx, os.DirFS(dir), base, in.StartLine, in.EndLine, in.MaxLines)
	if err != nil {
		return nil, ReadLinesOutput{}, fmt.Errorf("read lines: %w", err)
	}
	return nil, ReadLinesOutput{
		Path:       abs,
		StartLine:  res.StartLine,
		EndLine:    res.EndLine,
		TotalLines: res.TotalLines,
		Lines:      res.Lines,
		Truncated:  res.Truncated,
	}, nil
}

func (h *handlers) findDuplicatesHandler(ctx context.Context, _ *mcp.CallToolRequest, in FindDuplicatesInput) (*mcp.CallToolResult, FindDuplicatesOutput, error) {
	expr := in.Expr
	if expr == "" {
		expr = "true"
	}
	dir := in.Dir
	if dir == "" && len(in.Dirs) == 0 {
		dir = "."
	}

	// Same timeout resolution as the other walking tools.
	timeout := h.defaultTimeout
	if in.TimeoutSeconds != nil {
		timeout = time.Duration(*in.TimeoutSeconds * float64(time.Second))
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	start := time.Now()
	dups, err := search.FindDuplicates(ctx, search.Options{
		Root:             dir,
		Roots:            in.Dirs,
		Expr:             expr,
		Workers:          in.Workers,
		MaxLineBytes:     in.MaxLineBytes,
		Index:            h.idx,
		Excludes:         in.Excludes,
		RespectGitignore: in.RespectGitignore,
		MinSize:          in.MinSize,
	}, content.DefaultRegistry())
	elapsed := time.Since(start).Seconds()

	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		return nil, FindDuplicatesOutput{}, fmt.Errorf("find_duplicates: %w", err)
	}

	out := FindDuplicatesOutput{ElapsedSeconds: elapsed}
	if dups != nil {
		out.TotalFiles = dups.TotalFiles
		out.DuplicateGroups = dups.DuplicateGroups
		out.WastedBytes = dups.WastedBytes
		out.Cancelled = dups.Cancelled
		out.CancellationReason = dups.CancellationReason
		out.Duplicates = make([]DuplicateGroup, len(dups.Duplicates))
		for i, g := range dups.Duplicates {
			out.Duplicates[i] = DuplicateGroup{
				Hash:        g.Hash,
				Size:        g.Size,
				Count:       g.Count,
				WastedBytes: g.WastedBytes,
				Paths:       g.Paths,
			}
		}
	}
	return nil, out, nil
}

func (h *handlers) statsHandler(ctx context.Context, _ *mcp.CallToolRequest, in StatsInput) (*mcp.CallToolResult, StatsOutput, error) {
	expr := in.Expr
	if expr == "" {
		expr = "true"
	}
	dir := in.Dir
	if dir == "" {
		dir = "."
	}

	// Same timeout resolution as searchHandler: per-call > server
	// default > none. parentCtx separation isn't needed because
	// ComputeStats itself surfaces cancelled=true via the Stats
	// struct rather than via the ctx — we just need to apply the
	// deadline.
	timeout := h.defaultTimeout
	if in.TimeoutSeconds != nil {
		timeout = time.Duration(*in.TimeoutSeconds * float64(time.Second))
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	start := time.Now()
	stats, err := search.ComputeStats(ctx, search.Options{
		Root:             dir,
		Roots:            in.Dirs,
		Expr:             expr,
		Workers:          in.Workers,
		MaxLineBytes:     in.MaxLineBytes,
		Index:            h.idx,
		Excludes:         in.Excludes,
		RespectGitignore: in.RespectGitignore,
		GroupBy:          in.GroupBy,
	}, content.DefaultRegistry())
	elapsed := time.Since(start).Seconds()

	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		return nil, StatsOutput{}, fmt.Errorf("stats: %w", err)
	}

	out := StatsOutput{
		ElapsedSeconds: elapsed,
	}
	if stats != nil {
		out.TotalCount = stats.TotalCount
		out.TotalSize = stats.TotalSize
		out.GroupBy = stats.GroupBy
		out.Cancelled = stats.Cancelled
		out.CancellationReason = stats.CancellationReason
		out.Groups = make([]StatsBucket, len(stats.Groups))
		for i, b := range stats.Groups {
			out.Groups[i] = StatsBucket{
				Name:      b.Name,
				Count:     b.Count,
				TotalSize: b.TotalSize,
			}
		}
		if len(stats.ContentTypes) > 0 {
			out.ContentTypes = make([]StatsContentTypeBucket, len(stats.ContentTypes))
			for i, b := range stats.ContentTypes {
				out.ContentTypes[i] = StatsContentTypeBucket{
					Name:      b.Name,
					Count:     b.Count,
					TotalSize: b.TotalSize,
				}
			}
		}
	}
	return nil, out, nil
}

func (h *handlers) indexStatsHandler(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, IndexStatsOutput, error) {
	if h.idx == nil {
		return nil, IndexStatsOutput{}, nil
	}
	st := h.idx.Stats()
	return nil, IndexStatsOutput{
		Hits:   st.Hits,
		Misses: st.Misses,
		Puts:   st.Puts,
		Stales: st.Stales,
		Errors: st.Errors,
	}, nil
}

func listAttributesHandler(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, ListAttributesOutput, error) {
	types := content.DefaultRegistry().Types()
	docs := make([]ContentTypeDoc, len(types))
	for i, t := range types {
		docs[i] = ContentTypeDoc{Name: t.Name(), Extensions: t.Extensions()}
	}
	return nil, ListAttributesOutput{
		Schema:       celexpr.Schema(),
		ContentTypes: docs,
	}, nil
}
