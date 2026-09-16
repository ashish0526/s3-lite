# Design notes

Scope decisions and simplifications made along the way, recorded here
rather than scattered only in commit messages, for anyone comparing this
build against real S3 internals.

## Chapter 1 — filesystem key encoding

- **A key segment spelled like the traversal escape can collide.**
  `EncodeKey` remaps a literal `.`/`..` segment to `=2e`/`=2e2e` (hex of the
  ASCII bytes) before falling through to `url.PathEscape` for everything
  else, since `PathEscape` leaves `.`/`..` untouched and a literal `..`
  directory component would walk out of the bucket's data directory. A key
  segment that happens to already be spelled `=2e` collides with the
  escaped form of `.`. Real S3 has no such issue because it never maps keys
  onto a filesystem namespace at all — keys are opaque strings in a
  key-value index. Fixing this for real means picking an escape prefix that
  cannot appear in `url.PathEscape`'s output, or hashing every segment
  instead of preserving readable paths — the latter would also remove the
  (cosmetic, `/`-collapsing) directory-like structure this build keeps on
  purpose for readability when poking around the data directory by hand.

## Chapter 4 — multipart upload

- **Abort/cleanup uses `os.RemoveAll`, not the atomic-write primitives.**
  An in-progress upload's scratch directory has no durability contract of
  its own — if the process crashes mid-upload, the worst case is an orphaned
  scratch directory, not a corrupted object, since nothing under
  `uploads/<id>/` is ever linked into a key's version history until
  `CompleteMultipartUpload` succeeds. This build never garbage-collects
  those orphans; real S3 expires incomplete multipart uploads via a
  lifecycle rule, which this build doesn't implement at all (no lifecycle
  rules of any kind).
- **Part ETags are SHA-256, not MD5**, for consistency with every other
  hash in this build (Chapter 2's whole-object ETag, the composite
  multipart ETag below). Real S3 uses MD5 throughout. This changes nothing
  about the mechanism being taught — content hashing as an integrity
  check — only the algorithm.

## Chapter 6 — the versioned storage model

- **`DeleteVersion` never garbage-collects the blob it pointed at.**
  Blobs are content-addressed and shared: another version of the same key,
  or a completely different key, might reference the exact same bytes, and
  this build keeps no reference count to know when a blob is safe to
  reclaim. Real S3 doesn't expose this problem to a caller at all (it's
  handled internally); a real fix here is a reference-counted or
  mark-and-sweep GC pass over the blob store, which chapters 1-8 never
  needed until content addressing made blobs shared in the first place.
- **No non-versioned bucket mode.** Every bucket in this build behaves like
  a real S3 bucket with versioning permanently enabled — `Put` always adds
  a version, `Delete` always adds a marker. Real S3 buckets default to
  versioning *disabled* (a `Put` there really does overwrite, and `Delete`
  really does remove). Implementing that mode faithfully would mean two
  different code paths through `addVersion`/`resolveVersion`, doubling the
  chapter's surface for a mode this build's teaching goal (show how
  versioning actually works) doesn't need.
- **List-object-versions is not implemented.** Chapter 5's `ListObjectsV2`
  already demonstrates the pagination/prefix/delimiter mechanics on the
  "latest, live version only" view; a version-aware listing would mostly
  repeat that same machinery over a second axis (per-key history) without
  teaching a new mechanism, so it was cut rather than built for
  completeness's sake (Step 0602's commit message has the same note).

## Chapter 7 — conditional requests

- **The metadata-write lock is store-wide, not per-key.** `addVersion` and
  `DeleteVersion` both take the same `Store.mu` for their entire
  read-modify-write, which is what makes a conditional PUT's precondition
  check a real compare-and-swap instead of a check that can go stale
  before the write lands (proven under `-race` by
  `TestConditionalPutIsCompareAndSwapUnderConcurrency`). The cost is
  throughput, not correctness: a write to key A blocks a concurrent write
  to unrelated key B, which a real system avoids with a per-key lock (a
  mutex map, or sharding by key hash) — a mechanical change that wouldn't
  alter the correctness argument at all. This is the direct parallel to
  postgres-in-steps' MVCC write-conflict check (an `UpdateRow` there proves
  it saw the row version it thinks it's replacing, the same way `IfMatch`
  proves it saw the ETag it thinks is current) — that build's conflict
  check is scoped to one row via the buffer pool's per-page locking, which
  is the finer granularity this build's store-wide mutex deliberately
  skips.
- **If-Match/If-None-Match only compare a single ETag value.** Real S3 (and
  the HTTP spec) allows a comma-separated list in either header — "succeed
  if the current ETag matches any of these." This build's
  `checkReadConditions`/`PutOptions` only ever compare one value, which
  covers the actual use case both headers exist for (optimistic
  concurrency and cache validation against one known-previous ETag) without
  needing a list-parsing/matching pass that no test in this build would
  ever exercise differently.
