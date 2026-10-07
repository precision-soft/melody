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
