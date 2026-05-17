package celexpr

import "time"

// ApplyFileTimesForTest exposes applyFileTimes to package-external
// tests so they can exercise the anomaly predicate without going
// through the OS / filesystem (where APFS / Chtimes quirks make the
// anomaly hard to synthesise).
func ApplyFileTimesForTest(attrs *FileAttributes, created, metadataChanged time.Time) {
	applyFileTimes(attrs, fileTimesInfo{
		created:         created,
		metadataChanged: metadataChanged,
	})
}
