# Implementation prompt: request-scoped check memoization

Turn the working **prototype** (`requestmemo.go`, the `WithRequestMemo()` option
wired into `Wrap`, and the memo tests in `wrap_test.go`) into the production
feature. This is "option 1" of the client-cache design: cache authorization
**decisions** for the lifetime of a single HTTP request so a page that runs many
`HasRel`/`List` calls collapses to far fewer gRPC round-trips.

## Why this and not a general check cache

A request is **one logical instant**, so asking the same authorization question
twice within it must return the same answer — memoizing is therefore free of the
"new-enemy" staleness risk that makes a *cross-request* decision cache unsafe.
Cross-request caching, tuple `Watch`-based invalidation, and a client-side tuple
evaluator are explicitly **out of scope** (that reverse-index work belongs
server-side; see the design discussion). Do not add them here.

## Current state (prototype — already merged in the working tree)

- `requestmemo.go`: `wrapConfig`, `WrapOption`, `WithRequestMemo()`, and
  `checkMemo` (mutex-guarded `map[checkMemoKey]checkMemoVal`, keyed on
  `(ns, obj, rel, userId)`, never caches errors).
- `wrap.go`: `Wrap(..., opts ...WrapOption)` parses options and, when enabled,
  wraps the final `user.check` (after the `check_ts` override) with the memo.
- Tests: `TestWrapRequestMemoDedupesChecks` (2 underlying checks with memo),
  `TestWrapWithoutMemoRepeatsChecks` (4 without), `TestCheckMemoDoesNotCacheErrors`.

The prototype covers **Check only**, has **no singleflight**, and adds **no
metrics**. Everything below is the delta to production-ready.

## Normative design

### Key and timestamp
- Cache key is `(ns, obj, rel, userId)`. The request's check timestamp is fixed
  for the whole request (it is baked into the wrapped `user.check` closure —
  epoch, or the `check_ts` cookie value), so it is **not** part of the key. If a
  future change lets the timestamp vary mid-request, add `ts` to the key.
- `userId` is the resolved principal (post-#245): the gate check and every
  `HasRel` use the same principal, so their keys align and the gate populates
  the memo for the handler.

### Opt-in, per route
- Keep it opt-in via `WithRequestMemo()` (default off). Per-route control is the
  point: enable it on read handlers, leave it off on mutation handlers. Do **not**
  make it always-on and do **not** reintroduce an optional-interface probe on
  `Wrapper` — options flow through `Wrap`'s variadic `opts`.

### Read-after-write caveat (document loudly)
- A handler that writes a tuple and then re-checks expecting to observe its own
  write MUST NOT enable the memo — it would see the pre-write decision. Call this
  out in the `WithRequestMemo` doc comment and the README.
- Optional hardening (only if a real handler needs it): expose a way to clear the
  request memo, e.g. a `User` method `InvalidateChecks()` (or auto-clear when a
  write goes through the same request scope). Prefer documentation over machinery
  unless there's a concrete caller.

### Concurrency
- Handlers may fan checks out across goroutines, so the memo stays
  mutex-guarded and must pass `go test -race`.
- Add **per-request singleflight**: two concurrent identical misses should
  trigger one underlying check, not two. Reuse the pattern already in
  `session.go` (`golang.org/x/sync/singleflight`, keyed by the same struct
  serialized to a string, scoped to the per-request memo instance). Keep it
  bounded to the request — do not share a singleflight group across requests.

### Errors
- Never cache errors (prototype already does this). An errored check must be
  retryable within the same request.

### List memoization
- Add `List` memoization symmetrically, keyed on `(ns, rel, userId)`, wrapping
  `user.list` the same way `user.check` is wrapped. Guard the returned slice:
  either return a defensive copy or document that callers must not mutate it
  (a shared cached slice mutated by one caller would corrupt others). Prefer the
  copy — `List` results are small and correctness beats the allocation.

### Metrics / observability
- Mirror the existing `WithObserveCheck` / `WithObserveList` hooks: expose a way
  to observe memo hit/miss counts (e.g. an optional callback on the option, or
  reuse the observe funcs with a `cached bool`). Keep it optional and off by
  default.

## Acceptance criteria

- With `WithRequestMemo()`, a handler issuing N identical `(ns,obj,rel,principal)`
  checks triggers exactly **one** underlying `Check`; distinct keys each trigger
  one. Without the option, every call hits the wrapper (current behavior).
- `List` memoization behaves the same on `(ns,rel,userId)`; the returned slice is
  safe against caller mutation.
- Concurrent identical checks within one request collapse to a single underlying
  call (singleflight) and the suite passes under `-race`.
- Errors are never cached.
- Existing call sites `Wrap(w, e, h)` still compile unchanged (variadic option).
- README documents the option, the per-route opt-in, and the read-after-write
  caveat.

## Files to touch

- `requestmemo.go` — singleflight, `List` memo, optional metrics hook.
- `wrap.go` — wrap `user.list` alongside `user.check` when enabled.
- `wrap_test.go` — add: `List` dedupe, concurrent-miss singleflight (`-race`),
  slice-mutation safety.
- `README.md` — "Request-scoped memoization" subsection with the caveat.
- `cmd/server/main.go` — optionally demonstrate `WithRequestMemo()` on the GET route.

## Non-goals (do not implement)

- Cross-request / TTL decision caching (that is option 2, a separate, opt-in,
  bounded-staleness cache — do not conflate).
- Tuple `Watch`-based invalidation or any client-side tuple evaluator.
- Caching across principals or requests of any kind.
