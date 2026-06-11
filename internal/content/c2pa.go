package content

import (
	"context"
	"crypto/x509"
	"encoding/asn1"
	"encoding/binary"
	"io"
	"math/big"
	"reflect"
	"strings"
	"time"

	"github.com/fxamacker/cbor/v2"
	cose "github.com/veraison/go-cose"
)

// c2paDecMode decodes CBOR maps (including nested ones) into map[string]any
// rather than fxamacker's default map[any]any, so the field lookups below
// work at every depth. C2PA claims/assertions use text keys throughout.
var c2paDecMode = func() cbor.DecMode {
	dm, err := cbor.DecOptions{DefaultMapType: reflect.TypeFor[map[string]any]()}.DecMode()
	if err != nil {
		panic(err) // static options; can't fail
	}
	return dm
}()

// C2PA / Content Credentials (https://c2pa.org) provenance extraction.
//
// We surface what a file CLAIMS about its provenance — the creating tool,
// title, and whether it declares AI-generated content — by reading the
// embedded JUMBF manifest (ISO 19566-5) and CBOR-decoding the active
// manifest's claim + c2pa.actions assertion. This is read-only and
// UNVERIFIED: we deliberately do NOT validate the COSE signature or trust
// chain (that needs the Rust c2pa-rs via CGO, which the pure-Go build
// forbids). Treat these like EXIF — accurate-as-recorded, not authenticated.
// Issue #374.

// maxC2PAScan caps how many leading bytes we read looking for a manifest.
// C2PA manifests sit in the file header (before image data) and rarely
// exceed a few MB even with embedded thumbnails; past the cap we give up.
const maxC2PAScan = 16 << 20

// c2paInfo is the surfaced subset of a C2PA manifest.
type c2paInfo struct {
	Present        bool
	ClaimGenerator string
	Title          string
	Format         string
	AIGenerated    bool
	// SignedBy is the COSE_Sign1 signer's leaf x509 certificate common name
	// (Subject CN, falling back to the first Organization). SignedAt is the
	// signing time from the RFC 3161 timestamp embedded in the signature.
	// Both are CLAIMED, not validated — we never check the certificate chain
	// against the C2PA trust list (issue #375).
	SignedBy string
	SignedAt time.Time
}

// extractC2PA reads up to maxC2PAScan bytes from rs and, for the given
// container ("jpeg" / "png"), locates + parses the JUMBF manifest. Returns
// a zero c2paInfo (Present=false) when there's no manifest. Never errors —
// provenance is best-effort metadata, like EXIF.
//
// ctx is honoured at entry and inside the input-scaled scan loops (up to
// maxC2PAScan = 16 MiB), so a cancelled search surrenders promptly mid-scan
// rather than parsing a full adversarial header (issue #337).
func extractC2PA(ctx context.Context, container string, rs io.Reader) c2paInfo {
	if ctx.Err() != nil {
		return c2paInfo{}
	}
	data, err := io.ReadAll(io.LimitReader(rs, maxC2PAScan))
	if err != nil || len(data) == 0 {
		return c2paInfo{}
	}
	var jumbf []byte
	switch container {
	case "jpeg":
		jumbf = jpegJUMBF(ctx, data)
	case "png":
		jumbf = pngJUMBF(ctx, data)
	default:
		return c2paInfo{}
	}
	if len(jumbf) == 0 {
		return c2paInfo{}
	}
	return parseC2PAManifest(ctx, jumbf)
}

// jpegJUMBF reassembles the JUMBF box from APP11 (0xFFEB) marker segments,
// stopping at start-of-scan. Packet 1 of a box keeps its LBox+TBox; later
// packets repeat them and are skipped (ISO 19566-5 JPEG embedding).
func jpegJUMBF(ctx context.Context, data []byte) []byte {
	var out []byte
	i := 2 // skip SOI
	for i < len(data)-1 {
		if ctx.Err() != nil {
			return out
		}
		if data[i] != 0xFF {
			i++
			continue
		}
		m := data[i+1]
		switch {
		case m == 0xD9 || m == 0xDA: // EOI / SOS — image data starts; stop.
			return out
		case m == 0xD8 || (m >= 0xD0 && m <= 0xD7) || m == 0x01: // standalone markers
			i += 2
			continue
		}
		if i+4 > len(data) {
			break
		}
		ln := int(binary.BigEndian.Uint16(data[i+2 : i+4]))
		if ln < 2 || i+2+ln > len(data) {
			break
		}
		if m == 0xEB { // APP11
			p := data[i+4 : i+2+ln]
			if len(p) > 8 && binary.BigEndian.Uint16(p[:2]) == 0x4A50 { // "JP"
				box := p[8:] // skip CI(2)+En(2)+Z(4)
				if binary.BigEndian.Uint32(p[4:8]) == 1 {
					out = append(out, box...)
				} else if len(box) > 8 {
					out = append(out, box[8:]...)
				}
			}
		}
		i += 2 + ln
	}
	return out
}

// pngJUMBF concatenates the data of all `caBX` chunks (PNG's C2PA carrier),
// stopping at IDAT. PNG: 8-byte signature, then [len(4)][type(4)][data][crc(4)].
func pngJUMBF(ctx context.Context, data []byte) []byte {
	if len(data) < 8 {
		return nil
	}
	var out []byte
	i := 8
	for i+8 <= len(data) {
		if ctx.Err() != nil {
			return out
		}
		ln := int(binary.BigEndian.Uint32(data[i : i+4]))
		typ := string(data[i+4 : i+8])
		if ln < 0 || i+12+ln > len(data) {
			break
		}
		switch typ {
		case "IDAT", "IEND":
			return out
		case "caBX":
			out = append(out, data[i+8:i+8+ln]...)
		}
		i += 12 + ln // len + type + data + crc
	}
	return out
}

// parseC2PAManifest walks the JUMBF box tree, decodes the c2pa.claim and
// c2pa.actions CBOR, and returns the surfaced fields.
func parseC2PAManifest(ctx context.Context, jumbf []byte) c2paInfo {
	info := c2paInfo{}
	walkJUMBFBoxes(ctx, jumbf, "", func(label string, tbox string, content []byte) {
		switch {
		case tbox != "cbor":
			return
		case strings.HasSuffix(label, "c2pa.claim") || strings.Contains(label, "c2pa.claim.v"):
			info.Present = true
			var claim map[string]any
			if c2paDecMode.Unmarshal(content, &claim) == nil {
				info.ClaimGenerator = c2paClaimGenerator(claim)
				info.Title, _ = claim["dc:title"].(string)
				info.Format, _ = claim["dc:format"].(string)
			}
		case strings.HasSuffix(label, "c2pa.actions") || strings.Contains(label, "c2pa.actions.v"):
			info.Present = true
			var act map[string]any
			if c2paDecMode.Unmarshal(content, &act) == nil && c2paActionsAreAI(act) {
				info.AIGenerated = true
			}
		case strings.HasSuffix(label, "c2pa.signature"):
			// The signature box content is a COSE_Sign1 (a raw CBOR array,
			// not a text-keyed map), so it bypasses the claim/actions
			// decode above and is parsed by c2paSignerIdentity.
			info.Present = true
			if by, at := c2paSignerIdentity(content); by != "" || !at.IsZero() {
				info.SignedBy = by
				info.SignedAt = at
			}
		}
	})
	return info
}

// c2paSignerIdentity decodes the COSE_Sign1 envelope of a c2pa.signature box
// and returns the signer's leaf-certificate name and the signing time. It is
// best-effort and never panics: any decode failure yields the zero values.
// This reads the CLAIMED identity only — it performs NO trust-chain
// validation (issue #375 non-goal).
func c2paSignerIdentity(coseSign1 []byte) (signedBy string, signedAt time.Time) {
	var msg cose.Sign1Message
	if err := msg.UnmarshalCBOR(coseSign1); err != nil {
		return "", time.Time{}
	}
	if leaf := c2paLeafCert(msg.Headers); leaf != nil {
		signedBy = leaf.Subject.CommonName
		if signedBy == "" && len(leaf.Subject.Organization) > 0 {
			signedBy = leaf.Subject.Organization[0]
		}
	}
	signedAt = c2paSigningTime(msg.Headers.Unprotected)
	return signedBy, signedAt
}

// c2paLeafCert pulls the x5chain (header label 33) from the COSE headers
// (protected first, then unprotected) and parses its first entry — the leaf
// signer certificate.
func c2paLeafCert(h cose.Headers) *x509.Certificate {
	for _, store := range []map[any]any{h.Protected, h.Unprotected} {
		// go-cose keys protected/unprotected headers with its int64 label
		// constants, so look up the x5chain with cose.HeaderLabelX5Chain
		// (== 33) rather than an untyped int literal — int(33) would miss.
		der := firstX5ChainDER(store[cose.HeaderLabelX5Chain])
		if der == nil {
			continue
		}
		if c, err := x509.ParseCertificate(der); err == nil {
			return c
		}
	}
	return nil
}

// firstX5ChainDER extracts the first DER certificate from an x5chain header
// value, which may be a single []byte (one cert) or an array of them.
func firstX5ChainDER(v any) []byte {
	switch x := v.(type) {
	case []byte:
		return x
	case [][]byte:
		if len(x) > 0 {
			return x[0]
		}
	case []any:
		for _, e := range x {
			if b, ok := e.([]byte); ok {
				return b
			}
		}
	}
	return nil
}

// c2paSigningTime extracts the signing time from the COSE unprotected
// `sigTst` header — a C2PA timestamp container holding RFC 3161 timestamp
// tokens. Returns the zero time if absent or unparseable.
func c2paSigningTime(unprotected map[any]any) time.Time {
	tst, ok := unprotected["sigTst"].(map[any]any)
	if !ok {
		return time.Time{}
	}
	tokens, ok := tst["tstTokens"].([]any)
	if !ok {
		return time.Time{}
	}
	for _, tk := range tokens {
		m, ok := tk.(map[any]any)
		if !ok {
			continue
		}
		der, ok := m["val"].([]byte)
		if !ok {
			continue
		}
		if t := rfc3161GenTime(der); !t.IsZero() {
			return t
		}
	}
	return time.Time{}
}

// rfc3161GenTime walks an RFC 3161 timestamp (a TimeStampResp wrapping a CMS
// SignedData, or a bare ContentInfo) down to TSTInfo.genTime. It is defensive
// — any structural surprise returns the zero time rather than erroring.
func rfc3161GenTime(der []byte) time.Time {
	contentInfo := der
	// TimeStampResp ::= SEQUENCE { status PKIStatusInfo, timeStampToken ContentInfo OPTIONAL }
	// When the optional token is present, descend into it; otherwise `der`
	// is already a bare ContentInfo.
	var resp struct {
		Status asn1.RawValue
		Token  asn1.RawValue `asn1:"optional"`
	}
	if _, err := asn1.Unmarshal(der, &resp); err == nil && len(resp.Token.FullBytes) > 0 {
		contentInfo = resp.Token.FullBytes
	}

	// ContentInfo ::= SEQUENCE { contentType OID, content [0] EXPLICIT ANY }
	var ci struct {
		OID     asn1.ObjectIdentifier
		Content asn1.RawValue `asn1:"explicit,tag:0"`
	}
	if _, err := asn1.Unmarshal(contentInfo, &ci); err != nil {
		return time.Time{}
	}

	// SignedData ::= SEQUENCE { version, digestAlgorithms SET,
	//   encapContentInfo SEQUENCE { eContentType OID, eContent [0] EXPLICIT OCTET STRING }, ... }
	var sd struct {
		Version     int
		DigestAlgos asn1.RawValue `asn1:"set"`
		Encap       struct {
			OID     asn1.ObjectIdentifier
			Content asn1.RawValue `asn1:"explicit,optional,tag:0"`
		}
		Rest []asn1.RawValue `asn1:"optional"`
	}
	if _, err := asn1.Unmarshal(ci.Content.Bytes, &sd); err != nil {
		return time.Time{}
	}

	// eContent is an OCTET STRING wrapping the DER-encoded TSTInfo.
	var eContent []byte
	if _, err := asn1.Unmarshal(sd.Encap.Content.Bytes, &eContent); err != nil {
		return time.Time{}
	}

	// TSTInfo ::= SEQUENCE { version, policy OID, messageImprint, serialNumber INTEGER, genTime GeneralizedTime, ... }
	var tst struct {
		Version        int
		Policy         asn1.ObjectIdentifier
		MessageImprint asn1.RawValue
		SerialNumber   *big.Int
		GenTime        time.Time       `asn1:"generalized"`
		Rest           []asn1.RawValue `asn1:"optional"`
	}
	if _, err := asn1.Unmarshal(eContent, &tst); err != nil {
		return time.Time{}
	}
	return tst.GenTime
}

// maxJUMBFDepth caps superbox nesting. Real C2PA manifests nest only a few
// levels (store → manifest → c2pa.assertions → assertion ≈ 4); 64 is far
// above that yet well below a stack-overflow threshold. Without this an
// adversarial input — a chain of nested `jumb` boxes, each stripping only
// the 8-byte header — could nest ~maxC2PAScan/8 levels deep and exhaust the
// goroutine stack. We degrade gracefully (stop descending) instead.
const maxJUMBFDepth = 64

// walkJUMBFBoxes recursively walks JUMBF boxes, invoking fn(label, tbox,
// content) for every box. label is the nearest enclosing superbox's jumd
// label.
func walkJUMBFBoxes(ctx context.Context, b []byte, label string, fn func(label, tbox string, content []byte)) {
	walkJUMBFBoxesDepth(ctx, b, label, 0, fn)
}

func walkJUMBFBoxesDepth(ctx context.Context, b []byte, label string, depth int, fn func(label, tbox string, content []byte)) {
	if depth > maxJUMBFDepth {
		return
	}
	for len(b) >= 8 {
		if ctx.Err() != nil {
			return
		}
		lbox := int(binary.BigEndian.Uint32(b[:4]))
		tbox := string(b[4:8])
		if lbox < 8 || lbox > len(b) {
			return
		}
		content := b[8:lbox]
		if tbox == "jumb" {
			childLabel, rest := jumdLabel(content)
			walkJUMBFBoxesDepth(ctx, rest, childLabel, depth+1, fn)
		} else {
			fn(label, tbox, content)
		}
		b = b[lbox:]
	}
}

// jumdLabel parses the leading jumd description box of a superbox's content
// and returns its label plus the remaining child boxes.
func jumdLabel(content []byte) (label string, rest []byte) {
	if len(content) < 8 {
		return "", content
	}
	lbox := int(binary.BigEndian.Uint32(content[:4]))
	if string(content[4:8]) != "jumd" || lbox < 8 || lbox > len(content) {
		return "", content
	}
	d := content[8:lbox]
	rest = content[lbox:]
	if len(d) < 17 { // 16-byte type UUID + 1-byte toggles
		return "", rest
	}
	if d[16]&0x02 != 0 { // toggles bit 1: label present (null-terminated)
		end := 17
		for end < len(d) && d[end] != 0 {
			end++
		}
		label = string(d[17:end])
	}
	return label, rest
}

// c2paClaimGenerator returns the claim's generator string, preferring the
// flat `claim_generator` field and falling back to claim_generator_info[].
func c2paClaimGenerator(claim map[string]any) string {
	if s, ok := claim["claim_generator"].(string); ok && s != "" {
		return s
	}
	infos, ok := claim["claim_generator_info"].([]any)
	if !ok {
		return ""
	}
	var parts []string
	for _, e := range infos {
		m, ok := e.(map[string]any)
		if !ok {
			continue
		}
		name, _ := m["name"].(string)
		if name == "" {
			continue
		}
		if ver, ok := m["version"].(string); ok && ver != "" {
			name += "/" + ver
		}
		parts = append(parts, name)
	}
	return strings.Join(parts, " ")
}

// c2paActionsAreAI reports whether a c2pa.actions assertion declares
// AI-generated content via a digitalSourceType of trainedAlgorithmicMedia
// or compositeWithTrainedAlgorithmicMedia (anywhere in the action or its
// parameters).
func c2paActionsAreAI(actAssertion map[string]any) bool {
	actions, ok := actAssertion["actions"].([]any)
	if !ok {
		return false
	}
	for _, a := range actions {
		m, ok := a.(map[string]any)
		if !ok {
			continue
		}
		if isAIDigitalSourceType(m["digitalSourceType"]) {
			return true
		}
		if params, ok := m["parameters"].(map[string]any); ok {
			if isAIDigitalSourceType(params["digitalSourceType"]) {
				return true
			}
		}
	}
	return false
}

func isAIDigitalSourceType(v any) bool {
	s, ok := v.(string)
	// Matches both digitalSourceType values that denote AI generation:
	// `trainedAlgorithmicMedia` and `compositeWithTrainedAlgorithmicMedia`
	// (note the capitalised "Trained" in the latter) — hence ToLower.
	return ok && strings.Contains(strings.ToLower(s), "trainedalgorithmicmedia")
}
