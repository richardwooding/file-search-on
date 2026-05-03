package content

import (
	"bufio"
	"os"
	"regexp"
	"strings"
)

func init() {
	Register(&htmlType{})
}

type htmlType struct{}

func (h *htmlType) Name() string { return "html" }
func (h *htmlType) Extensions() []string { return []string{".html", ".htm", ".xhtml"} }
func (h *htmlType) MagicBytes() [][]byte {
	return [][]byte{
		[]byte("<!DOCTYPE html"),
		[]byte("<!doctype html"),
		[]byte("<html"),
	}
}

var (
	titleRe    = regexp.MustCompile(`(?i)<title[^>]*>(.*?)</title>`)
	htmlLangRe = regexp.MustCompile(`(?i)<html[^>]*\blang\s*=\s*["']?([A-Za-z][\w-]*)`)
)

func (h *htmlType) Attributes(path string) (Attributes, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var sb strings.Builder
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		sb.WriteString(scanner.Text())
		sb.WriteString("\n")
	}
	content := sb.String()

	title := ""
	if m := titleRe.FindStringSubmatch(content); len(m) > 1 {
		title = strings.TrimSpace(m[1])
	}

	attrs := Attributes{
		"title": title,
	}
	if m := htmlLangRe.FindStringSubmatch(content); len(m) > 1 {
		attrs["language"] = m[1]
	}
	return attrs, nil
}
