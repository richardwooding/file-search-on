package search

// skipPrefixesForProfile returns the content-type-name prefixes whose
// per-format Attributes() parse should be skipped under the named
// profile. Detection still runs (so ContentType + the is_X family
// flags populate), but the expensive per-format parsing AND the
// attribute-cache write are bypassed. nil means "no skipping".
//
// v1 supports only "code" — the headline use case from #284: walking
// a tree where source code is signal and media / archive / scientific
// data is noise. The skip list inverts the keep-set (source/*,
// markdown, text-shaped formats, repo / build / manifest metadata).
// Categories left implicit because they aren't worth a stub: tree of
// media + tree of binaries can fall back to the default 'no profile'
// walk; the v1 ask was specifically code-only. Issue #284.
func skipPrefixesForProfile(profile string) []string {
	switch profile {
	case "code":
		return []string{
			// Media — image / audio / video parses dominate the
			// per-file cost on mixed trees.
			"image/",
			"audio/",
			"video/",
			// Compiled / packaged binaries — heavyweight section
			// walkers (ELF / Mach-O / PE / class / pyc / wasm).
			"binary/",
			"bytecode/",
			"install/",
			"disk-image/",
			// Container formats — ZIP / TAR walks + per-entry
			// re-detection.
			"archive/",
			"office/",
			"epub",
			// Records / structured data heavy on parse:
			"email/",
			"science/",
			"database/",
			"font/",
			"3d/",
			// Browser / chat / bookmark exports — rich but not
			// code-relevant.
			"chat/",
			"browser/",
			"bookmark/",
		}
	}
	return nil
}

// matchesSkipPrefix reports whether contentTypeName starts with any
// of the prefixes — the per-file gate inside BuildAttributesWith.
// Empty prefixes / empty content type → false (no skip).
func matchesSkipPrefix(contentTypeName string, prefixes []string) bool {
	if contentTypeName == "" || len(prefixes) == 0 {
		return false
	}
	for _, p := range prefixes {
		if len(contentTypeName) >= len(p) && contentTypeName[:len(p)] == p {
			return true
		}
	}
	return false
}
