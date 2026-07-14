# Go Client for NIO Authorization

A Go client library for the **NIO Authorization Service**, a high-performance, relationship-based authorization system.
This library provides a gRPC client to interact with the service and includes middleware for easy integration
with the `julienschmidt/httprouter` framework.

# Usage

See the [cmd](cmd) directory for how to use with httprouter and for how to use the client.


# Updating gRPC Code

Run

    go generate ./...

to update the protobuf generated files.

The original proto files track the nio server:

- [nio/proto/iam.proto](https://github.com/ecociel/nio/blob/main/proto/iam.proto) (authorization: check, list, expand, write, watch, …)
- [nio/proto/sessions.proto](https://github.com/ecociel/nio/blob/main/proto/sessions.proto) (session resolve)

`proto/iam.proto` and `proto/sessions.proto` should match the nio server you
deploy against so wire fields (e.g. `ListResponse.ts`, packed write zookies)
are visible to this client.

# Built-in namespaces and the admin gate

Constants mirror nio's `domain` crate and check bootstrap. Built-in namespaces
are `NsIam` (`iam`) and `NsServiceAccount` (`serviceaccount`) only.

The singleton admin object is `iam:root` (`NsIam` + `ObjRoot`). Typical gate
relations:

- viewer path: `RelIamGet` (`iam.get`)
- admin path: `RelIamUpdate` (`iam.update`), `RelServiceAccountCreate`

Roles that carry direct grants: `RelAdmin`, `RelEditor`, `RelViewer`. Public
subject markers: `UserIdAllUsers`, `UserIdAuthenticatedUsers`. The pointer
object/rel keyword is `"..."` (`ObjUnspecified` / `RelUnspecified`).

# Session resolution

Opaque session tokens are resolved to a principal via `am.SessionService` on
nio-client (issue #243/#245). The check and session channels are both required
by the constructor; the middleware hashes the cookie token (`sha256`, hex — the
raw token never leaves the process), resolves it, and sends the resolved
principal UUID to `check`. An unknown/expired/revoked token redirects to signin
with zero check RPCs.

    sessionConn, err := nioclient.DialSessionFromEnv() // NIO_SESSION_URI + SESSION_GRPC_TLS_*
    if err != nil {
        log.Fatalf("session channel: %v", err)
    }
    nioClient := nioclient.New(checkConn, sessionConn)

Resolver tunables (env): `SESSION_L1_CAPACITY` (10000), `SESSION_L1_TTL`
seconds (30), `SESSION_NEG_TTL` seconds (2), `SESSION_STALE_IF_ERROR` seconds
(0 = off).

# Zookies (timestamps)

Check/list/write use **opaque packed zookies** (standard Base64 of 7 bytes:
`[epoch:u8][millis:u48 BE]`). Treat them as opaque: store and echo only.

- `TimestampEmpty` (`AQAAAAAAAA==`) — no fresher-than constraint; server picks a snapshot
- Write helpers (`AddOneUserId`, `AddOneUserSet`, `DeleteOne*`, `Write`) return the **commit** zookie
- `ListResult` / `ListWithTimestamp` return the **evaluation** snapshot zookie
- Pass a zookie into `CheckWithTimestamp` / `ListWithTimestamp` for read-your-writes

```go
ts, err := client.AddOneUserId(ctx, ns, obj, rel, userId)
// ...
principal, ok, err := client.CheckWithTimestamp(ctx, ns, obj, rel, userId, ts)
```

`Write(ctx, add, del, precondition)` supports atomic multi-tuple commits and an
optional OCC precondition zookie (`nil` = unconditional).

# Request-scoped check memoization

Pass `WithRequestMemo()` to `Wrap` to memoize check and list decisions for the
lifetime of a single request — a page running many `HasRel`/`List` calls for the
same subject collapses to far fewer RPCs:

    router.GET(route, nioclient.Wrap(nioClient, extract, handler, nioclient.WithRequestMemo()))

Identical `(ns, obj, rel, principal)` checks (and `(ns, rel, principal)` lists)
are answered from an in-request cache; concurrent identical misses are collapsed
with singleflight so a handler fanning checks across goroutines still issues one
RPC per key. List results are copied on return, so callers may mutate them
freely.

This is free of staleness risk (a request is one logical instant) but is
**opt-in per route**: do not enable it on a handler that writes a tuple and then
re-checks expecting to observe its own write.

Use `WithRequestMemoObserver(func(op string, hit bool){…})` instead to enable the
memo and observe hit/miss per lookup (`op` is `"check"` or `"list"`). See
[docs/request-memo-impl.md](docs/request-memo-impl.md) for the design and
non-goals.

# License

This project is licensed under the **Apache 2.0 License**. See the [LICENSE](https://www.google.com/search?q=LICENSE) file for details.




