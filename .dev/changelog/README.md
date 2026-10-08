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
references, its links, its door spans, its backtick parity and its bold balance. Every code span is a door except a span
of one or two letters or digits, punctuation alone and a marker quoted by name, so a command, a flag, a path, a status
code or a lowercase key is required as much as an exported identifier. A marker is counted outside the code spans only,
and the id is taken with the markers, their bold and the punctuation a hoist leaves behind removed, so hoisting a marker
to the head of its entry keeps the entry's id. The snapshot is taken once, before the first condensing edit, and every
later check runs against it, never against the output of the previous edit, so a loss made early is still reported at
the end.

`check` reads the block again and finds each snapshot entry by its own id, or through the map when its text changed, in
the entries of the same section the map names for it, which together still carry each of its markers, references, links
and door spans; a door written as a call is still found once its arguments are dropped or changed, `Foo(ctx)` under
`Foo`. It reports a lost entry, a lost marker, reference, link or door span, an entry moved to another section, two
marked entries carried by one entry that writes their marker once, an odd backtick count, an unbalanced bold, a map,
drop or fold line that names no entry and an entry the snapshot does not hold, prints the counts it compared and exits
non-zero on any finding. It lists, without failing, every mapped entry that carries nothing it can compare — no door, no
marker, no link, no reference — so the hand pass re-reads exactly those against their target. It refuses a snapshot that
holds no marker, since a check that counted none proves nothing about markers; `--no-markers-expected` admits a block
that never carried one.

The map holds three directives, one per line, `#` starting a comment, after the closing backtick on a `drop` line: `map
<after id> <before id>...` says that an entry of the block carries the snapshot entries named, and a snapshot entry
named under two entries is split between them; `drop <before id> <door span>` drops one door span of a snapshot entry by
name; `fold <before id> <marker>` lets the marker of a snapshot entry merged with another marked one be written once.
Moving an entry to another section has no directive, since a compression never does it. `template` prints a `map` line
for every entry of the block the snapshot does not hold, with the snapshot entries of its section that share its door
spans, for the hand pass to confirm.

`draft` prints, for every entry or the ones of `--section` and `--module`, the starting text of its condensed form: the
first sentence, every sentence that carries a marker, a document reference or a link, the second sentence of a security
entry whose first runs under 130 bytes, the marker moved to the head, and the rest up to 300 characters, 500 for a marked
or security entry; a cut that changes the backtick parity or the bold balance is refused and the entry printed whole. The
hand pass decides the result. `families` lists the entries by section and module, `stats` the sizes and the markers.
