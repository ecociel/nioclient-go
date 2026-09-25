# Go Client for NIO Authorization

A Go client library for the **NIO Authorization Service**, a high-performance, relationship-based authorization system.
This library provides a gRPC client to interact with the service and includes middleware for easy integration
with the `julienschmidt/httprouter` framework.

# Usage

See the [cmd](cmd) directory for how to use with httprouter and for how to use the client.

# Run nio locally

`docker-compose.yml` starts nio `check` and `nio-client` on SQLite, so the
programs in `cmd` have a server to talk to.

1. Clone [ecociel/nio](https://github.com/ecociel/nio) next to this
   repository, as `../nio`. Use nio `main` at `f7569b9` or later. Build the two
   images in that checkout:

       task build:check:sqlite build:client:sqlite

   The tasks tag the images `nio-check:sqlite` and `nio-client:sqlite`. The
   Taskfile sets `ARCHDIR` to `aarch64-linux-gnu` by default. On an x86_64
   host, add `ARCHDIR=x86_64-linux-gnu` to the `task` command.

2. Start the stack in this repository:

       docker compose up -d

3. Wait until both gRPC services answer. nio has no gRPC reflection, so give
   `grpcurl` the proto files from the nio checkout:

       grpcurl -plaintext -import-path ../nio/proto -proto iam.proto \
         -d '{"ns":"project","obj":"p42","rel":"project.get","userId":"1"}' \
         localhost:50052 am.CheckService/check
       grpcurl -plaintext -import-path ../nio/proto -proto sessions.proto \
         -d '{"token_hash":"0000000000000000000000000000000000000000000000000000000000000000"}' \
         localhost:50053 am.SessionService/resolve

   The first command prints `"ok": true`. The second prints `"notFound": {}`.

| Host port | Service |
| --- | --- |
| 50052 | `am.CheckService` on `check` |
| 50053 | `am.SessionService` on `nio-client` |
| 8090 | `nio-client` sign-in pages, under `http://localhost:8090/auth` |

The stack creates three local users. Each has the password `123456`.

| User | ID | Grants |
| --- | --- | --- |
| `admin@local.local` | 1 | `iam:root#admin`, `project:p42#owner`, `project:p65#viewer` |
| `anna@local.local` | 2 | `project:p42#editor`, `project:p65#viewer` |
| `bob@local.local` | 3 | `project:p42#viewer`, `project:p65#viewer`, `article:a1#viewer` |

Sign in as `bob` and keep the `session` cookie:

    curl -s -c cookies.txt -o /dev/null -X POST http://localhost:8090/auth/signin \
      --data-urlencode 'email=bob@local.local' \
      --data-urlencode 'password=123456' \
      --data-urlencode 'tenant_id=default' \
      --data-urlencode 'back=/'

The cookie is valid for the host `localhost` only. Send it to `localhost`, not
to `127.0.0.1`.

`go run ./cmd/server` listens on port 8080 and guards `/articles/:id` with the
`article` namespace.

A signed-in request to `cmd/server` does not work yet. nio `main` sends and
expects user IDs as integers (nio issue #301). This client still sends them as
strings. check refuses the request, and `cmd/server` answers HTTP 500 with
`missing expected field: user`. A request without a cookie works: it gets a
`303` to `/signin`.

The stack is for local development only. It turns off client certificates on
both gRPC services and uses a fixed, public `TENANT_ENCRYPTION_KEY`.

The databases live in `./test`. Stop the stack with `docker compose down`.
Delete `./test` to start again from empty databases.


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

# Construction

`check` and `nio-client` (session) are always separate TCP endpoints. The
library does not read environment variables; the process supplies targets,
credentials, and cache config.

Two client types keep misuse a **compile error**:

| Type | Constructor | Use |
|---|---|---|
| `*Client` | `New(checkConn)` | RPC only (Check / List / Write / …) |
| `*SessionClient` | `NewWithSession(check, session, opts…)` | RPC + session; implements `Wrapper` for `Wrap` |

```go
// RPC-only — cannot be passed to Wrap
checkConn, err := nioclient.DialCheck(checkTarget, checkCreds)
rpc := nioclient.New(checkConn)

// HTTP Wrap — both channels; type is *SessionClient
sessionConn, err := nioclient.DialSession(sessionTarget, sessionCreds)
web := nioclient.NewWithSession(checkConn, sessionConn,
    nioclient.WithPrefix("/app"),
    nioclient.WithResolverConfig(nioclient.ResolverConfig{
        Capacity: 10000,
        L1TTL:    30 * time.Second,
        NegTTL:   2 * time.Second,
    }),
)
router.GET(route, nioclient.Wrap(web, extract, handler))
```

Dial helpers enable HTTP/2 keepalive (30s / 10s / while idle — nio #239).
`LoadTLSCredentials(cert, key, ca, serverName)` builds mTLS credentials from
paths; empty cert+key yields insecure (dev only). `DefaultResolverConfig()` is
used when `WithResolverConfig` is omitted.

# Session resolution

Opaque session tokens are resolved via `am.SessionService` on nio-client
(issue #243/#245) on `*SessionClient` only. Wrap hashes the cookie token
(`sha256`, hex — the raw token never leaves the process), resolves it, and
sends the principal UUID to `check`. Unknown / expired / revoked tokens
redirect to signin with zero check RPCs.

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

`ContentChangeCheck` authorizes a content modification at the freshest snapshot
and returns the zookie to store with the new content version.

`Watch(ctx, ns, startTs)` tails the changelog for a namespace (paper §2.4.6).
Call `Recv` on the returned stream: empty `Updates` is a heartbeat; non-empty
is one atomic write at `Ts`. Resume from any received `Ts` (exclusive).

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




