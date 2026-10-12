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
code or a lowercase key is required as much as an exported identifier. A marker is counted outside the code spans only
and only where the blocks write one, at the head of the entry after its module prefix or after a sentence or clause
boundary, in either place behind a run of other markers of any length, each followed by `: `, and `C→v4` bare or bold;
"the **Breaking** marker" named in prose is no mark, and neither is a marker behind another bold span or behind a
sub-heading such as `- cli: the flags: **Breaking**: …`, so a condensed entry writes its markers right after the module
prefix, before any sub-heading; and the id is taken with the markers, their bold and the punctuation a hoist leaves behind removed, so hoisting a marker
to the head of its entry keeps the entry's id. The snapshot is taken once, before the first condensing edit, and every
later check runs against it, never against the output of the previous edit, so a loss made early is still reported at
the end.

`check` reads the block again and finds each snapshot entry by its own id, or through the map when its text changed, in
the entries of the same section the map names for it, which together still carry each of its markers, references, links
and door spans. Each span of the targets answers one door, so two doors are not satisfied by one span; a door is found
spelled as it was first, then under its name: a call written with its arguments dropped or changed, `Foo(ctx)` as `Foo`,
or a bare name written as a call, `Foo` as `Foo(ctx)`, each such match listed under `respelled` for the hand pass. A span
whose parentheses follow a space, `TEARDOWN (SEQUENTIAL)`, or whose arguments quote a literal, `Require("admin")`, has
no other spelling than its own. It reports a lost entry, a lost marker, reference, link or door span, an entry moved to
another section or under the prefix of another module (the modules of a prefix are read as a set, so `logging, cli` and `cli, logging` are one family; a prefix is one or two lowercase words, a word carrying letters, digits, `/`, `.`, `-` or `_` as `http/static` does, or several such names joined by commas, before `: `, so the prose of an integration block's entry, which writes no prefix, is no module even where a colon follows it, and a condensed entry there avoids a one- or two-word lead-in before a colon, which would read as one), an entry mapped into an entry the block still carries word for word, a split nobody declared or one
whose half carries nothing of the entry, two marked entries carried by one entry that writes their marker once, a marker
an entry carries more times than the snapshot entries it holds, a `**Behavioural change**`, `**Breaking**` or `**Operational note**` a rewritten entry writes past its head rather than after its module prefix (`C→v4` is written as the trailing sentence naming the v4 form and is not held to the head), a marker an entry no longer writes at its head where the snapshot entry did (the id strips the markers, so the move keeps the id; the snapshot stores each entry's head run, an older snapshot's being read again off its stored head), a marker an entry merged into one that kept its own id writes past that entry's head (a second net: the unchanged-entry refusal fires first on such a merge, and it also refuses an entry whose facts the kept one already states word for word, which has to keep a bullet of its own), a snapshot entry carried in an entry written under a `**Behavioural change**`, `**Breaking**` or `**Operational note**` it did not carry, which the marker would classify with it, a door of an `Added` entry found only under its name and not as written (the signature an `Added` entry announces is kept whole), an odd backtick count, an unbalanced bold, a map, drop,
fold or split line that names no entry or nothing it applies to and an entry the snapshot does not hold, prints the
counts it compared and exits non-zero on any finding. It lists, without failing, every mapped entry left with nothing to
compare once its drops are applied — no door, no marker, no link, no reference — so the hand pass re-reads exactly
those against their target. It refuses a snapshot that
holds no marker, since a check that counted none proves nothing about markers; `--no-markers-expected` admits a block
that never carried one.

The map holds six directives, one per line, `#` starting a comment, after the closing backtick on a `drop` line: `map
<after id> <before id>...` says that an entry of the block carries the snapshot entries named; `split <before id>` says
that a snapshot entry named under several entries is split between them, checked against their union, each half
carrying at least one of its doors, markers, links or references, and the `SPLIT` line printing which half carries which
door; `drop <before id> <door span>` drops one door span of a snapshot entry by name, the span ending at its first
closing backtick; `fold <before id> <marker>` lets the marker of a snapshot entry merged with another marked one be
written once; `widen <before id> <marker>` declares a snapshot entry without that classifying marker carried in an
entry written under it, the marker's scope widened on purpose, and is reported as stale where it applies to nothing;
`refer <after id> <before id>...` says that the unmarked snapshot entries named are carried by the pointer entry `<after id>`
alone — the release-body form of a `### Fixed` section, where the marked entries keep their bullets and the unmarked ones
of one module become one bullet naming their count and the permalink of this file at the commit that still holds their
text (`https://github.com/precision-soft/melody/blob/<sha>/<path>`), so a reader of the body finds the number and the
place rather than nothing. A referred entry's doors are released, not required, and their number is printed as
`released`; what the directive refuses, each by name: an entry that carries a marker (`REFERRED MARKED ENTRY` — a marker
classifies, and a classification is kept in the body), an entry that names `UPGRADE.md`, `SECURITY.md` or a link
(`REFERRED ENTRY WITH A REFERENCE` — a documented remedy is kept in the body), an entry mapped or kept as well
(`AMBIGUOUS CARRIER`), a pointer in another section or under a module its entries do not name (`SECTION MOVED`, `MOVED
ACROSS MODULES` — a pointer's module is one of the modules a compound prefix names, so `container, debug` is referred to
the `container` pointer), a pointer without the permalink (`REFERRER WITHOUT PERMALINK`), a pointer whose first number,
in digits or in words up to nine hundred and ninety-nine, is not the count of the entries referred to it (`REFERRER
COUNT`), a pointer that carries a marker (`REFERRER MARKED`), a pointer that is also mapped from or kept as a snapshot
entry (`REFERRER CARRIES TEXT`), a pointer written in any form but `- <module>: <count> further fixes, their entries whole in this
file at commit `<sha>`, <permalink>.` — `one further fix, its entry whole` for a single entry — (`REFERRER WITH EXTRA TEXT` — a count
and a permalink that are right would otherwise cover any claim written beside them), a pointer whose permalink names another file
than the checked one or another commit than the sha the pointer writes in backticks (`REFERRER PERMALINK` — the path is the
file's own repository path, or the one `--permalink-path` gives for a copy outside the tree), two pointers of one section under one
module (`REFERRER TWICE`), the same mismatch on the prose head line a condensed block opens with (`HEAD LINE PERMALINK`), and a `drop`
on a referred entry (`STALE DROP`), since it applies to nothing. The K6 construction referred a compound prefix to the pointer of its
FIRST module; the check admits any of its modules, the hand pass being the one that chooses.
Every occurrence of a marker a snapshot entry carries, a second one of one kind included, needs an occurrence of
its own in the entries carrying it; the occurrences are assigned by a complete matching, so the order of the target
entries decides nothing, and an occurrence left over is a lost marker when it belongs to one entry and a folded one when
several marked entries share the targets.
Moving an entry to another section has no directive, since a compression never does it. `template` prints a `map` line
for every entry of the block the snapshot does not hold, with the snapshot entries of its section that share its door
spans, for the hand pass to confirm, and a `refer` line, with the unmarked snapshot entries of its section and module, for
an entry that carries a permalink.

`draft` prints, for every entry or the ones of `--section` and `--module`, the starting text of its condensed form: the
first sentence, every sentence that carries a marker, a document reference or a link, the second sentence of a security
entry whose first runs under 130 bytes, the marker moved to the head, and the rest up to 300 characters, 500 for a marked
or security entry; a cut that changes the backtick parity or the bold balance is refused and the entry printed whole. The
hand pass decides the result. `families` lists the entries by section and module, `stats` the sizes and the markers.
