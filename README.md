# s3-lite — build S3's object model, one commit at a time

A from-scratch, **commit-by-commit** reimplementation of the mechanisms behind
Amazon S3 in Go: content-addressed blob storage with crash-safe atomic
writes, an S3-shaped HTTP API, multipart upload, prefix/delimiter listing,
object versioning with delete markers, and conditional requests as
optimistic concurrency control.

This is the sibling of [`db45`](../db45) (an LSM-tree KV store) and
[`postgres-in-steps`](../postgres-in-steps) (a page-based OLTP engine) in
this workspace, built in the same format, but a different shape of system:
those two are engines behind a *query* interface; this one is a durable
blob store behind an *HTTP object* interface — the defining mechanism is
content addressing and versioned, conditional writes rather than a query
planner or a buffer pool. "All of S3" is not a reachable target (multi-tenant
buckets, IAM, lifecycle rules, cross-region replication, real XML request
signing), so this build scopes down to the ideas that make an object store
actually work: atomic durability of a single object write, content hashing
as an ETag, chunked/multipart upload reassembly, and versioning as an
append-only history per key rather than in-place mutation.

Same teaching format as its siblings: **one Go package, one numbered step
per git commit**, each commit leaves `go test ./...` green.

## The arc

```
Chapter 1   0101-0103   the blob store           filesystem key encoding, atomic Put/Get/Delete/Head
Chapter 2   0201-0202   content integrity        streaming hash, ETag
Chapter 3   0301-0303   the HTTP API             net/http surface, S3-shaped errors
Chapter 4   0401-0403   multipart upload         Create/UploadPart/Complete/Abort, out-of-order parts
Chapter 5   0501-0503   listing                  prefix + delimiter, common prefixes, pagination
Chapter 6   0601-0603   versioning               version IDs, delete markers, list-object-versions
Chapter 7   0701-0703   conditional requests     metadata, If-Match/If-None-Match, conditional PUT as CAS
Chapter 8   0801-0803   closing                  Server wiring, examples/basic, docs
```

(Chapter/step numbering may merge or split a step from this initial plan as
the actual work lands — each chapter's closing commit message notes what
changed and why, same convention `db45`/`postgres-in-steps` use.)

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

See `examples/basic/main.go` for a runnable demo (`go run ./examples/basic`).

## Running the tests

```bash
go test ./...
```

Requires Go 1.21+.

## Source layout

See the table in this section once Chapter 8 lands; until then, `git show`
each step or read `COMMITS.md` for the running walkthrough.

## Credits

Curriculum scoped and built as a study project, in the same spirit and
format as the sibling [`db45`](../db45) and [`postgres-in-steps`](../postgres-in-steps)
builds in this workspace.
