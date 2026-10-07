# copy

Builds the fourth major from the third: every tracked file of `v3/` and of the eleven third-major integration modules,
read at a revision through git, written under an output root with the module paths, the example database names, the
`go.mod` versions and replace paths, the markdown links and the build version rewritten, and each `CHANGELOG.md` replaced
by the one entry that names the revision it was copied from.

```bash
python3 .dev/copy/copy.py <revision> .temp/copy/out .temp/copy/manifest.tsv
python3 .dev/copy/compare.py <revision> .temp/copy/out .temp/copy/manifest.tsv
.dev/copy/testlist.sh
```

`copy.py` prints the totals of every rewrite and writes the manifest, one row per file with its rewrites. `compare.py`
reads the manifest and prints `UNEXPLAINED` for every changed line that is not equal under the inverse rewrite; the
`go.mod` lines whose module version moves to `v4.0.0` are expected there, and `CHANGELOG.md`, `go.sum` and
`version.go` are reported apart. `testlist.sh` runs inside the dev container once the copy is in place under `v4/` and
`integrations/*/v4`, and prints the test count of each third-major module beside its copy and the size of their
difference.

`copy.py` refuses an output root that is not empty, prints every rewrite count including the zero ones, and exits non-zero
when the build version or the changelogs were not rewritten exactly once each. `compare.py` exits non-zero when a changelog
is not the fresh entry, a `go.sum` is not its source without the melody lines, or `version.go` does not read `v4.0.0`, and
lists as `RESIDUAL` every line of the copy that still spells `v3` outside a version number and a third-party module path:
prose that the preparation of the fourth major rewrites, or a path no rewrite covers. A path inside a code span or a fenced
block of a document is re-spelled when it names a third-major module root, and is counted as `code_path`. `testlist.sh`
prints `BUILD FAILED` with the transcript for a module that does not compile, rather than a count of zero.

## Before testing

The copy covers the module roots alone. Before `testlist.sh` can compile the fourth major, the files that enumerate the
majors by hand need it too: `go.work` uses `./v4`, `./v4/.example`, the eleven `./integrations/**/v4` and
`./integrations/cron/v4/.example`, since the copied requirements pin `v4.0.0`, which no tag carries and only the workspace
resolves; the copied modules may need `go mod tidy` for the `go.sum` lines the copy drops; `.dev/docker/mysql/init.sql` and
`.dev/docker/postgres/init.sql` create `melody_example_v4` for the example; and `.dev/validate/all.sh`,
`.github/workflows/ci.yml`, `.dev/validate/citation.sh` and `.dev/validate/apidiff.baseline` name the majors their lanes read.
