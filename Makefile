# file-search-on developer Makefile.
#
# The main build / test commands stay as plain `go build` / `go test`
# per CLAUDE.md. This Makefile is the home for ancillary build steps
# that aren't Go — today, the macOS Vision OCR helper (issue #189).

.PHONY: ocr-helper
ocr-helper:
	@if [ "$$(uname -s)" != "Darwin" ]; then \
		echo "ocr-helper: skipping — Vision OCR helper builds only on macOS"; \
		exit 0; \
	fi
	@command -v swiftc >/dev/null || { echo "ocr-helper: swiftc not in PATH (install Xcode Command Line Tools)"; exit 1; }
	@OUT_DIR=$$(go env GOBIN); \
	if [ -z "$$OUT_DIR" ]; then OUT_DIR=$$(go env GOPATH)/bin; fi; \
	mkdir -p $$OUT_DIR; \
	swiftc -O internal/content/ocr/vision-helper/main.swift -o $$OUT_DIR/file-search-on-ocr-helper; \
	echo "ocr-helper: built $$OUT_DIR/file-search-on-ocr-helper"
