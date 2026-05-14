package content

import (
	"bytes"
	"context"
	"testing"
)

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
		const maxBytes = 4096
		ctx := context.Background()

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
		const maxBytes = 4096
		out, _ := extractHTMLText(context.Background(), bytes.NewReader(data), maxBytes)
		if len(out) > 2*maxBytes {
			t.Fatalf("output %d bytes exceeds 2× cap (%d)", len(out), maxBytes)
		}
	})
}
