#!/usr/bin/env bash
# Build the semantic-search demo corpus in ~/Demo/semantic-demo.
#
# Twelve short markdown notes on distinct technical topics. None of the
# query phrases the demo uses appear verbatim in the matching file's
# body — that's the whole point of semantic search vs grep.
#
# Re-runnable; overwrites in place.

set -euo pipefail

DEST="${HOME}/Demo/semantic-demo"
mkdir -p "$DEST"

# --- 1. database outage post-mortem ------------------------------------
cat > "$DEST/incident-2024-q3.md" <<'EOF'
# What went wrong on the night of 2024-09-14

At 23:47 UTC the primary started rejecting connections. The on-call
engineer paged within ninety seconds and we declared a Sev-1 at 23:51.

Root cause: a runaway analytical query had exhausted the connection
pool. The query had been added three days earlier as part of a
reporting endpoint; nobody had tested it at production scale.

Detection was slower than it should have been because our alerting
keyed off CPU rather than wait events. The reporting endpoint kept
serving slow responses without tripping the threshold; only when the
pool saturated did pager fire.

Mitigation: we killed the offending query and rotated the credential.
Service recovered by 00:23. The reporting endpoint was rolled back the
next morning.

Action items: connection-pool-saturation alert, statement timeout on
the read-only role, and a load test before any new endpoint ships.
EOF

# --- 2. kubernetes pod scheduling --------------------------------------
cat > "$DEST/k8s-scheduling.md" <<'EOF'
# Pod placement controls

Three knobs decide where a pod lands.

**Node selectors** are the simplest — a flat label match. Use them
when you want a workload pinned to a specific hardware class
(GPU nodes, SSD nodes, that one machine with the FPGA).

**Affinity rules** are richer. A pod can prefer (soft) or require
(hard) co-location with another pod, or anti-co-location to spread
replicas across zones. Anti-affinity is how you stop the scheduler
from cramming three replicas of the same deployment onto one node and
then losing them all when the node reboots.

**Taints and tolerations** invert the relationship: the node refuses
to accept pods that don't carry the matching toleration. Use them for
nodes that have a side effect — paid licences, security zones,
specialty hardware that costs real money per pod-hour.
EOF

# --- 3. database transaction isolation ---------------------------------
cat > "$DEST/transaction-isolation.md" <<'EOF'
# What happens when two writers touch the same row

Postgres defaults to READ COMMITTED. Each statement sees a snapshot
taken at statement start, so two transactions can each read the same
row, modify it, and last-write-wins.

REPEATABLE READ pins the snapshot to transaction start. Now if
transaction A reads a row that transaction B has updated and
committed, A sees the old value. Phantom reads are still possible —
inserts by other transactions show up.

SERIALIZABLE is what most people mean when they say "isolated".
Postgres implements it with predicate locks plus an abort-on-conflict
strategy. If two transactions interleave in a way that couldn't have
arisen from any serial order, one of them gets thrown out and has to
retry.

The level you want is almost always SERIALIZABLE for anything
touching money. The cost is conflict-retry traffic under contention.
EOF

# --- 4. TLS handshake --------------------------------------------------
cat > "$DEST/tls-1.3.md" <<'EOF'
# Setting up an encrypted channel

The 1.3 handshake collapses to one round trip. ClientHello carries
the supported cipher suites, the supported groups, and a key share
for whichever group the client is most optimistic about. ServerHello
echoes the chosen suite and a matching key share.

Once both sides have the shared secret, the rest of the handshake is
encrypted — the certificate, the verify, the finished message. That's
the big departure from 1.2, where the certificate was in the clear
and an on-path observer could see who you were talking to.

Session resumption uses pre-shared keys derived from the previous
session. A client can attach early data ("0-RTT") to the first
message, with the caveat that 0-RTT data isn't forward-secret and is
replayable, so it should only carry idempotent requests.
EOF

# --- 5. GC pauses ------------------------------------------------------
cat > "$DEST/gc-tuning.md" <<'EOF'
# Stop-the-world latency in Go services

The Go runtime's collector is concurrent for most of its work but
still needs brief stop-the-world phases — marking roots and finalising
the mark phase. Modern releases have squeezed these into sub-millisecond
intervals on small heaps; on heaps over 20 GB you start seeing tens of
milliseconds, which is enough to matter for tail-latency-sensitive
endpoints.

The biggest lever for hot paths is allocation rate. Every megabyte you
allocate per second is more work the collector has to do. Pooling
short-lived buffers via sync.Pool is the simplest fix. Switching from
interface-typed return values to concrete types removes a heap
escape. Switching from map of string to map of int when the keys are
bounded saves a string allocation per lookup.

GOGC controls the trigger ratio. Lower it for less memory at the cost
of more frequent collection; raise it for fewer cycles at the cost of
larger working set.
EOF

# --- 6. moving off MongoDB --------------------------------------------
cat > "$DEST/why-we-left-mongo.md" <<'EOF'
# Lessons from migrating a document store

We picked Mongo in 2019 for schema flexibility. The application data
genuinely was tree-shaped, the team was new to operating databases,
and the ergonomics of "just shove the JSON in" were attractive.

By 2023 we had three reasons to leave. First, every interesting
query had grown into an aggregation pipeline that nobody could read.
Second, our analytics team had given up trying to query the document
store and was nightly-snapshotting to Postgres anyway. Third, the
schema HAD stabilised — we were enforcing JSON Schema at the
application layer, which is the worst of both worlds.

The migration took eleven weeks. The dual-write phase ran for four of
those. We discovered three bugs that had been silently corrupting the
Mongo collections for years; the migration script choked on them and
forced us to actually look. Postgres rejected the broken rows on
INSERT, which is the kind of "good loud failure" you want.
EOF

# --- 7. observability / OpenTelemetry ---------------------------------
cat > "$DEST/tracing.md" <<'EOF'
# Watching requests cross service boundaries

A trace is a tree of spans. Each span has a parent, a duration, and
some attributes. Spans propagate across processes via a context
header — W3C traceparent these days — so the receiving service can
make its spans children of the caller's.

The two interesting failure modes are sampling and clock skew.
Sampling: most services can't afford to record every request, so they
sample with some probability and only the sampled requests show up in
the trace store. Skew: if two services have clocks that differ by
more than the request duration, the child span can appear to start
before its parent. Most viewers paper over this by lying about the
timestamps.

OpenTelemetry is now the industry's lingua franca — instrumentation
libraries for every language, a wire format both Jaeger and Tempo
ingest, vendor backends that pick it up without translation.
EOF

# --- 8. caching strategies --------------------------------------------
cat > "$DEST/caching-patterns.md" <<'EOF'
# Living with stale data

Three patterns cover most of the design space.

Cache-aside: the application checks the cache, falls through to the
source of truth on miss, writes the result into the cache. Simple,
but the cache is updated by every reader, which can produce a
stampede when a popular key expires.

Write-through: writes go to the cache and the source in the same
operation. The cache is always fresh. The price is that writes are
slower and a partial failure leaves the two stores disagreeing.

Write-behind: writes go to the cache immediately and to the source
asynchronously. Writes are fast; reads are warm; durability is the
worry — if the cache node dies before the async write lands, that
update is gone.

Whichever you pick, the question of "when does this entry expire" is
where the real decisions live. Wall-clock TTL is the default but the
wrong default for anything event-driven.
EOF

# --- 9. rate limiting --------------------------------------------------
cat > "$DEST/throttling.md" <<'EOF'
# Limiting how often a client can call you

The token bucket is the most flexible algorithm. The bucket holds N
tokens; each request consumes one; tokens refill at a fixed rate.
Bursts up to N are allowed; the steady-state ceiling is the refill
rate. Implementations are a few lines if you allow the bucket to be
fractional.

The leaky bucket inverts the framing: requests enter a queue at any
rate, the queue drains at the configured rate, and overflow gets
rejected. Same steady-state behaviour, different burst semantics.

Fixed and sliding windows are easier to reason about for billing
periods but mishandle the edges. A client that times its calls right
can get 2N requests in two seconds when the limit is N per second.

For distributed enforcement, the actual hard problem is shared state.
Each replica seeing its own slice of the traffic will collectively
over- or under-shoot. Redis with INCR-then-EXPIRE is the usual
compromise.
EOF

# --- 10. monorepo vs polyrepo ------------------------------------------
cat > "$DEST/code-organization.md" <<'EOF'
# Where to draw repository boundaries

The case for one repo: refactors across components are atomic. The
build system can dedupe shared dependencies. Search and code review
work on the obvious unit. Tooling investment pays off everywhere at
once.

The case for many repos: ownership boundaries are explicit. CI runs
are smaller. The repo's contents fit in one engineer's head. Open-
sourcing or selling a piece is a matter of changing a remote URL.

In practice the line is drawn around release cadence. Things that
ship together belong in one repo. Things that ship on independent
schedules and have stable API contracts belong apart.

Tooling is the asymmetric multiplier. A monorepo without remote build
caching, sparse checkouts, and a build-graph-aware CI is a tax on
every engineer. A polyrepo without dependency-update automation
collects security alerts faster than anyone can review them.
EOF

# --- 11. dependency management ----------------------------------------
cat > "$DEST/dependency-hygiene.md" <<'EOF'
# Keeping third-party code under control

The first rule is "pin every version". Floating ranges break
reproducible builds and make compromise surface area unpredictable.
Lock files exist for a reason; commit them.

The second rule is "audit transitive depth". A direct dependency you
chose carries an indirect graph you didn't. The 2021 left-pad story
and every npm supply-chain incident since has been about transitive
trust, not direct trust.

Automation closes the loop: tools that open a PR per upgrade make the
review surface small and predictable. The expensive habit is
batching upgrades for a quarterly cleanup; by then the diff is huge
and risky and nobody wants to do it.
EOF

# --- 12. zero-downtime migrations -------------------------------------
cat > "$DEST/schema-migrations.md" <<'EOF'
# Changing the shape of a live database without breaking it

The pattern is expand-then-contract. To rename a column from foo to
bar, you first add bar as a nullable column, then deploy code that
writes to both foo and bar (with a default-old behaviour on conflict),
then backfill bar from foo, then deploy code that reads bar, then
finally drop foo.

It's slower than just rewriting the schema. It's also the only way to
do it without a maintenance window. Every step is independently
revertible, which means a bug discovered in step three doesn't require
unwinding two and one.

The expensive bit is patience. Each step needs to bake — sometimes
hours, sometimes a release cycle — before the next step is safe. The
team that tries to compress this into one PR is the team that learns
why this pattern exists.
EOF

echo "[done] $(ls "$DEST"/*.md | wc -l | tr -d ' ') markdown files in $DEST"
ls -lh "$DEST"/*.md | awk '{print $9, $5}'
