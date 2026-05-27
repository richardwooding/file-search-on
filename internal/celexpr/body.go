package celexpr

import (
	"context"
	"io"
	"io/fs"
	"strings"
	"time"

	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/content/ocr"
	"github.com/richardwooding/file-search-on/internal/cryptohash"
	"github.com/richardwooding/file-search-on/internal/embed"
	"github.com/richardwooding/file-search-on/internal/fingerprint"
	"github.com/richardwooding/file-search-on/internal/hashset"
	"github.com/richardwooding/file-search-on/internal/index"
)

// isTextForBody reports whether the given content type's body is
// readable as plain text and worth surfacing for CEL body-content
// filters via a raw byte read. Kept in sync with the snippet reader's
// text-type allowlist (internal/search/snippet.go).
//
// Structured-document types (office/* and epub) ALSO populate body
// but via a format-specific extractor — see isStructuredBody +
// content.ExtractBody. The two checks are split because the read
// path is fundamentally different (raw byte slice vs ZIP-walking
// XML extractor) and only the plain-text path supports the streaming
// LimitReader semantics.
func isTextForBody(name string) bool {
	switch name {
	case "markdown", "text", "html", "csv", "json", "xml", "yaml", "toml":
		return true
	}
	return strings.HasPrefix(name, "source/")
}

// isStructuredBody reports whether the given content type's body is
// best surfaced via a format-specific extractor rather than a raw byte
// read. Office documents (DOCX / XLSX / PPTX / ODT) and EPUB are ZIP
// envelopes with body text buried in XML; .eml / .mbox are RFC 5322
// messages with the body buried under MIME headers + transfer-encoding
// + multipart boundaries; PDF carries body text inside content streams
// behind font / encoding indirection. Agents searching these files
// want the human-readable text, not the wire envelope. Routed through
// content.ExtractBody at read time.
func isStructuredBody(name string) bool {
	switch name {
	case "office/docx", "office/xlsx", "office/pptx", "office/odt",
		"epub",
		"email/rfc822", "email/mbox",
		"pdf",
		"database/sqlite",
		"browser/bookmarks-chromium", "browser/bookmarks-safari",
		"chat/slack-export", "chat/discord-export", "chat/signal-cli":
		return true
	}
	return false
}

// canExtractBody reports whether the body CEL variable can be
// populated for this content type — either as raw text (isTextForBody)
// or via a format-specific extractor (isStructuredBody). The walker
// uses this to gate the body read.
func canExtractBody(name string) bool {
	return isTextForBody(name) || isStructuredBody(name)
}

// populateSimilarity fills FileAttributes.Similarity with the
// cosine similarity between the file's body embedding and the
// caller's pre-embedded query (issue #151).
//
// Cache-aware: when the cache holds a non-empty Vector for the
// file, the cosine is computed directly and the embed-hit counter
// bumps. On miss, the file's body is read via lookupOrExtractBody
// (which itself is cache-aware on the body cache), embedded via
// opts.Embedder.Embed, normalised, and stored back to the cache.
// The query embedding is assumed to be normalised already (caller
// responsibility); both vectors normalised => Dot == cosine, the
// O(d) fast path.
//
// No-ops when:
//   - opts.SemanticQueryEmbedding is empty (no query in this walk)
//   - opts.Embedder is nil (search isn't configured)
//   - the content type isn't text-shaped (canExtractBody returns
//     false) — semantic search across binary content makes no sense
//   - the file body is empty (no signal to embed)
//   - the Embedder returns an error (counter bumps; Similarity stays 0)
//
// Stats bumps:
//   - "hit": cached Vector reused, no Embedder call
//   - "miss": cached Vector missing, Embedder call attempted
//   - "put": cached Vector freshly stored
//   - "error": Embedder.Embed returned a non-nil error
//   - "model_mismatch": cached Vector existed but EmbedModel differs
//     from opts.Embedder.Model() (or EmbedModel is empty — unknown
//     provenance, never trust). Falls through to re-embed; does NOT
//     also bump "miss" (the model mismatch IS the miss reason).
func populateSimilarity(ctx context.Context, fsys fs.FS, fsPath, displayPath, cacheKey string, info fs.FileInfo, cached *index.Entry, attrs *FileAttributes, opts BuildOptions) {
	if attrs == nil {
		return
	}
	if opts.Embedder == nil || len(opts.SemanticQueryEmbedding) == 0 {
		return
	}
	if !canExtractBody(attrs.ContentType) {
		return
	}

	currentModel := opts.Embedder.Model()

	// Cache hit path — three sub-cases:
	//
	//   1. Vector + EmbedModel match the current model → reuse, Dot
	//      gives cosine (both pre-normalised). "hit".
	//   2. Vector present but EmbedModel differs (or is empty —
	//      pre-#154 provenance) → bump "model_mismatch" and fall
	//      through to re-embed. We can't trust the cached vector
	//      because dimensions and/or coordinate systems differ across
	//      models; computing a dot product would return either 0
	//      (length mismatch) or nonsense (same dim, different model).
	//   3. Vector empty → bump "miss" below in the re-embed path.
	if cached != nil && len(cached.Vector) > 0 {
		if cached.EmbedModel == currentModel && currentModel != "" {
			attrs.Similarity = embed.Dot(opts.SemanticQueryEmbedding, cached.Vector)
			if opts.Index != nil {
				opts.Index.BumpEmbedStat("hit")
			}
			return
		}
		if opts.Index != nil {
			opts.Index.BumpEmbedStat("model_mismatch")
		}
		// fall through to re-embed; don't also bump "miss".
	}

	// Re-embed path. Extract the body via the body-cache-aware reader
	// so we share the body cache with anything else asking for it in
	// this walk. Empty body → no signal → leave Similarity at 0.
	body, err := readBody(ctx, fsys, fsPath, attrs.Path, attrs.ContentType, opts.BodyMaxBytes)
	if err != nil || body == "" {
		if opts.Index != nil && (cached == nil || len(cached.Vector) == 0) {
			// Pure cache-miss (no Vector at all). When we got here
			// via model_mismatch we've already bumped that counter
			// and shouldn't also bump "miss".
			opts.Index.BumpEmbedStat("miss")
		}
		return
	}
	if opts.Index != nil && (cached == nil || len(cached.Vector) == 0) {
		opts.Index.BumpEmbedStat("miss")
	}
	vec, err := opts.Embedder.Embed(ctx, body)
	if err != nil || len(vec) == 0 {
		if opts.Index != nil {
			opts.Index.BumpEmbedStat("error")
		}
		return
	}
	embed.Normalize(vec)
	attrs.Similarity = embed.Dot(opts.SemanticQueryEmbedding, vec)

	// Write the freshly-computed vector back to the cache so the
	// next walk against the same (size, mtime, model) skips the
	// Embedder call. Stamp EmbedModel so future calls with a
	// different model surface as model_mismatch instead of silently
	// returning wrong scores.
	if opts.Index != nil && cacheKey != "" {
		entry := cached
		if entry == nil {
			entry = &index.Entry{
				Size:            info.Size(),
				ModTimeUnixNano: info.ModTime().UnixNano(),
				ContentType:     attrs.ContentType,
			}
		}
		entry.Vector = vec
		entry.EmbedModel = currentModel
		_ = opts.Index.Put(cacheKey, entry)
		opts.Index.BumpEmbedStat("put")
	}
}

// applyKnownStatus checks the file's MD5 / SHA1 / SHA256 against
// the loaded allowlist / denylist hashsets and sets the IsKnownGood
// / IsKnownBad flags on attrs. Membership in ANY of the three
// algorithms is sufficient — NSRL ships MD5 + SHA1; threat-intel
// feeds tend to use MD5 or SHA1; modern tools index by SHA256.
//
// Called from BuildAttributesWith AFTER populateHashes so the
// hashes on attrs are populated. No-op when both lists are nil.
func applyKnownStatus(attrs *FileAttributes, opts BuildOptions) {
	if attrs == nil {
		return
	}
	if opts.Allowlist != nil {
		if anyHashIn(opts.Allowlist, attrs.MD5, attrs.SHA1, attrs.SHA256) {
			attrs.IsKnownGood = true
		}
	}
	if opts.Denylist != nil {
		if anyHashIn(opts.Denylist, attrs.MD5, attrs.SHA1, attrs.SHA256) {
			attrs.IsKnownBad = true
		}
	}
}

// anyHashIn returns true when any of md5 / sha1 / sha256 (skipping
// empty strings) is a member of the given Set.
func anyHashIn(set hashset.Set, md5, sha1, sha256 string) bool {
	if md5 != "" && set.Contains("md5", md5) {
		return true
	}
	if sha1 != "" && set.Contains("sha1", sha1) {
		return true
	}
	if sha256 != "" && set.Contains("sha256", sha256) {
		return true
	}
	return false
}

// applyDisguise writes the magic-vs-extension content-type strings
// onto attrs and sets IsDisguised when both are non-empty and they
// disagree. Used by the CheckDisguised opt-in path (PR #145).
func applyDisguise(attrs *FileAttributes, magicCT, extCT string) {
	if attrs == nil {
		return
	}
	attrs.MagicContentType = magicCT
	attrs.ExtensionContentType = extCT
	if magicCT != "" && extCT != "" && magicCT != extCT {
		attrs.IsDisguised = true
	}
}

// redetectDisguise calls registry.DetectBoth and returns the two
// content-type names (magic, extension). Used on the cache-hit path
// when the cached entry lacks the disguise fields (pre-#145 cache
// or CheckDisguised wasn't set in the prior walk).
func redetectDisguise(fsys fs.FS, fsPath string, registry *content.Registry) (magicCT, extCT string) {
	nameType, magicType := registry.DetectBoth(fsys, fsPath)
	if nameType != nil {
		extCT = nameType.Name()
	}
	if magicType != nil {
		magicCT = magicType.Name()
	}
	return magicCT, extCT
}

// populateHashes fills FileAttributes.{MD5,SHA1,SHA256} when the
// caller opts in via BuildOptions.ComputeHashes. Cache-aware: when
// cached is non-nil and carries all three hashes, no file read
// happens; otherwise cryptohash.File reads the file once and stores
// the trio in the cache for the next call.
//
// cacheKey may be empty (tests with relative paths or no index);
// that just skips the Put — the live hashes still surface on attrs.
//
// Errors degrade silently — a file we can't read just leaves the
// hashes empty. The CEL filter then sees `md5 == ""` which won't
// match any forensic hash query.
func populateHashes(ctx context.Context, displayPath, cacheKey string, info fs.FileInfo, cached *index.Entry, attrs *FileAttributes, idx index.Index) {
	if cached != nil && cached.MD5 != "" && cached.SHA1 != "" && cached.Hash != "" {
		attrs.MD5 = cached.MD5
		attrs.SHA1 = cached.SHA1
		attrs.SHA256 = cached.Hash
		return
	}
	trio, err := cryptohash.File(ctx, displayPath)
	if err != nil {
		return
	}
	attrs.MD5 = trio.MD5
	attrs.SHA1 = trio.SHA1
	attrs.SHA256 = trio.SHA256
	if idx != nil && cacheKey != "" {
		entry := cached
		if entry == nil {
			entry = &index.Entry{
				Size:            info.Size(),
				ModTimeUnixNano: info.ModTime().UnixNano(),
				ContentType:     attrs.ContentType,
			}
		}
		entry.MD5 = trio.MD5
		entry.SHA1 = trio.SHA1
		entry.Hash = trio.SHA256
		_ = idx.Put(cacheKey, entry)
	}
}

// populatePHash fills attrs.Extra["phash"] (16-char hex) with the
// perceptual hash of the image, when:
//   - the file is image-shaped (contentTypeName starts with "image/")
//   - opts.WithPHash is set OR the CEL expression references
//     image_similar_to(...) — the caller sets WithPHash accordingly
//
// Cache-aware: when the cached entry carries a non-zero PHash, it's
// reused without re-decoding the image; otherwise the image is
// decoded + hashed and the result Put back to the cache.
//
// Errors degrade silently — an unreadable image leaves phash empty.
// Issue #208.
func populatePHash(ctx context.Context, fsys fs.FS, fsPath, displayPath, cacheKey, contentTypeName string, info fs.FileInfo, cached *index.Entry, extra content.Attributes, idx index.Index) {
	if err := ctx.Err(); err != nil {
		return
	}
	if !strings.HasPrefix(contentTypeName, "image/") {
		return
	}
	// Cache hit — reuse without decoding the image.
	if cached != nil && cached.PHash != 0 {
		extra["phash"] = fingerprint.PHashHex(cached.PHash)
		return
	}
	f, err := fsys.Open(fsPath)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	hash, err := fingerprint.PHash(f)
	if err != nil || hash == 0 {
		return
	}
	extra["phash"] = fingerprint.PHashHex(hash)
	if idx != nil && cacheKey != "" {
		entry := cached
		if entry == nil {
			entry = &index.Entry{
				Size:            info.Size(),
				ModTimeUnixNano: info.ModTime().UnixNano(),
				ContentType:     contentTypeName,
			}
		}
		entry.PHash = hash
		_ = idx.Put(cacheKey, entry)
	}
}

// defaultOCRTimeout is the per-file OCR ceiling when
// BuildOptions.OCRTimeout is unset. 10 seconds matches the issue #189
// design — a typical screenshot OCRs in 200ms-2s; pathological images
// (huge resolution, dense glyphs) can take longer. Beyond 10s the
// helper process gets SIGKILL via ctx cancellation.
const defaultOCRTimeout = 10 * time.Second

// runImageOCR runs the registered OCR provider over an image file and
// stamps the result (text + confidence + language + provider) into
// extras. Cache-aware on bodies_v1 the same way lookupOrExtractBody is:
// a hit returns the cached text without re-running the helper; a miss
// runs the helper and PutBody's the result.
//
// Caller contract: only call when opts.OCRImages is true AND the
// content type is `image/*`. The function gates on ocr.HasProvider()
// internally so callers on non-target platforms still no-op cleanly.
//
// Three extras get stamped:
//   - "body" — the recognized text (joined lines, newline-separated).
//   - "ocr_confidence" — average per-line confidence (float64, 0..1).
//   - "ocr_language" — BCP-47 dominant language ("en" / "ja" / "zh-Hans") or
//     empty when the recognizer couldn't decide.
//   - "ocr_provider" — registered provider name ("vision-macos").
//
// On any error (helper not found, ctx timeout, parse failure), the
// function returns silently — extras are left as-is, matching the
// "best effort, empty on failure" contract of every other body
// extractor.
//
// Issue #189.
func runImageOCR(ctx context.Context, displayPath, cacheKey string, info fs.FileInfo, extras content.Attributes, opts BuildOptions) {
	if !ocr.HasProvider() {
		return
	}
	if displayPath == "" {
		// In-memory test filesystems / archive-walk paths can't be
		// handed to the helper — it needs a real OS path.
		return
	}

	// Body-cache fast path. On hit, the text body comes back without a
	// helper call; the OCR extras (confidence / language / provider)
	// live in the attrs_v1 cache alongside the rest of Extra, so the
	// cache-hit path at BuildAttributesWith's assembleFromCache returns
	// them for free.
	if opts.Index != nil && cacheKey != "" {
		if body, ok := opts.Index.LookupBody(cacheKey, info.Size(), info.ModTime()); ok {
			if body != "" {
				extras["body"] = body
			}
			return
		}
	}

	p := ocr.Default()
	if p == nil {
		return
	}

	timeout := opts.OCRTimeout
	if timeout <= 0 {
		timeout = defaultOCRTimeout
	}
	ocrCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	result, err := p.Recognize(ocrCtx, displayPath)
	if err != nil {
		return
	}
	if result.Text != "" {
		extras["body"] = result.Text
	}
	extras["ocr_confidence"] = result.Confidence
	if result.Language != "" {
		extras["ocr_language"] = result.Language
	}
	if result.Provider != "" {
		extras["ocr_provider"] = result.Provider
	}

	// Cache the body for the next walk. The attrs cache (attrs_v1) is
	// written separately by BuildAttributesWith after this function
	// returns; the OCR extras above land in extras → Extra → attrs_v1.
	if opts.Index != nil && cacheKey != "" && result.Text != "" {
		_ = opts.Index.PutBody(cacheKey, &index.BodyEntry{
			Size:            info.Size(),
			ModTimeUnixNano: info.ModTime().UnixNano(),
			CreatedUnixNano: time.Now().UnixNano(),
			Body:            result.Text,
		})
	}
}

// lookupOrExtractBody is the cache-aware body reader. When the
// caller's BuildOptions carries a non-nil Index AND a valid cache
// key, it tries LookupBody first; a hit returns the cached body
// without re-extracting (the bbolt cache also touches the access
// timestamp internally for LRU eviction). A miss runs readBody and
// asynchronously Puts the result so the next call against the same
// (path, size, mtime) hits.
//
// Bodies for paths that aren't validly absolute (in-memory test
// filesystems with displayPath="") bypass the cache — there's no
// stable cache key. The caller still gets a freshly-extracted body.
//
// Returns "" on extraction error OR empty body — same contract as
// the prior inline readBody calls; callers check empty-string before
// writing into attrs.Extra.
func lookupOrExtractBody(ctx context.Context, fsys fs.FS, fsPath, displayPath, cacheKey string, info fs.FileInfo, contentTypeName string, opts BuildOptions) string {
	if opts.Index != nil && cacheKey != "" {
		if body, ok := opts.Index.LookupBody(cacheKey, info.Size(), info.ModTime()); ok {
			return body
		}
	}
	body, err := readBody(ctx, fsys, fsPath, displayPath, contentTypeName, opts.BodyMaxBytes)
	if err != nil || body == "" {
		return ""
	}
	if opts.Index != nil && cacheKey != "" {
		_ = opts.Index.PutBody(cacheKey, &index.BodyEntry{
			Size:            info.Size(),
			ModTimeUnixNano: info.ModTime().UnixNano(),
			CreatedUnixNano: time.Now().UnixNano(),
			Body:            body,
		})
	}
	return body
}

// readBody returns the file's body as a string capped at maxBytes.
// When maxBytes <= 0 the package default (1 MiB) is used. The cap is a
// hard limit: files larger than the cap are silently truncated, not
// rejected — agents writing `body.contains("X")` filters typically
// want the prefix to participate even if the file is enormous.
//
// Dispatch:
//   - text-shaped types (markdown / text / html / csv / json / xml /
//     source/*) read raw bytes via io.LimitReader.
//   - structured types (office/* / epub) call content.ExtractBody for
//     a format-specific extractor that strips XML / ZIP envelope and
//     returns the human-readable text only.
//
// ctx is checked at entry; structured extractors honour ctx between
// every XML token internally. Raw reads will surface ctx.Err()
// through the underlying file IO eventually but don't poll between
// bytes.
func readBody(ctx context.Context, fsys fs.FS, fsPath, displayPath, contentTypeName string, maxBytes int) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if maxBytes <= 0 {
		maxBytes = defaultBodyMaxBytes
	}
	if isStructuredBody(contentTypeName) {
		// SQLite extraction goes through the modernc.org/sqlite driver
		// which only opens real OS paths. Archive-walk callers leave
		// displayPath either empty or set to the in-archive virtual
		// path; we accept any non-empty displayPath and let the driver
		// error fast if it can't open. This is the same contract as
		// every other structured extractor — "best effort, empty on
		// failure".
		if content.RequiresOSPath(contentTypeName) {
			if displayPath == "" {
				return "", nil
			}
			return content.ExtractBodyOSPath(ctx, contentTypeName, displayPath, maxBytes)
		}
		return content.ExtractBody(ctx, contentTypeName, fsys, fsPath, maxBytes)
	}
	f, err := fsys.Open(fsPath)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	// LimitReader caps the underlying read; ReadAll then collects
	// up to that many bytes. We add 1 to the cap so we can detect
	// "truncated" if we ever want to (we don't currently surface
	// that distinction — see the doc above).
	b, err := io.ReadAll(io.LimitReader(f, int64(maxBytes)))
	if err != nil {
		return "", err
	}
	return string(b), nil
}
