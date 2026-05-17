package content

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"io/fs"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"strings"
)

// emlBody extracts the human-readable text body from a single RFC 5322
// message. Walks the MIME tree: for non-multipart messages decodes the
// content-transfer encoding (quoted-printable / base64 / identity) and
// returns the decoded text; for multipart/alternative prefers
// text/plain over text/html; for multipart/mixed or related
// concatenates every text part, skipping attachments. text/html parts
// flow through extractHTMLText to strip tags.
//
// Headers (Subject / From / etc.) are NOT included — those already
// surface as separate CEL variables (title / author / email_to /
// email_message_id / ...). This extractor is specifically the message
// body that an agent would search for content.
func emlBody(ctx context.Context, fsys fs.FS, filePath string, maxBytes int) (string, error) {
	f, err := fsys.Open(filePath)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	return emlBodyFromReader(ctx, f, maxBytes)
}

// emlBodyFromReader parses one RFC 5322 message from r and returns its
// text body. Shared by emlBody (file path) and mboxBody (per-message
// in-memory buffer). Malformed message → empty body + nil error.
func emlBodyFromReader(ctx context.Context, r io.Reader, maxBytes int) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	msg, err := mail.ReadMessage(r)
	if err != nil {
		return "", nil //nolint:nilerr // malformed message → empty body, not error
	}
	return walkEmailBody(ctx, msg.Body, msg.Header.Get("Content-Type"), msg.Header.Get("Content-Transfer-Encoding"), maxBytes)
}

// walkEmailBody is the shared recursive walker — handles a single
// message part. multipart parts recurse via multipart.Reader; text
// parts decode the transfer encoding and (for text/html) strip tags;
// other media types (application/*, image/*, etc.) return "" to skip.
//
// RFC 2045 §5.2: when Content-Type is absent the default is
// "text/plain; charset=us-ascii". We honour that — a header-only
// message with no Content-Type still gets read as text.
func walkEmailBody(ctx context.Context, r io.Reader, contentType, transferEncoding string, maxBytes int) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType == "" {
		mediaType = "text/plain"
	}
	switch {
	case strings.HasPrefix(mediaType, "multipart/"):
		return walkMultipartBody(ctx, r, params["boundary"], mediaType, maxBytes)
	case mediaType == "text/plain":
		return decodeTextPart(ctx, r, transferEncoding, false, maxBytes)
	case mediaType == "text/html":
		return decodeTextPart(ctx, r, transferEncoding, true, maxBytes)
	}
	// application/* / image/* / etc. — not human-readable text. Skip.
	return "", nil
}

// walkMultipartBody walks a multipart container. For
// multipart/alternative, the RFC convention is "each part is a
// different representation of the same content"; agents want plain
// text, so we prefer text/plain and fall back to stripped text/html.
// For other multipart types (mixed / related / parallel / signed),
// every non-attachment text part is concatenated.
func walkMultipartBody(ctx context.Context, r io.Reader, boundary, multipartType string, maxBytes int) (string, error) {
	if boundary == "" {
		return "", nil
	}
	mr := multipart.NewReader(r, boundary)
	if multipartType == "multipart/alternative" {
		var plain, html string
		for {
			if err := ctx.Err(); err != nil {
				break
			}
			part, err := mr.NextPart()
			if err != nil {
				break
			}
			if isAttachmentPart(part) {
				_ = part.Close()
				continue
			}
			pCT := part.Header.Get("Content-Type")
			pCTE := part.Header.Get("Content-Transfer-Encoding")
			body, _ := walkEmailBody(ctx, part, pCT, pCTE, maxBytes)
			_ = part.Close()
			pMediaType, _, _ := mime.ParseMediaType(pCT)
			if pMediaType == "" {
				pMediaType = "text/plain"
			}
			switch {
			case pMediaType == "text/plain" && plain == "":
				plain = body
			case pMediaType == "text/html" && html == "":
				html = body
			}
		}
		if plain != "" {
			return plain, nil
		}
		return html, nil
	}

	// mixed / related / parallel / signed — concatenate text parts.
	var out strings.Builder
	for {
		if err := ctx.Err(); err != nil {
			return out.String(), err
		}
		if maxBytes > 0 && out.Len() >= maxBytes {
			break
		}
		part, err := mr.NextPart()
		if err != nil {
			break
		}
		if isAttachmentPart(part) {
			_ = part.Close()
			continue
		}
		pCT := part.Header.Get("Content-Type")
		pCTE := part.Header.Get("Content-Transfer-Encoding")
		remaining := maxBytes
		if maxBytes > 0 {
			remaining = maxBytes - out.Len()
		}
		body, _ := walkEmailBody(ctx, part, pCT, pCTE, remaining)
		_ = part.Close()
		if body == "" {
			continue
		}
		if out.Len() > 0 {
			out.WriteByte('\n')
		}
		out.WriteString(body)
	}
	return out.String(), nil
}

// decodeTextPart reads a text/plain or text/html part, applying the
// Content-Transfer-Encoding decoder (quoted-printable / base64 are the
// two that matter for real-world email; 7bit / 8bit / binary pass
// through). Unknown encodings read raw bytes — at worst the agent sees
// undecoded MIME-quoted output, never an error. When isHTML is set,
// the decoded bytes are run through extractHTMLText to strip tags
// before returning.
func decodeTextPart(ctx context.Context, r io.Reader, transferEncoding string, isHTML bool, maxBytes int) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	decoded := r
	switch strings.ToLower(strings.TrimSpace(transferEncoding)) {
	case "quoted-printable":
		decoded = quotedprintable.NewReader(r)
	case "base64":
		decoded = base64.NewDecoder(base64.StdEncoding, r)
		// "", "7bit", "8bit", "binary", or anything else — read raw.
	}
	cap := maxBytes
	if cap <= 0 {
		cap = 1 << 20 // 1 MiB local default — matches celexpr.defaultBodyMaxBytes
	}
	if isHTML {
		return extractHTMLText(ctx, decoded, cap)
	}
	b, err := io.ReadAll(io.LimitReader(decoded, int64(cap)))
	if err != nil {
		return strings.TrimRight(string(b), "\r\n "), nil //nolint:nilerr // partial body on transfer-encoding error is fine
	}
	return strings.TrimRight(string(b), "\r\n "), nil
}

// mboxBody walks an mbox archive, extracting each message's body via
// emlBodyFromReader and concatenating with double-newline separators
// so an agent grepping the result can search across the whole inbox.
// Splits on "From " lines using the same isMboxSeparator helper that
// the attribute parser uses, so the body and attribute parsers agree
// on where messages start.
func mboxBody(ctx context.Context, fsys fs.FS, filePath string, maxBytes int) (string, error) {
	f, err := fsys.Open(filePath)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), MaxLineBytes())

	var out strings.Builder
	var msgBuf bytes.Buffer
	flush := func() {
		if msgBuf.Len() == 0 {
			return
		}
		remaining := maxBytes
		if maxBytes > 0 {
			remaining = maxBytes - out.Len()
		}
		body, _ := emlBodyFromReader(ctx, &msgBuf, remaining)
		msgBuf.Reset()
		if body == "" {
			return
		}
		if out.Len() > 0 {
			out.WriteString("\n\n")
		}
		out.WriteString(body)
	}
	started := false
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return out.String(), err
		}
		if maxBytes > 0 && out.Len() >= maxBytes {
			break
		}
		line := scanner.Bytes()
		if isMboxSeparator(line) {
			if started {
				flush()
			}
			started = true
			continue
		}
		if started {
			msgBuf.Write(line)
			msgBuf.WriteByte('\n')
		}
	}
	flush()
	return out.String(), nil
}
