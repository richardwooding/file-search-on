package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/template"

	"github.com/richardwooding/file-search-on/internal/projecttype"
	"github.com/richardwooding/file-search-on/internal/search"
)

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
		writeVerboseRecord(w, search.MatchFrom(r))
	}
}

// writeVerboseRecord renders one match. Caller is responsible for the
// inter-record blank line. Shared between printVerbose (slice) and
// printVerboseStream (channel).
func writeVerboseRecord(w io.Writer, rec search.Match) {
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
	printIfStr(w, "yaml_kind", rec.YAMLKind)
	printIfInt(w, "yaml_document_count", rec.YAMLDocumentCount)
	printIfStr(w, "module", rec.Module)
	printIfStr(w, "go_version", rec.GoVersion)
	printIfStr(w, "base_image", rec.BaseImage)
	printIfStr(w, "project_type", rec.ProjectType)
	if len(rec.ProjectTypes) > 0 {
		fp(w, "  project_types  [%s]\n", strings.Join(rec.ProjectTypes, ", "))
	}

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

	// Source-code metadata.
	printIfInt(w, "loc", rec.LOC)
	printIfInt(w, "comment_loc", rec.CommentLOC)
	printIfInt(w, "blank_loc", rec.BlankLOC)

	// Notebook metadata.
	printIfInt(w, "cell_count", rec.CellCount)
	printIfInt(w, "code_cells", rec.CodeCellCount)
	printIfInt(w, "md_cells", rec.MarkdownCellCount)
	printIfStr(w, "kernel", rec.Kernel)

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

	// Snippet sits at the bottom because it can span many lines —
	// keeping the scalar attributes aligned at the top makes the
	// record scannable. Indent each line for readability.
	if rec.Snippet != "" {
		fp(w, "  snippet:\n")
		for line := range strings.SplitSeq(rec.Snippet, "\n") {
			fp(w, "    %s\n", line)
		}
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

// printBareStream is the streaming counterpart of printBare. Reads
// from ch until it closes, writing one path per line. Nondeterministic
// order (walk order, mangled by the worker pool).
func printBareStream(w io.Writer, ch <-chan search.Result) {
	for r := range ch {
		fpn(w, r.Path)
	}
}

// printJSONStream is the streaming counterpart of printJSON. Writes
// one NDJSON object per match as matches arrive.
func printJSONStream(w io.Writer, ch <-chan search.Result) error {
	enc := json.NewEncoder(w)
	for r := range ch {
		if err := enc.Encode(search.MatchFrom(r)); err != nil {
			return fmt.Errorf("json encode %s: %w", r.Path, err)
		}
	}
	return nil
}

// printTemplateStream is the streaming counterpart of printTemplate.
// Renders each match through the parsed template as it arrives.
func printTemplateStream(w io.Writer, ch <-chan search.Result, tmpl *template.Template) error {
	for r := range ch {
		if err := tmpl.Execute(w, search.MatchFrom(r)); err != nil {
			return fmt.Errorf("template execute %s: %w", r.Path, err)
		}
		fpn(w)
	}
	return nil
}

// printDefaultStream is the streaming counterpart of printDefault.
// Returns the number of records seen so the caller can emit the
// "N file(s) found" footer after the stream closes.
func printDefaultStream(w io.Writer, ch <-chan search.Result) int64 {
	var count int64
	for r := range ch {
		count++
		ct := r.ContentType
		if ct == "" {
			ct = "unknown"
		}
		fp(w, "%s\t[%s]\t%d bytes\n", r.Path, ct, r.Size)
	}
	return count
}

// printVerboseStream is the streaming counterpart of printVerbose.
// Returns the number of records seen so the caller can emit the
// "N file(s) found" footer after the stream closes.
func printVerboseStream(w io.Writer, ch <-chan search.Result) int64 {
	var count int64
	for r := range ch {
		if count > 0 {
			fpn(w)
		}
		count++
		writeVerboseRecord(w, search.MatchFrom(r))
	}
	return count
}

// printJSON writes one JSON object per line (NDJSON / JSON Lines).
func printJSON(w io.Writer, results []search.Result) error {
	enc := json.NewEncoder(w)
	for _, r := range results {
		if err := enc.Encode(search.MatchFrom(r)); err != nil {
			return fmt.Errorf("json encode %s: %w", r.Path, err)
		}
	}
	return nil
}

// printTemplate renders each match through a parsed text/template against a search.Match.
func printTemplate(w io.Writer, results []search.Result, tmpl *template.Template) error {
	for _, r := range results {
		if err := tmpl.Execute(w, search.MatchFrom(r)); err != nil {
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

// printStatsTable renders a Stats histogram as a human-readable
// table on stdout. The header column reflects the group_by — e.g.
// "content_type", "language", "camera_make". Rows are sorted by
// ComputeStats (count desc, name asc); the footer shows totals.
func printStatsTable(w io.Writer, s *search.Stats) {
	if len(s.Groups) == 0 {
		fp(w, "no files matched\n")
		return
	}
	header := s.GroupBy
	if header == "" {
		header = "content_type"
	}
	fp(w, "%-30s %10s %15s\n", header, "count", "total_size")
	for _, b := range s.Groups {
		fp(w, "%-30s %10s %15s\n", b.Name, commafy(b.Count), commafy(b.TotalSize)+" B")
	}
	fp(w, "%-30s %10s %15s\n", "---", "---", "---")
	fp(w, "%-30s %10s %15s\n", "TOTAL", commafy(s.TotalCount), commafy(s.TotalSize)+" B")
	if s.Cancelled {
		fp(w, "(partial — %s)\n", s.CancellationReason)
	}
}

// printStatsJSON writes the Stats object as a single JSON document.
// Useful for piping into jq / scripts.
func printStatsJSON(w io.Writer, s *search.Stats) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(s)
}

// printDuplicatesTable renders a Duplicates result as a
// human-readable table on stdout. One block per group, sorted by
// wasted-bytes descending. Footer summarises the totals.
func printDuplicatesTable(w io.Writer, d *search.Duplicates) {
	if len(d.Duplicates) == 0 {
		fp(w, "no duplicates found (%s files considered)\n", commafy(d.TotalFiles))
		return
	}
	for i, g := range d.Duplicates {
		if i > 0 {
			fpn(w)
		}
		fp(w, "hash:  %s\n", g.Hash)
		fp(w, "size:  %s bytes  (count=%d, wasted=%s B)\n",
			commafy(g.Size), g.Count, commafy(g.WastedBytes))
		for _, p := range g.Paths {
			fp(w, "  %s\n", p)
		}
	}
	fpn(w)
	fp(w, "%s duplicate group(s), %s files considered, %s B wasted\n",
		commafy(d.DuplicateGroups), commafy(d.TotalFiles), commafy(d.WastedBytes))
	if d.Cancelled {
		fp(w, "(partial — %s)\n", d.CancellationReason)
	}
}

// printDuplicatesJSON writes the Duplicates object as a single
// JSON document.
func printDuplicatesJSON(w io.Writer, d *search.Duplicates) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(d)
}

// printFindMatches renders a FindMatchesResult as grep-style output.
// Each match line is "path:line:text". Context lines (when present)
// use "path-line-text" — same convention as ripgrep / grep -C, so the
// dash vs colon distinguishes context from match. Matches are
// separated by a blank line (and a "--" marker, ripgrep-style) only
// when context is non-empty; without context the output is dense.
func printFindMatches(w io.Writer, r *search.FindMatchesResult) {
	if len(r.Matches) == 0 {
		fp(w, "no matches (%d file(s) scanned)\n", r.FilesScanned)
		if r.Cancelled {
			fp(w, "(partial — %s)\n", r.CancellationReason)
		}
		return
	}
	hasContext := false
	for _, m := range r.Matches {
		if len(m.Before) > 0 || len(m.After) > 0 {
			hasContext = true
			break
		}
	}
	prevPath := ""
	for i, m := range r.Matches {
		// Inter-match separator: only when context was attached. ripgrep
		// uses "--" between context blocks; emulate.
		if hasContext && i > 0 {
			fp(w, "--\n")
		}
		// Header line per file when path changes. Without context this
		// would be noise; only emit when context is present.
		if hasContext && m.Path != prevPath {
			fp(w, "%s\n", m.Path)
			prevPath = m.Path
		}
		// Before-context lines: numbered if we know the start.
		startBefore := m.Line - len(m.Before)
		for j, line := range m.Before {
			lineNo := startBefore + j
			if hasContext {
				fp(w, "%d-%s\n", lineNo, line)
			}
		}
		// The match itself.
		if hasContext {
			fp(w, "%d:%s\n", m.Line, m.Text)
		} else {
			fp(w, "%s:%d:%s\n", m.Path, m.Line, m.Text)
		}
		// After-context lines.
		for j, line := range m.After {
			fp(w, "%d-%s\n", m.Line+1+j, line)
		}
	}
	fpn(w)
	fp(w, "%d match(es) across %d file(s) (%d file(s) scanned)\n",
		r.Count, r.FilesWithMatches, r.FilesScanned)
	if r.Cancelled {
		fp(w, "(partial — %s)\n", r.CancellationReason)
	}
}

// printFindMatchesJSON writes the full FindMatchesResult as a single
// JSON document. The wire shape is identical to the MCP tool so
// downstream tooling can consume either output.
func printFindMatchesJSON(w io.Writer, r *search.FindMatchesResult) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// printConfigPaths renders the project-type config search paths as
// a human-readable table. Each entry shows existence (`*` for
// present, ` ` for missing) so users can see whether their config
// is in the right place at a glance.
func printConfigPaths(w io.Writer, entries []projecttype.DiscoveryEntry) {
	for _, e := range entries {
		marker := " "
		if _, err := os.Stat(e.Path); err == nil {
			marker = "*"
		}
		fp(w, "%s %-12s  %s\n", marker, e.Scope, e.Path)
	}
	if len(entries) == 0 {
		fp(w, "(no discovery paths resolvable on this platform)\n")
	}
}

func printConfigPathsJSON(w io.Writer, entries []projecttype.DiscoveryEntry) error {
	type entryJSON struct {
		Scope  string `json:"scope"`
		Path   string `json:"path"`
		Exists bool   `json:"exists"`
	}
	out := make([]entryJSON, len(entries))
	for i, e := range entries {
		_, err := os.Stat(e.Path)
		out[i] = entryJSON{Scope: e.Scope, Path: e.Path, Exists: err == nil}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

// printDetectProject renders one directory's project-type matches as
// a human-readable summary. Empty matches print a "no project type
// detected" line so the user knows the call ran.
func printDetectProject(w io.Writer, path string, matches []projecttype.Match) {
	fp(w, "%s\n", path)
	if len(matches) == 0 {
		fp(w, "  (no project type detected)\n")
		return
	}
	for _, m := range matches {
		fp(w, "  %-16s  via %s\n", m.Type, m.Indicator)
	}
}

func printDetectProjectJSON(w io.Writer, path string, matches []projecttype.Match) error {
	types := make([]string, len(matches))
	for i, m := range matches {
		types[i] = m.Type
	}
	out := struct {
		Path         string              `json:"path"`
		ProjectTypes []string            `json:"project_types"`
		Indicators   []projecttype.Match `json:"indicators"`
	}{
		Path:         path,
		ProjectTypes: types,
		Indicators:   matches,
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

// printFindProjects renders the find-projects walk as a table —
// path on the left, comma-separated types on the right.
func printFindProjects(w io.Writer, r *projecttype.FindResult) {
	for _, p := range r.Projects {
		types := make([]string, len(p.Types))
		for i, t := range p.Types {
			types[i] = t.Type
		}
		fp(w, "%s\t[%s]\n", p.Path, strings.Join(types, ","))
	}
	fpn(w, "")
	fp(w, "%d project(s) found in %.3fs\n", r.Count, r.ElapsedSeconds)
}

func printFindProjectsJSON(w io.Writer, r *projecttype.FindResult) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// printLinesJSON renders the LinesResult plus the resolved path,
// so callers can pipe the output to jq.
func printLinesJSON(w io.Writer, path string, r *search.LinesResult) error {
	type out struct {
		Path string `json:"path"`
		*search.LinesResult
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out{Path: path, LinesResult: r})
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
