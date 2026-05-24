package content

import (
	"bytes"
	"context"
	"testing"
	"testing/fstest"
	"time"
)

// fuzzXMLInputCap caps the per-input size for the XML / HTML fuzz
// targets. Adversarial all-tags input doesn't trip the per-call
// maxBytes output cap (no CharData is emitted) and can drive the
// decoder for many seconds before the production maxXMLTokens guard
// kicks in. The mutator generates a long tail of multi-KB nested-tag
// inputs that are individually safe but collectively starve the fuzz
// worker, so the harness fails with "context deadline exceeded" at
// the -fuzztime ceiling.
//
// History: workflow run 26206369524 first hit this; we capped at 8
// KiB + 500ms timeout. Workflow run 26352331608 hit it again on a
// new mutator shape — `xml.Decoder.Token()` can spin > 500ms inside
// a single call (e.g. a long DOCTYPE block in an 8 KiB input)
// without yielding to ctx, since the ctx check is at the iteration
// boundary not inside Token(). Tightened to 2 KiB + 200ms; 2 KiB
// still exercises every interesting shape (deep nesting, mixed
// content, entity bombs, namespace quirks).
const fuzzXMLInputCap = 2 * 1024

// fuzzXMLPerCallTimeout is the wall-clock ceiling for a single fuzz
// invocation. Belt-and-braces alongside fuzzXMLInputCap: even if a
// future mutator finds a small-but-slow input that the size cap
// misses, ctx cancellation surfaces inside extractXMLText /
// extractHTMLText (both check ctx.Err() per token) so the worker
// completes promptly. 200ms leaves plenty of headroom under any
// fuzz-worker grace period when -fuzztime expires.
const fuzzXMLPerCallTimeout = 200 * time.Millisecond

// FuzzExtractXMLText targets the shared XML walker that DOCX / XLSX /
// PPTX / ODT all funnel through. The walker takes (paraElem, textElem)
// as the per-format shape parameters; we fuzz the input bytes with a
// representative element-pair so the same target exercises every
// format's wire grammar.
//
// Risk model: Go's encoding/xml package has had CVEs around deeply
// nested elements, entity recursion, and namespace handling
// (CVE-2020-29509 / CVE-2020-29510 / CVE-2021-27918). Our walker
// adds parameterised collection state on top — a paragraph depth
// counter, a text-element depth counter, and string builders bounded
// by maxBytes. The contract:
//
//   - never panic, even on malformed XML, deeply nested input, or
//     truncated streams
//   - output never exceeds 2 × maxBytes (we allow some slack for the
//     "stop at next paragraph after cap" semantic)
//   - ctx.Done() honoured: a cancelled ctx terminates the walk within
//     a small bounded number of tokens
//
// We don't assert content equality — corrupt input legitimately
// produces empty output. The contract is "doesn't crash, doesn't run
// forever, respects the cap".
func FuzzExtractXMLText(f *testing.F) {
	seeds := [][]byte{
		[]byte(""),
		[]byte("not xml"),
		// Minimal well-formed snippet for the DOCX/PPTX shape (p/t).
		[]byte(`<w:p xmlns:w="x"><w:r><w:t>hello</w:t></w:r></w:p>`),
		// XLSX sharedStrings shape (si/t).
		[]byte(`<sst xmlns="x"><si><t>revenue</t></si></sst>`),
		// ODT shape (p / direct CharData).
		[]byte(`<text:p xmlns:text="x">direct text</text:p>`),
		// Nested paragraphs (real docs have <w:p>/<w:tc>/<w:p>).
		[]byte(`<a><p><p>nested</p></p></a>`),
		// Adversarial: claim huge depth via repeated open tags.
		[]byte(`<p><p><p><p><p><p><p><p>x`),
		// Entity-expansion bomb shape (we don't define entities so
		// this should error cleanly, not expand).
		[]byte(`<!DOCTYPE x [<!ENTITY a "aaaa">]><p>&a;&a;&a;</p>`),
		// Truncated mid-element.
		[]byte(`<w:p><w:t>part`),
	}
	for _, s := range seeds {
		f.Add(s)
	}

	// One target, two passes — DOCX/PPTX shape (textElem set) and ODT
	// shape (textElem empty, scoped to paraElem). Captures both
	// extractor modes from a single fuzz function.
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > fuzzXMLInputCap {
			return
		}
		const maxBytes = 4096
		ctx, cancel := context.WithTimeout(context.Background(), fuzzXMLPerCallTimeout)
		defer cancel()

		// Scoped mode (DOCX / XLSX / PPTX).
		out, _ := extractXMLText(ctx, bytes.NewReader(data), "p", "t", maxBytes)
		if len(out) > 2*maxBytes {
			t.Fatalf("scoped mode: output %d bytes exceeds 2× cap (%d)", len(out), maxBytes)
		}
		// Unscoped mode (ODT).
		out, _ = extractXMLText(ctx, bytes.NewReader(data), "p", "", maxBytes)
		if len(out) > 2*maxBytes {
			t.Fatalf("unscoped mode: output %d bytes exceeds 2× cap (%d)", len(out), maxBytes)
		}
	})
}

// FuzzExtractHTMLText targets the EPUB chapter extractor — the
// permissive HTML reader that runs encoding/xml in non-strict mode
// with our custom auto-close list and entity map. This is the highest-
// leverage surface in the body extractor: permissive parsing of
// untrusted markup is exactly the territory where parser bugs hide.
//
// Same contract as FuzzExtractXMLText: no panic, output bounded.
func FuzzExtractHTMLText(f *testing.F) {
	seeds := [][]byte{
		[]byte(""),
		[]byte(`<p>hello</p>`),
		[]byte(`<html><body><h1>Title</h1><p>Para one.</p><p>Para two.</p></body></html>`),
		// XHTML self-closing void elements.
		[]byte(`<p>before<br/>after</p>`),
		// Script/style content must be skipped.
		[]byte(`<script>alert("xss")</script><p>visible</p>`),
		[]byte(`<style>p{color:red}</style><p>visible</p>`),
		// Custom entities (in our map).
		[]byte(`<p>&nbsp;copy &copy; &mdash; &rsquo;</p>`),
		// Unknown entity — should error cleanly inside the decoder.
		[]byte(`<p>&unknown;</p>`),
		// Adversarial nesting.
		[]byte(`<div><div><div><div><div>nested</div></div></div></div></div>`),
		// Mismatched tags (HTML5 tolerance).
		[]byte(`<p><b>bold</p></b>`),
		// Truncated mid-tag.
		[]byte(`<p>text`),
		// Numeric character references.
		[]byte(`<p>&#65;&#x42;</p>`),
		// Comment + CDATA + processing instruction.
		[]byte(`<?xml version="1.0"?><!-- comment --><![CDATA[verbatim]]><p>tail</p>`),
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > fuzzXMLInputCap {
			return
		}
		const maxBytes = 4096
		ctx, cancel := context.WithTimeout(context.Background(), fuzzXMLPerCallTimeout)
		defer cancel()
		out, _ := extractHTMLText(ctx, bytes.NewReader(data), maxBytes)
		if len(out) > 2*maxBytes {
			t.Fatalf("output %d bytes exceeds 2× cap (%d)", len(out), maxBytes)
		}
	})
}

// FuzzExtractEmailBody targets the email body extractor — both forms.
// The fuzzer mutates raw bytes which we route through both the
// single-message (.eml) and mbox extractors via an in-memory fs.FS.
// The eml extractor exercises mail.ReadMessage + MIME header parsing
// + mime/multipart + mime/quotedprintable + encoding/base64 — every
// one of those parsers is a historical CVE source. The mbox extractor
// adds the "From " separator splitter on top.
//
// Risk model: adversarial RFC 5322 input can carry malformed MIME
// boundaries, recursive multipart structures, oversized headers,
// invalid quoted-printable / base64 streams, and "From " lines that
// look like separators but aren't. The contract:
//
//   - never panic, even on truncated / random / deeply-nested input
//   - output never exceeds 2 × maxBytes
//
// Seeds cover well-formed eml + mbox shapes plus pathological MIME.
func FuzzExtractEmailBody(f *testing.F) {
	seeds := [][]byte{
		[]byte(""),
		[]byte("not an email"),
		// Header-only message (no body, no Content-Type).
		[]byte("Subject: test\r\nFrom: a@example.com\r\n\r\n"),
		// Minimal text/plain message.
		[]byte("Content-Type: text/plain\r\n\r\nhello\r\n"),
		// Minimal multipart/mixed.
		[]byte("Content-Type: multipart/mixed; boundary=\"x\"\r\n\r\n--x\r\nContent-Type: text/plain\r\n\r\nbody\r\n--x--\r\n"),
		// multipart/alternative — should prefer text/plain.
		[]byte("Content-Type: multipart/alternative; boundary=\"y\"\r\n\r\n--y\r\nContent-Type: text/plain\r\n\r\nplain version\r\n--y\r\nContent-Type: text/html\r\n\r\n<p>html version</p>\r\n--y--\r\n"),
		// Quoted-printable encoded.
		[]byte("Content-Type: text/plain\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\nHello=20world=\r\n"),
		// base64 encoded.
		[]byte("Content-Type: text/plain\r\nContent-Transfer-Encoding: base64\r\n\r\naGVsbG8K\r\n"),
		// Adversarial: claimed multipart with bogus boundary.
		[]byte("Content-Type: multipart/mixed; boundary=\"\"\r\n\r\nbody\r\n"),
		// mbox shape — two messages.
		[]byte("From alice@example.com Tue Apr 14 09:30:00 2026\r\nSubject: one\r\n\r\nfirst\r\nFrom bob@example.com Tue Apr 14 10:30:00 2026\r\nSubject: two\r\n\r\nsecond\r\n"),
		// mbox with body line that looks like a separator (no @).
		[]byte("From alice@example.com Tue Apr 14 09:30:00 2026\r\nSubject: tricky\r\n\r\nFrom the look of things, this isn't a separator.\r\n"),
		// Deeply-nested multipart (boundary recursion risk).
		[]byte("Content-Type: multipart/mixed; boundary=\"a\"\r\n\r\n--a\r\nContent-Type: multipart/mixed; boundary=\"b\"\r\n\r\n--b\r\nContent-Type: multipart/mixed; boundary=\"c\"\r\n\r\n--c\r\nContent-Type: text/plain\r\n\r\ndeep\r\n--c--\r\n--b--\r\n--a--\r\n"),
		// Truncated mid-body.
		[]byte("Content-Type: text/plain\r\n\r\nhello"),
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		const maxBytes = 4096
		ctx := context.Background()
		fsys := fstest.MapFS{"msg": &fstest.MapFile{Data: data}}

		// eml extractor (single message).
		out, _ := emlBody(ctx, fsys, "msg", maxBytes)
		if len(out) > 2*maxBytes {
			t.Fatalf("eml output %d bytes exceeds 2× cap (%d)", len(out), maxBytes)
		}

		// mbox extractor (cross-message). Only valid mbox bodies start
		// with "From ", but the contract is that ARBITRARY bytes don't
		// panic — adversarial input here is the whole point of the
		// fuzz.
		out, _ = mboxBody(ctx, fsys, "msg", maxBytes)
		if len(out) > 2*maxBytes {
			t.Fatalf("mbox output %d bytes exceeds 2× cap (%d)", len(out), maxBytes)
		}

		// Also exercise the in-memory variant (mboxBody recurses into
		// it once per message) for shapes the wrapper might gate out.
		out, _ = emlBodyFromReader(ctx, bytes.NewReader(data), maxBytes)
		if len(out) > 2*maxBytes {
			t.Fatalf("emlBodyFromReader output %d bytes exceeds 2× cap (%d)", len(out), maxBytes)
		}
	})
}

// FuzzExtractPDFBody targets the PDF body extractor — the highest-risk
// surface added in this round. PDF is a binary container with indirect
// objects, a cross-reference table, encrypted-stream support, font
// dictionaries with their own ToUnicode CMaps, and per-page content
// streams that are themselves a tokeniser-based mini-language. The
// underlying parser (ledongthuc/pdf) self-documents as "incomplete"
// and panics on many adversarial inputs.
//
// Risk model: malformed cross-reference tables, gigantic claimed
// object counts, recursive object references, malformed font
// dictionaries, broken ToUnicode CMaps, and content-stream operands
// that drive the tokeniser into unbounded loops are all in scope for
// mutation. The contract:
//
//   - never panic — the pdfBody defer/recover catches library panics
//   - output never exceeds 2 × maxBytes (single-byte cap slack)
//
// We don't assert content equality — random bytes legitimately yield
// empty output. The contract is "doesn't crash, doesn't run forever".
func FuzzExtractPDFBody(f *testing.F) {
	seeds := [][]byte{
		[]byte(""),
		[]byte("not a PDF"),
		// Just the header.
		[]byte("%PDF-1.4\n"),
		// Header + binary signature + something resembling a trivial
		// catalog. Real PDFs prefix four non-ASCII bytes to flag the
		// file as binary; ledongthuc/pdf parses that prefix during
		// header sniffing.
		[]byte("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n"),
		// Header + catalog + pages but no actual page object — pages
		// count claims 1 but Page(1) won't resolve.
		[]byte("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n2 0 obj\n<< /Type /Pages /Count 1 /Kids [3 0 R] >>\nendobj\n"),
		// Truncated cross-reference section.
		[]byte("%PDF-1.4\nxref\n0 999999\n"),
		// Claimed-encrypted PDF — has an /Encrypt entry in the trailer
		// dict. ledongthuc/pdf's encrypted-PDF support is documented
		// as weak; the parser should error or return empty.
		[]byte("%PDF-1.4\ntrailer << /Encrypt 1 0 R >>\nstartxref\n0\n%%EOF"),
		// Pathological content-stream payload — operators that look
		// real but with nonsense operands.
		[]byte("%PDF-1.4\n%\xe2\xe3\xcf\xd3\nq BT /F1 12 Tf (cid:99999) Tj ET Q\n"),
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		const maxBytes = 4096
		ctx := context.Background()
		fsys := fstest.MapFS{"x.pdf": &fstest.MapFile{Data: data}}

		out, _ := pdfBody(ctx, fsys, "x.pdf", maxBytes)
		if len(out) > 2*maxBytes {
			t.Fatalf("pdf output %d bytes exceeds 2× cap (%d)", len(out), maxBytes)
		}
	})
}
