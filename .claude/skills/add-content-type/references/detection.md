# Content-type detection

How `Registry.Detect` decides what a file is, what `Attributes(path)` should and should not do, and the gotchas that bite when adding a new type.

## How `Registry.Detect` works

The detector is in `internal/content/detector.go`. The algorithm:

1. **Extension first.** Lower-case `filepath.Ext(path)` and check it against each registered type's `Extensions()` slice. First match wins.
2. **Magic-byte fallback.** If no extension matched, read up to 512 bytes from the file. For each registered type, walk its `MagicBytes()` slice; if any entry is a prefix of the read bytes, that type wins.
3. **No match → `nil`.** `BuildAttributes` treats this as "no content type"; the `content_type` CEL attribute is the empty string and every `is_*` flag is false.

This means:

- Registration order matters when multiple types claim the same extension or the same magic prefix. The detector returns the first match. In practice the existing types don't overlap, but if you register a type whose extensions or magic overlap an existing one, expect a flaky-feeling bug.
- A type with `Extensions() = nil` and `MagicBytes() = nil` is unreachable. The detector will never return it.

## Worked example: detecting an EPUB

EPUBs are zip files with a specific marker. Detection options:

- **Extension only:** `Extensions() = []string{".epub"}`, `MagicBytes() = nil`. Cheap, but misses files renamed away from `.epub`.
- **Magic only:** EPUB starts with `PK\x03\x04` (zip header) plus, deeper inside, a `mimetype` entry containing `application/epub+zip`. The first 512 bytes can include the zip header but you'd be detecting "any zip" — too broad.
- **Both:** Use the extension match for the common case; magic-byte fallback only if you can find a sufficiently specific prefix in the first 512 bytes. EPUBs typically place the `mimetype` entry first (it's an EPUB requirement), so the bytes `application/epub+zip` often appear in the first 512 bytes.

Pragmatic recommendation: start with extension-only. Add magic bytes when you have a real test file proving they appear in the first 512 bytes.

## Magic-byte gotchas

- **`nil` vs. empty slice.** Return `nil` for "no magic". Returning `[][]byte{}` is technically valid but semantically the same — pick one (`nil`) so the codebase is consistent.
- **First 512 bytes only.** Magic deeper into the file is invisible. PDFs (`%PDF-`) at byte 0 work; EPUB `mimetype` entries usually do, by spec.
- **Prefix, not substring.** The detector checks `bytes.HasPrefix(read, magic)`. A magic of `"foo"` won't match a file that starts with `"\xef\xbb\xbffoo"` (BOM-prefixed). If you need BOM tolerance, register multiple magic entries: `[]byte("foo"), []byte("\xef\xbb\xbffoo")`.
- **Avoid super-short or super-common prefixes.** `"{"` matches every JSON file but also every file that happens to start with `{`. JSON gets away with it because almost nothing else does. CSV has no usable magic — leave `MagicBytes` as `nil`.

## What `Attributes(ctx, path)` should and should not do

`Attributes` is called *per matching file*. A search across 100k markdown files calls it 100k times. Performance and resource bounds matter.

**Do:**

- Open the file at `path`, decode just enough to extract what you need, close it. Use `defer f.Close()`.
- Use bounded buffers — `bufio.Scanner` with a sized buffer cap, `io.LimitReader` for streams that could be huge.
- Honour `ctx`: check `ctx.Err()` at entry, and inside any unbounded scan/decode loop. Return `ctx.Err()` on cancellation. The walker treats `context.Canceled` / `context.DeadlineExceeded` as a signal to terminate the worker rather than skip-and-continue, so cancellation actually stops the search.
- Return `Attributes{}` (empty map, not `nil`) if there's nothing type-specific to extract. The detector still records `content_type`; you don't need attributes to be useful.
- Return `(nil, err)` on a real I/O error. The walker silently skips files whose `Attributes` returns an error — by design, since the search shouldn't crash on a single malformed file.

**Don't:**

- Read or parse the entire file when you only need the first chunk. JSON's content type only reads the first token to detect `object` vs `array` — copy that pattern.
- Ignore ctx. A pathological 1 GB log file with `bufio.Scanner` will run to completion if the loop doesn't check ctx, even after the user has cancelled. Per-iteration `ctx.Err()` is a single atomic load — well below file-I/O noise.
- Make additional file-system calls beyond reading `path`. The walker has already passed you a stat-able path; recomputing stat or scanning siblings is wasted work.
- Cache anything in a package-level variable. The `Attributes` call is concurrent — content types are stateless by contract. If you need a cache, scope it inside the function with a `sync.Once`-protected init, but think hard about why first.
- Panic. The walker doesn't recover; a panicking type takes down the whole search.

## Image-family branching

The `BuildAttributes` function in `internal/celexpr/evaluator.go` (around line 189) has a single branch that catches every image variant by name prefix:

```go
case strings.HasPrefix(contentTypeName, "image/"):
    isImage = true
```

Adding a new image variant (e.g. `image/avif`) is therefore additive: register the type with `Name() = "image/avif"` and the `is_image` flag flips on for that variant automatically. No edit to `evaluator.go` is needed.

For a brand-new family (audio, archive, …), you DO need to extend `BuildAttributes`:

1. Add a `case strings.HasPrefix(contentTypeName, "audio/"): isAudio = true` (or whatever family prefix).
2. Add an `IsAudio bool` field to the `FileAttributes` struct in the same file.
3. Add `is_audio` to the four-place CEL invariant (use the `extend-cel-schema` skill).

The reason image is different is that it was the first family to need this pattern; if you add audio/archive/video, you're following the same template, not inventing one.

## Verifying a new type

After registering:

```sh
go build ./...
go test ./...
go run ./cmd/file-search-on --list   # your type appears under "Registered content types"
```

Then point the search at a directory containing a real file of the new type:

```sh
go run ./cmd/file-search-on 'content_type == "<your-name>"' -d ./path-with-test-files
```

If the file isn't reported, either the extension or magic-bytes match isn't firing. Run `--list` to confirm registration; if it's listed there, the issue is in `Detect` (extension/magic mismatch). If it's not listed, your `init()` didn't run — make sure the file is in `internal/content/` and the package was actually built (no syntax errors, etc.).
