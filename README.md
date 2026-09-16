# s3-lite — build S3's object model, one commit at a time

A from-scratch, **commit-by-commit** reimplementation of the mechanisms
behind Amazon S3 in Go: content-addressed blob storage with crash-safe
atomic writes, an S3-shaped HTTP API, multipart upload, prefix/delimiter
listing, object versioning with delete markers, and conditional requests as
optimistic concurrency control.

This is the sibling of [`db45`](../db45) (an LSM-tree KV store) and
[`postgres-in-steps`](../postgres-in-steps) (a page-based OLTP engine) in
this workspace, built in the same format, but a different shape of system:
those two are engines behind a *query* interface; this one is a durable
blob store behind an *HTTP object* interface. The defining mechanisms are
content addressing (identical bytes are stored once, no matter how many
keys or versions point at them) and append-only, conditionally-written
version history, rather than a query planner or a buffer pool. "All of S3"
is not a reachable target (multi-tenant buckets, IAM, lifecycle rules,
cross-region replication, real request signing), so this build scopes down
to the ideas that make an object store actually work — every scope cut is
recorded in `DESIGN.md`, right where it comes up.

Same teaching format as its siblings: **one Go package, one numbered step
per git commit**, each commit leaves `go test ./...` green.

## The arc

```
Chapter 1   0101-0103   the blob store           filesystem key encoding, atomic Put/Get/Head/Delete
Chapter 2   0201-0202   content integrity        streaming hash, ETag
Chapter 3   0301-0302   the HTTP API             net/http surface, S3-shaped errors
Chapter 4   0401-0402   multipart upload         Create/UploadPart/Complete/Abort, out-of-order parts, composite ETag
Chapter 5   0501-0502   listing                  prefix + delimiter, common prefixes, pagination
Chapter 6   0601-0602   versioning               content-addressed blobs, version history, delete markers
Chapter 7   0701-0702   conditional requests     metadata, If-Match/If-None-Match, conditional PUT as CAS
Chapter 8   0801         closing                  examples/basic, final docs
```

(Chapter/step numbering follows the plan this project was scoped from; most
chapters landed in two steps instead of three — a store-layer step and an
HTTP-wiring step that closes the chapter — see each chapter's closing
commit message for what changed from the original plan and why. Chapter 6
is a genuine mid-course refactor, the same kind db45's Step 0603 makes for
its in-memory structure: it replaces the one-file-per-key model of
Chapters 1-2 outright with content-addressed blobs plus per-key version
history.)

## How to read it

```bash
git log --reverse --oneline     # every step, oldest first
git show <hash>                  # one step's diff + the note on what it teaches
go test ./...
```

To build/run at a specific step:

```bash
git checkout <hash>      # detached HEAD at that step
go test ./...
git checkout master      # back to the finished store
```

## Try it

```bash
go run ./examples/basic
```

`examples/basic/main.go` starts a real server on a loopback port and drives
it with plain `net/http` requests: create a bucket, PUT an object, run a
multipart upload assembled out of order, list by prefix, read an old
version back after overwriting the key, and watch a conditional PUT
correctly get rejected with 412 when its `If-Match` ETag has gone stale.

## Running the tests

```bash
go test ./...          # everything
go test ./... -race    # Chapter 7's compare-and-swap claim is checked under -race
```

Requires Go 1.21+.

## Source layout

| file | role |
| --- | --- |
| `keypath.go` | `EncodeKey`/`EncodeBucket`: map an S3 key/bucket onto a safe filesystem path |
| `atomic.go` | `writeFileAtomic`/`syncDir`/`removeFileAtomic`: the crash-safety primitive everything else is built on |
| `etag.go` | `hashingReader`: stream a content hash (S3's ETag) while writing |
| `store.go` | `Store`: `Put`/`Get`/`Head`/`Delete`, `PutOptions` (metadata + conditional writes), the content-addressed blob writer |
| `version.go` | the version-history model: `objectVersion`/`objectMeta`, `addVersion`'s locked compare-and-swap, `DecodeKey`'s inverse of `EncodeKey`, `DeleteVersion` |
| `multipart.go` | `CreateMultipartUpload`/`UploadPart`/`CompleteMultipartUpload`/`AbortMultipartUpload`, the composite-ETag construction |
| `list.go` | `List`: sorted, prefix-filtered, delimiter-grouped, paginated key enumeration |
| `server.go` | `Server`: S3-style path routing and the plain object/bucket HTTP verbs |
| `errors_http.go`, `conditional_http.go`, `list_http.go`, `multipart_http.go` | the HTTP-shaped pieces: S3 error XML, metadata/conditional-header handling, `ListObjectsV2` XML, multipart's query-param wiring |

See `DESIGN.md` for the scope decisions and simplifications made along the
way — what's real S3 fidelity versus a deliberate, documented cut.

## Credits

Curriculum scoped and built as a study project, in the same spirit and
format as the sibling [`db45`](../db45) and
[`postgres-in-steps`](../postgres-in-steps) builds in this workspace.
