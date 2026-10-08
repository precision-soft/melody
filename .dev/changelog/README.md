# changelog

Proves a compression of a changelog's `[Unreleased]` block entry by entry: what every entry carried before is found after,
in the entry it was condensed or merged into.

```bash
python3 .dev/changelog/inventory.py snapshot v3/CHANGELOG.md .temp/compression/before-v3.json
python3 .dev/changelog/inventory.py template v3/CHANGELOG.md .temp/compression/before-v3.json --map .temp/compression/v3.map
python3 .dev/changelog/inventory.py check v3/CHANGELOG.md .temp/compression/before-v3.json --map .temp/compression/v3.map --max-chars 115000
```

`snapshot` records every entry of the block under an id taken from its text with the markers stripped: its section, its
`**Behavioural change**`, `**Breaking**`, `**Operational note**` and `C→v4` markers, its `UPGRADE.md` and `SECURITY.md`
references, its links, the code spans that name a door (an exported identifier, an environment name, a dotted
configuration key, a quoted message), its backtick parity and its bold balance. The snapshot is taken once, before the
first condensing edit, and every later check runs against it, never against the output of the previous edit, so a loss
made early is still reported at the end.

`check` reads the block again and finds each snapshot entry by its own id, or through the map when its text changed, in
one entry of the same section that still carries each of its markers, references, links and door spans. It reports a
lost entry, a lost marker, reference, link or door span, an entry moved to another section, an odd backtick count, an
unbalanced bold, a map line that names no entry and an entry the snapshot does not hold, prints the counts it compared and
exits non-zero on any finding. It refuses a snapshot that holds no marker, since a check that counted none proves nothing
about markers; `--no-markers-expected` admits a block that never carried one.

The map holds two directives, one per line: `map <after id> <before id>...` says that an entry of the block carries the
snapshot entries named, and `drop <before id> <door span>` drops one door span of a snapshot entry by name. `template`
prints a `map` line for every entry of the block the snapshot does not hold, with the snapshot entries of its section that
share its door spans, for the hand pass to confirm.

`draft` prints, for every entry or the ones of `--section` and `--module`, the starting text of its condensed form: the
first sentence, every sentence that carries a marker, a document reference or a link, the second sentence of a security
entry whose first runs under 130 bytes, the marker moved to the head, and the rest up to 300 characters, 500 for a marked
or security entry; a cut that changes the backtick parity or the bold balance is refused and the entry printed whole. The
hand pass decides the result. `families` lists the entries by section and module, `stats` the sizes and the markers.
