// Package search is the orchestration layer: it walks directory trees
// (and archives), evaluates the compiled CEL filter per file, and
// aggregates the results (sort/limit/pagination, stats histograms,
// duplicate + near-duplicate clustering, line-level regex matching,
// tree diffing). The streaming core is WalkStream; Walk is its buffered
// wrapper; the rest are sibling orchestrators over the same walk.
//
// # Robustness invariants
//
// Two robustness properties are CONVENTIONS that every orchestrator and
// every content-type parser must uphold. They are not merely aspirational
// — each is pinned by a property/smoke test so a regression fails the
// suite rather than waiting to be re-discovered in a dogfood run (the
// origin of bugs #321 and #331, generalised by issue #337).
//
// ## 1. Graceful degradation — parse failure degrades, never drops
//
// A file that is detected but whose ContentType.Attributes() errors (or
// panics, for the defer/recover'd parsers) MUST still appear in
// `search 'true'` with whatever attributes survived — it is never
// silently dropped from results. The walker treats an Attributes error as
// "emit the file with its base (path/size/type) attributes" rather than
// skipping it. Empty and truncated files take the same path.
//
// Guard: TestWalk_GracefulDegradation_EveryType feeds malformed + empty
// inputs for every parsed family and asserts every file survives the
// walk. A new content type that drops malformed input fails it.
//
// ## 2. Cancellation — observe ctx, return promptly
//
// Every orchestrator and every hand-rolled parser loop MUST check
// ctx.Err() (or select on ctx.Done()) at entry AND inside any unbounded
// loop — at least every N iterations for hot inner loops, every iteration
// for per-file/per-entry loops. On cancellation an orchestrator either
// returns the context error (the streaming Walk/WalkStream) or returns
// its partial result with Cancelled=true (the buffered aggregators:
// ComputeStats, FindDuplicates, FindNearDuplicates, FindMatches,
// DiffTrees, WalkArchiveEntries). It must never run to completion past a
// cancelled context.
//
// Concretely, when adding a loop that could run long (per file, per
// archive entry, per byte-walked box/atom/EBML element, per candidate
// pair), thread ctx through and add:
//
//	if err := ctx.Err(); err != nil {
//		return ..., err // or set Cancelled = true and stop
//	}
//
// Guard: TestOrchestrators_PreCancelledCtxReturnPromptly runs each
// orchestrator with an already-cancelled context and fails if any does
// not return promptly with cancellation signalled.
//
// See also issue #229 (the nilerr linter for intentional graceful-
// degradation sites) — complementary mechanics for the same spirit.
package search
