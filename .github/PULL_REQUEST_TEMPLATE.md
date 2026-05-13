## Summary

<!-- One paragraph: what changes and why. The "why" is the part future
readers will care about — link issues if relevant ("closes #123"). -->

## Test plan

<!-- Tick the boxes that apply. Add custom checks if the change has
something unusual (benchmark, new fuzz target, MCP smoke, …). -->

- [ ] `go build ./...`
- [ ] `go test -race ./...`
- [ ] `go vet ./...`
- [ ] `golangci-lint run`
- [ ] `go fix -diff ./...` (empty)
- [ ] If the change touches a parser: relevant fuzz target re-run (`go test -fuzz=Fuzz<Name> -fuzztime=30s ./<pkg>`)
- [ ] If the change adds a CEL attribute: `go test ./internal/celexpr/` (README parity guard)
- [ ] If the change is user-facing: README / examples / CLAUDE.md updated

## Notes for the reviewer

<!-- Optional: where to look first, design choices you flagged for
discussion, what's intentionally out of scope. -->
