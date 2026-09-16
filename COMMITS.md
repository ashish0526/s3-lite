# s3-lite — the commit-by-commit history

This document walks every commit in this repository, oldest first, in the
order they actually landed. It exists as a single place to read *why* each
piece was built the way it was, without having to `git show` your way
through 17 commits one at a time — though `git show <hash>` on any commit
below will still get you the literal diff.

The repo is built the way [`db45`](../db45) and
[`postgres-in-steps`](../postgres-in-steps) are: **one Go package, one
numbered step per commit, every commit leaves `go test ./...` green.** Those
two build OLTP engines behind a query interface (an LSM-tree, and a
page-based store with a buffer pool and MVCC); this one builds a durable
blob store behind an HTTP object interface — content addressing and
conditionally-written version history are the defining mechanisms here,
not a query planner.

Eight chapters, 17 commits (Step 0000 through Step 0801):

| Chapter | Steps | Theme |
| --- | --- | --- |
| 0 | 0000 | scaffold |
| 1 | 0101–0103 | the blob store |
| 2 | 0201–0202 | content integrity |
| 3 | 0301–0302 | the HTTP API |
| 4 | 0401–0402 | multipart upload |
| 5 | 0501–0502 | listing |
| 6 | 0601–0602 | the versioned storage model |
| 7 | 0701–0702 | conditional requests |
| 8 | 0801 | closing |

---

## Chapter 0 — scaffold

### Step 0000 — project scaffold
`d36dfe9` · `go.mod`, `.gitignore`, `README.md` · 3 files, +87

The Go module (`github.com/ashish0526/s3-lite`), an initial README laying
out the 8-chapter curriculum, and a `.gitignore`. Declares its premise up
front: sibling of `db45` and `postgres-in-steps`, same one-commit-per-step
teaching format, but building a content-addressed, versioned blob store
behind an HTTP object API instead of a query engine.

---

## Chapter 1 — the blob store

Everything in this chapter builds the same mechanism every later chapter
depends on: an object's bytes, addressed by nothing more than
`(bucket, key)`, written and deleted so a crash mid-write never leaves a
reader observing a partial file.

### Step 0101 — filesystem key encoding
`9b670d8` · `keypath.go`, `keypath_test.go` · 2 files, +125

`EncodeKey`/`EncodeBucket` map an S3-style key (arbitrary bytes, cosmetic
`/` separators) onto a safe relative filesystem path: each segment goes
through `url.PathEscape`, with `.`/`..` specially remapped first
(`=2e`/`=2e2e`) since `PathEscape` leaves them untouched and a literal `..`
directory component would walk out of the bucket's data directory.
Repeated slashes collapse, matching S3's own flat namespace. A key segment
spelled exactly like the escape output can collide with an encoded `.` — a
documented "-lite" gap (`DESIGN.md`).

### Step 0102 — atomic writes
`daa7843` · `atomic.go`, `atomic_test.go` · 2 files, +141

`writeFileAtomic`/`syncDir`/`removeFileAtomic`: the crash-safety primitive
everything else builds on. A write lands in a temp file in the target's own
directory, gets fsynced, then `os.Rename` swaps it into place, and the
parent directory itself is fsynced afterward — the same durability lesson
as `db45`'s `Log.Write` fsync step and `postgres-in-steps`'s write-ahead
rule, applied here to whole-object writes.

### Step 0103 — Put/Get/Head/Delete, closing Chapter 1
`17a891d` · `atomic.go`, `store.go`, `store_test.go` · 3 files, +265/-1

`Store` wires key encoding and atomic writes into the four operations
everything else builds on. Writing the closing round-trip test surfaced a
real bug in Step 0102's `removeFileAtomic` (fsyncing a directory that was
never created); fixed forward here rather than reopening Step 0102, the
convention this build uses throughout for bugs a later step's test finds
in an earlier step's code.

---

## Chapter 2 — content integrity

### Step 0201 — streaming content hash
`26bd964` · `etag.go`, `etag_test.go` · 2 files, +77

`hashingReader` wraps any `io.Reader`, hashing (SHA-256) every byte as it's
read, so a content hash falls out of a write's existing single pass over
the data instead of a second read-back pass.

### Step 0202 — ETag on Put/Head, closing Chapter 2
`3e110fe` · `store.go`, `store_test.go` · 2 files, +88/-18

`Put` streams through the hashing reader and returns `PutResult{ETag,
Size}`; a `.etag` sidecar file next to the object's bytes is what `Head`
reads back. This sidecar is scaffolding — Chapter 6 replaces it outright
with a proper per-key metadata file written as part of a versioned write.

---

## Chapter 3 — the HTTP API

### Step 0301 — S3-shaped HTTP errors
`d454ff2` · `errors_http.go`, `errors_http_test.go` · 2 files, +75

`apiError` is the same small XML document real S3 returns on failure: a
stable `Code` an SDK branches on, plus a human `Message`. Built before the
router so every handler gets S3-shaped errors for free.

### Step 0302 — the HTTP object API, closing Chapter 3
`a5045f1` · `server.go`, `server_test.go` · 2 files, +236

`Server.ServeHTTP` does path-style routing the way S3 itself does: the
first path segment is the bucket, everything after it is the key. PUT on a
bucket-only path creates the bucket; PUT/GET/HEAD/DELETE on a bucket+key
path map onto `Store`'s methods, with the ETag surfaced as a quoted
response header the way S3's wire format does. No bucket listing, no auth,
no request signing — a single-tenant store behind an HTTP surface shaped
like S3's object operations, not the full API.

---

## Chapter 4 — multipart upload

### Step 0401 — multipart upload, store layer
`c09e296` · `multipart.go`, `multipart_test.go` · 2 files, +359

`CreateMultipartUpload`/`UploadPart`/`CompleteMultipartUpload`/
`AbortMultipartUpload`, one scratch directory per upload ID holding a
`target.key` sidecar every later call is checked against. Parts can upload
in any order; completion validates the caller's part list (ascending,
non-duplicate part numbers, each ETag matching what was actually uploaded)
before concatenating in order. The final ETag is deliberately *not* a hash
of the assembled bytes: it's `hex(sha256(concat of each part's raw
digest))` + `"-" + partCount`, the same composite-ETag construction real S3
uses (with MD5 instead of SHA-256) — reproduced on purpose, complete with
the "ETag contains a dash" tell for "this object was multipart-uploaded."

### Step 0402 — multipart over HTTP, closing Chapter 4
`0d5f62e` · `errors_http.go`, `multipart_http.go`, `multipart_http_test.go`, `server.go` · 4 files, +192

S3's own query-param convention on the object URL: `POST ?uploads`
initiates, `PUT ?partNumber=N&uploadId=ID` uploads one part, `POST
?uploadId=ID` with an XML `<CompleteMultipartUpload>` body completes,
`DELETE ?uploadId=ID` aborts. Writing the closing abort test surfaced a gap
in Step 0301's error mapping (no case for the multipart error sentinels,
so a call against an aborted upload fell through to a bare 500 instead of
404) — fixed forward here.

---

## Chapter 5 — listing

### Step 0501 — listing, store layer
`602ea6f` · `list.go`, `list_test.go` · 2 files, +295

`DecodeKey` reverses `EncodeKey`, which is what makes it possible to
recover real keys while walking the objects directory. `List` walks that
directory, sorts by key, filters by prefix, and — when a delimiter is
given — groups everything sharing a prefix up to and including the
delimiter into a common-prefix entry instead of listing it as an object,
the same trick S3 uses to fake a directory hierarchy over a flat keyspace.
Objects and common-prefix groups merge into one ordered list before
pagination logic runs, so a continuation token and `maxKeys` cut across
both kinds of entry in the same sorted order S3 itself returns them in.

### Step 0502 — ListObjectsV2 over HTTP, closing Chapter 5
`4513766` · `list_http.go`, `list_http_test.go`, `server.go` · 3 files, +105

`GET /bucket?list-type=2` with `prefix`/`delimiter`/`continuation-token`/
`max-keys`, wired onto `Store.List`, returned as the same
`ListBucketResult` XML shape S3 clients already parse.

---

## Chapter 6 — the versioned storage model

### Step 0601 — the versioned storage model (refactor)
`b286753` · `errors_http.go`, `list.go`, `multipart.go`, `multipart_test.go`, `server.go`, `store.go`, `store_test.go`, `version.go`, `version_test.go` · 9 files, +441/-113

Replaces the "one file per key" model from Chapters 1-2 outright, the same
kind of load-bearing refactor `db45`'s Step 0603 makes for its in-memory
structure. Object bytes now live in a content-addressed blob keyed by
ETag, shared across every version and every key whose content happens to
hash the same; a key's version history — an ordered, append-only list of
`{VersionID, ETag, Size, Deleted, ModTime}` — lives in a small per-key JSON
metadata file. `Put` always adds a new version; `Delete` appends a delete
marker instead of removing bytes. This step touches every chapter that
assumed one-file-per-key: `List` now walks `*.meta.json` files, multipart
completion writes its assembled bytes into the blob store under its
composite ETag, and the HTTP layer passes an empty version ID through for
today's unchanged "always the latest" behavior.

### Step 0602 — GET/HEAD/DELETE by version ID over HTTP, closing Chapter 6
`7984d3b` · `server.go`, `version.go`, `version_http_test.go` · 3 files, +135/-9

`?versionId=` on GET/HEAD resolves a specific version; every write and read
response carries `x-amz-version-id`. DELETE gained real S3's second
behavior: no `?versionId` still just adds a marker, an explicit
`?versionId` permanently erases that one version's record
(`Store.DeleteVersion`) without touching the blob it pointed at. Scope cut
from the chapter's original 3-step plan: list-object-versions is not
implemented (`DESIGN.md`) — Chapter 5's `ListObjectsV2` already
demonstrates the pagination mechanics this would mostly repeat.

---

## Chapter 7 — conditional requests

### Step 0701 — metadata and conditional PUT, store layer
`c45d30a` · `conditional_test.go`, `multipart.go`, `store.go`, `version.go` · 4 files, +223/-17

`PutOptions`/`PutWithOptions` add user metadata and conditional writes:
`IfMatch` requires the key's current live ETag to equal a given value (the
parallel to `postgres-in-steps`'s MVCC write-conflict check), `IfNoneMatch
= "*"` means "only create, never overwrite." The precondition is evaluated
inside `addVersion`'s read-modify-write, which required `Store` to gain an
actual mutex serializing that whole section — otherwise two concurrent
conditional writers could both read the same "current" state and both
pass. `TestConditionalPutIsCompareAndSwapUnderConcurrency` proves it: 20
goroutines racing an `IfMatch` PUT against the same stale ETag, exactly one
wins (checked under `-race`).

### Step 0702 — conditional requests over HTTP, closing Chapter 7
`18f9930` · `conditional_http.go`, `conditional_http_test.go`, `errors_http.go`, `server.go` · 4 files, +189/-2

PUT reads `X-Amz-Meta-*` headers into metadata and `If-Match`/
`If-None-Match` into the conditional-write fields, mapping
`ErrPreconditionFailed` to 412. GET/HEAD gained the read-side conditionals:
`If-None-Match` matching short-circuits to 304, `If-Match` not matching
returns 412, `If-Unmodified-Since` compares against the version's
`ModTime` — all checked via a `Head` call before GET ever opens the blob.

Chapter 7 is done: a client can do the thing every real S3 SDK's "upload
if unchanged" helper depends on — PUT with `If-Match` against a
previously-read ETag, and get a definite 200 or 412 instead of a race that
silently picks a winner.

---

## Chapter 8 — closing

### Step 0801 — examples/basic and final docs, closing the project
`f4b086a` · `DESIGN.md`, `README.md`, `examples/basic/main.go` · 3 files, +239/-28

`examples/basic/main.go` starts a real `Server` on a loopback port and
drives it with plain `net/http` requests through the same arc as the
closing tests: create a bucket, PUT, multipart-upload from out-of-order
parts, list by prefix, read an old version back after an overwrite, and
watch a conditional PUT get rejected with 412 once its `If-Match` ETag has
gone stale. `README.md`/`DESIGN.md` brought up to date with the finished
8-chapter shape and every scope decision made along the way.

This closes the curriculum: 8 chapters, a blob store through conditional
writes, all durable, all tested end to end (several under `-race`) — the
sibling to `db45` and `postgres-in-steps`'s OLTP engines, teaching an HTTP
object store's actual mechanisms instead of a query engine's.
