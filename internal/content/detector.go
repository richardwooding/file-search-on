package content

import (
	"bytes"
	"io"
	"io/fs"
	"path"
	"strings"
)

// FilenameMatcher is an optional interface a ContentType can implement
// to advertise exact basenames it matches — for files like Dockerfile,
// Makefile, LICENSE, .gitignore, go.mod that have no useful extension.
// Detection by exact name takes precedence over extension matching, so
// e.g. package.json detects as manifest/node (more specific) rather
// than generic json. Comparison is case-insensitive, matching the
// existing extension-detection convention.
type FilenameMatcher interface {
	Filenames() []string
}

// ContentDiscriminator is an optional interface for content types that
// share an extension with a generic type (e.g. chat exports vs plain
// `.json`) and have no fixed basename to match on. Such types can't win
// via extension matching alone — `.json` always resolves to the generic
// json type. The detector consults discriminators only when extension
// matching would otherwise yield a generic type, reading the file head
// once and asking each discriminator whose advertised extension matches.
//
// Implementations should inspect STRUCTURE (e.g. top-level JSON keys via
// a streaming json.Decoder), not byte prefixes, and must never buffer
// the whole file — head is already a bounded prefix.
type ContentDiscriminator interface {
	// DiscriminatorExtensions returns the extensions this type
	// discriminates within (lowercase, dotted, e.g. ".json").
	DiscriminatorExtensions() []string
	// MatchesContent reports whether head (a prefix of the file) has
	// this type's structure.
	MatchesContent(head []byte) bool
}

// discriminatorReadCap bounds the head read for the content-discriminator
// tier. Generous enough to contain the top-level keys of a realistic
// chat export (guild / channel metadata precedes the messages array)
// while staying cheap — it only fires for would-be-generic JSON files.
const discriminatorReadCap = 64 << 10

// Detect detects the content type of a file using extension first, then
// magic bytes. Path is an fs.FS-style key (forward slashes); fsys is the
// filesystem that provides Open access for magic-byte sniffing when no
// extension matches. fsys may be nil when the caller knows extension
// matching alone is sufficient.
func (r *Registry) Detect(fsys fs.FS, p string) ContentType {
	r.mu.RLock()
	types := make([]ContentType, len(r.types))
	copy(types, r.types)
	r.mu.RUnlock()

	// Exact-basename pass first. Lets types like build/dockerfile,
	// repo/license, manifest/gomod identify files that either have no
	// extension at all (LICENSE, .gitignore, Dockerfile) or whose
	// extension would otherwise dispatch to a less-specific parser
	// (package.json → manifest/node, not json).
	base := path.Base(p)
	for _, ct := range types {
		fm, ok := ct.(FilenameMatcher)
		if !ok {
			continue
		}
		for _, name := range fm.Filenames() {
			if strings.EqualFold(base, name) {
				return ct
			}
		}
	}

	// Two-pass extension match: prefer multi-component suffixes (e.g.
	// ".tar.gz") over single-component fallbacks (".gz") so registered
	// types like archive/tar+gzip win against archive/gzip when both
	// match. The longest registered extension that case-insensitively
	// suffix-matches the path wins.
	pLower := strings.ToLower(p)
	var best ContentType
	bestLen := 0
	for _, ct := range types {
		for _, e := range ct.Extensions() {
			if !strings.HasSuffix(pLower, e) {
				continue
			}
			if len(e) > bestLen {
				best = ct
				bestLen = len(e)
			}
		}
	}
	// Content-discriminator tier: when extension matching yielded only
	// the generic `json` type (or nothing), a more-specific type may
	// still claim the file by inspecting its structure — e.g. a Slack /
	// Discord / Signal chat export is a plain `.json` file with no fixed
	// basename. Read the head once and ask each discriminator whose
	// advertised extension matches.
	if fsys != nil && (best == nil || best.Name() == "json") {
		if ct := discriminate(fsys, p, pLower, types); ct != nil {
			return ct
		}
	}
	if best != nil {
		return best
	}
	if fsys == nil {
		return nil
	}
	// Defensive: stat before opening so we don't block on an
	// unconnected FIFO / socket / character device. The walker filters
	// these out earlier, but other callers (single-file tools like
	// read_attributes) reach here too.
	if info, err := fs.Stat(fsys, p); err == nil {
		typ := info.Mode().Type()
		if typ&(fs.ModeNamedPipe|fs.ModeSocket|fs.ModeDevice|fs.ModeCharDevice|fs.ModeIrregular) != 0 {
			return nil
		}
	}
	f, err := fsys.Open(p)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	buf := make([]byte, 512)
	n, err := f.Read(buf)
	if err != nil && err != io.EOF {
		return nil
	}
	buf = buf[:n]
	for _, ct := range types {
		for _, magic := range ct.MagicBytes() {
			if bytes.HasPrefix(buf, magic) {
				return ct
			}
		}
	}
	return nil
}

// discriminate runs the content-discriminator tier: for each registered
// ContentDiscriminator whose advertised extension suffix-matches pLower,
// it reads the file head once and returns the first type whose
// MatchesContent reports a structural match. Returns nil when no
// discriminator claims the file (the caller falls back to the generic
// extension match). The head is read at most once, lazily, only when a
// candidate discriminator exists.
func discriminate(fsys fs.FS, p, pLower string, types []ContentType) ContentType {
	var candidates []ContentDiscriminator
	for _, ct := range types {
		d, ok := ct.(ContentDiscriminator)
		if !ok {
			continue
		}
		for _, e := range d.DiscriminatorExtensions() {
			if strings.HasSuffix(pLower, e) {
				candidates = append(candidates, d)
				break
			}
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	f, err := fsys.Open(p)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	head, err := io.ReadAll(io.LimitReader(f, discriminatorReadCap))
	if err != nil {
		return nil
	}
	for _, d := range candidates {
		if d.MatchesContent(head) {
			return d.(ContentType)
		}
	}
	return nil
}

// DetectBoth runs the name-based passes (exact-basename, then
// extension) AND the magic-byte pass independently, returning both
// results. Either or both may be nil. Used by the disguise-detection
// path (BuildOptions.CheckDisguised) so callers can compare what the
// file CLAIMS to be (by name) with what its bytes actually look like
// (by magic).
//
// Unlike Detect, which short-circuits at the first matching tier,
// DetectBoth always performs the magic sniff when fsys is non-nil —
// the extra 512-byte read is exactly the cost of forensic-mode
// detection. Callers that don't want it shouldn't call DetectBoth.
//
// nameType matches Detect's logic up through the extension pass:
// exact-basename via FilenameMatcher first, then longest-suffix
// extension match. magicType is whatever the magic-byte sniff alone
// would return.
func (r *Registry) DetectBoth(fsys fs.FS, p string) (nameType, magicType ContentType) {
	r.mu.RLock()
	types := make([]ContentType, len(r.types))
	copy(types, r.types)
	r.mu.RUnlock()

	// Name-based passes — mirror Detect.
	base := path.Base(p)
	for _, ct := range types {
		fm, ok := ct.(FilenameMatcher)
		if !ok {
			continue
		}
		for _, name := range fm.Filenames() {
			if strings.EqualFold(base, name) {
				nameType = ct
				break
			}
		}
		if nameType != nil {
			break
		}
	}
	if nameType == nil {
		pLower := strings.ToLower(p)
		bestLen := 0
		for _, ct := range types {
			for _, e := range ct.Extensions() {
				if !strings.HasSuffix(pLower, e) {
					continue
				}
				if len(e) > bestLen {
					nameType = ct
					bestLen = len(e)
				}
			}
		}
	}

	// Magic pass — always runs when fsys is non-nil.
	if fsys == nil {
		return nameType, nil
	}
	f, err := fsys.Open(p)
	if err != nil {
		return nameType, nil
	}
	defer func() { _ = f.Close() }()
	buf := make([]byte, 512)
	n, err := f.Read(buf)
	if err != nil && err != io.EOF {
		return nameType, nil
	}
	buf = buf[:n]
	for _, ct := range types {
		for _, magic := range ct.MagicBytes() {
			if bytes.HasPrefix(buf, magic) {
				magicType = ct
				return nameType, magicType
			}
		}
	}
	return nameType, nil
}
