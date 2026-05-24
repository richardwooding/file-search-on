package content

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"howett.net/plist"
)

// Pure parsers for the macOS xattr value formats. Lives in a non-
// build-tagged file because the parsing logic is platform-independent
// (string + plist work) — only the syscall surface in xattrs_darwin.go
// is platform-specific. Splitting them keeps the fuzz target +
// parser-level unit tests reachable from Linux / Windows CI runners.

// LSQuarantineFlags bits surfaced as the `quarantine_user_approved`
// predicate per Apple's LaunchServices LSQuarantine* constants.
const (
	lsQuarantineFlagAlreadyApproved = 0x40
	lsQuarantineFlagUserApproved    = 0x80
)

// parseQuarantineValue decodes the semicolon-delimited
// com.apple.quarantine value into typed fields.
//
// Layout per Apple's LSQuarantine* constants:
//
//	<flags-hex>;<timestamp-hex>;<agent-name>;<agent-id>[;<source-url>]
//
// Field 4 (`agent-id`) is documented as the agent's bundle identifier
// but modern browsers populate it with a per-event UUID instead — the
// real bundle id + source URL live in a separate `kMDItemWhereFroms`
// xattr. We surface field 4 as `quarantine_event_id`.
//
// Field 5 (`source-url`) is optional and rarely populated on modern
// downloads — `kMDItemWhereFroms` is the canonical surface.
func parseQuarantineValue(value string) (flags uint64, downloadTime time.Time, agent, eventID, sourceURL string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, time.Time{}, "", "", ""
	}
	// SplitN with n=5 lets the URL contain `;` characters (modern
	// Google Docs export URLs include semicolons in query strings).
	parts := strings.SplitN(value, ";", 5)
	for i, p := range parts {
		parts[i] = strings.TrimSpace(p)
	}

	if len(parts) >= 1 && parts[0] != "" {
		if v, err := strconv.ParseUint(parts[0], 16, 64); err == nil {
			flags = v
		}
	}
	if len(parts) >= 2 && parts[1] != "" {
		// Modern macOS quarantine timestamps are plain Unix-epoch
		// seconds in hex (verified against `ls -la` and `stat -f %Sm`
		// on real downloads under ~/Downloads). Apple's older docs
		// referenced CFAbsoluteTime / Mac Absolute Time but the
		// filesystem attribute writers — Safari, Chrome, mail
		// clients, etc. — all use Unix epoch.
		if v, err := strconv.ParseInt(parts[1], 16, 64); err == nil {
			downloadTime = time.Unix(v, 0).UTC()
		}
	}
	if len(parts) >= 3 {
		agent = parts[2]
	}
	if len(parts) >= 4 {
		eventID = parts[3]
	}
	if len(parts) >= 5 {
		sourceURL = parts[4]
	}
	return flags, downloadTime, agent, eventID, sourceURL
}

// mergeQuarantineAttrs decodes the quarantine value and populates the
// Attributes map. Always sets is_quarantined when the xattr is
// present (even if individual fields are empty). The source URL from
// field 5 wins only if mergeWhereFromsAttrs didn't already set one
// from kMDItemWhereFroms (which is the canonical modern surface).
func mergeQuarantineAttrs(out Attributes, value []byte) {
	out["is_quarantined"] = true
	flags, downloadTime, agent, eventID, sourceURL := parseQuarantineValue(string(value))
	if agent != "" {
		out["quarantine_agent"] = agent
	}
	if eventID != "" {
		out["quarantine_event_id"] = eventID
	}
	if sourceURL != "" {
		if _, exists := out["quarantine_source_url"]; !exists {
			out["quarantine_source_url"] = sourceURL
		}
	}
	if !downloadTime.IsZero() {
		out["quarantine_download_date"] = downloadTime
	}
	if flags&(lsQuarantineFlagUserApproved|lsQuarantineFlagAlreadyApproved) != 0 {
		out["quarantine_user_approved"] = true
	}
}

// mergeWhereFromsAttrs decodes the kMDItemWhereFroms binary plist
// (an array of URL strings — typically the document URL + the page
// the user was on when they clicked) and surfaces them as
// `quarantine_source_url` (first element, the canonical download
// URL) and `quarantine_referrer_url` (second element, the page that
// linked to it).
func mergeWhereFromsAttrs(out Attributes, value []byte) {
	var root any
	if _, err := plist.Unmarshal(value, &root); err != nil {
		return
	}
	arr, ok := root.([]any)
	if !ok {
		return
	}
	urls := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			s = strings.TrimSpace(s)
			if s != "" {
				urls = append(urls, s)
			}
		}
	}
	if len(urls) > 0 {
		out["quarantine_source_url"] = urls[0]
	}
	if len(urls) > 1 {
		out["quarantine_referrer_url"] = urls[1]
	}
}

// finderColorNames maps the integer suffix on a Finder color tag to
// Apple's canonical short name. Per CoreServices' kMDItemUserTags
// convention: 0 = none, 1 = gray, 2 = green, 3 = purple, 4 = blue,
// 5 = yellow, 6 = red, 7 = orange.
var finderColorNames = []string{
	"", "gray", "green", "purple", "blue", "yellow", "red", "orange",
}

// mergeFinderTagAttrs decodes the _kMDItemUserTags binary plist
// (an array of strings) and surfaces both color-tag and user-tag
// surfaces. finder_color is set to the dominant color tag's short
// name (first one wins when multiple colors are applied).
func mergeFinderTagAttrs(out Attributes, value []byte) {
	var root any
	if _, err := plist.Unmarshal(value, &root); err != nil {
		return
	}
	arr, ok := root.([]any)
	if !ok {
		return
	}
	var tags []string
	var color string
	for _, item := range arr {
		s, ok := item.(string)
		if !ok {
			continue
		}
		if nl := strings.IndexByte(s, '\n'); nl >= 0 {
			label := s[:nl]
			suffix := s[nl+1:]
			if idx, err := strconv.Atoi(suffix); err == nil && idx >= 0 && idx < len(finderColorNames) {
				if color == "" {
					color = finderColorNames[idx]
				}
				if label != "" {
					tags = append(tags, label)
				}
				continue
			}
		}
		s = strings.TrimSpace(s)
		if s != "" {
			tags = append(tags, s)
		}
	}
	if len(tags) > 0 {
		sort.Strings(tags)
		out["finder_tags"] = tags
	}
	if color != "" {
		out["finder_color"] = color
	}
}
