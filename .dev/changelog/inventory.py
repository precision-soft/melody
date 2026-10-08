#!/usr/bin/env python3
"""The per-entry inventory of a changelog's [Unreleased] block, the control a compression is proved by.

usage:
    inventory.py snapshot <changelog> <snapshot.json>
    inventory.py check <changelog> <snapshot.json> [--map <map>] [--max-chars N] [--allow-new] [--no-markers-expected]
    inventory.py template <changelog> <snapshot.json> [--map <map>]
    inventory.py draft <changelog> [--section S] [--module M]
    inventory.py families <changelog> [--minimum N]
    inventory.py stats <changelog>

`snapshot` records, for every entry, the markers, the UPGRADE.md and SECURITY.md references, the links and the code spans
that name a door — every span but one of one or two letters or digits, punctuation alone or a quoted marker —, keyed
on an id taken from the entry's text with the markers stripped. `check` reads the block again and
requires every entry of the snapshot to be found, by its own id or through the map, in one entry of the same section that
still carries each of its markers, references, links and door spans; it prints the counts it compared and refuses a
snapshot that holds no marker. `template` prints the map lines a hand pass has to fill: every entry of the block that is
not in the snapshot, with the snapshot entries of its section that share its door spans. `draft` prints the starting
text of a condensed entry, never the result. `families` lists the (section, module) groups, `stats` the sizes.

The map is a text file, one directive per line, `#` starting a comment:
    map <after id> <before id> [<before id> ...]   the entry <after id> of the block carries the snapshot entries named;
                                                    a snapshot entry named under two entries is split between them
    drop <before id> <door span>                    the snapshot entry's door span is dropped on purpose, by name
    fold <before id> <marker>                       the entry's marker is written once for it and another marked entry
"""
import hashlib
import json
import re
import sys

sys.dont_write_bytecode = True

SECTIONS = ['Added', 'Changed', 'Deprecated', 'Removed', 'Fixed', 'Security']

MARKERS = ['**Behavioural change**', '**Breaking**', '**Operational note**', 'C→v4']

UNMARKED_CAP = 300

MARKED_CAP = 500

SECURITY_SHORT_FIRST_SENTENCE = 130

CODE_SPAN = re.compile(r'`([^`\n]+)`')

LINK = re.compile(r'\[[^\]\n]*\]\(([^)\s]+)\)')

INERT_SPANS = [
    re.compile(r'^[A-Za-z0-9]{1,2}$'),
    re.compile(r'^[^A-Za-z0-9]*$'),
]

CALL_ARGUMENTS = re.compile(r'\((?!\*)[^()]*\)')

MARKER_RESIDUE = re.compile(r'\s+([;:.,])')

SENTENCE_BOUNDARY = re.compile(r'\. (?=[A-Z`*])')


def block_of(text):
    """Answers the lines of the [Unreleased] block and the line number of its first line; None without a block."""
    lines = text.split('\n')
    start = None
    for index, line in enumerate(lines):
        if line.startswith('## [Unreleased]'):
            start = index
            break
    if start is None:
        return None, 0
    end = len(lines)
    for index in range(start + 1, len(lines)):
        if lines[index].startswith('## ['):
            end = index
            break
    return lines[start + 1:end], start + 2


def entries_of(lines, first_line_number):
    """Answers [section, text, line] for every `- ` entry of the block; a continuation line joins the entry above it."""
    section = '(none)'
    entries = []
    for offset, line in enumerate(lines):
        if line.startswith('### '):
            section = line[4:].strip()
            continue
        if line.startswith('- '):
            entries.append([section, line, first_line_number + offset])
            continue
        if entries and line.strip() and not line.startswith('#'):
            entries[-1][1] += '\n' + line
    return entries


def strip_markers(text):
    for marker in MARKERS:
        bare = marker.strip('*')
        text = text.replace('**' + bare + '**', bare)
        text = text.replace(bare + ': ', '').replace(bare + ' ', '').replace(bare, '')
    return text


def normalized(text):
    """The text an id is taken from: the markers and their bold gone, the punctuation a hoist leaves behind folded, the
    whitespace collapsed, so a marker moved to the head of its entry keeps the entry's id."""
    text = ' '.join(strip_markers(text).split())
    text = MARKER_RESIDUE.sub(r'\1', text)
    return re.sub(r'([;:.,])(?:[;:.,])+', r'\1', text)


def without_code_spans(text):
    return CODE_SPAN.sub(lambda match: ' ' * len(match.group(0)), text)


def markers_of(text):
    """Counts each marker on the text outside its code spans, so a marker quoted as `**Behavioural change**` is a name, not a mark."""
    prose = without_code_spans(text)
    return {marker: prose.count(marker) for marker in MARKERS if 0 < prose.count(marker)}


def module_of(text):
    head = strip_markers(text[2:]).lstrip()
    if ':' not in head[:80]:
        return '(no prefix)'
    return head.split(':', 1)[0].strip()


def is_door(span):
    """Every code span names something the entry is about, a status code and a literal such as `nil` included, except a
    span of one or two letters or digits, punctuation alone and a marker quoted by name."""
    if span in MARKERS:
        return False
    return not any(None != pattern.search(span) for pattern in INERT_SPANS)


def door_key(span):
    """The name a door is still found under once its argument list is dropped: `Foo(ctx)` and `(*T).M()` answer `Foo` and
    `(*T).M`; a span whose name carries no capital, such as `func(alpha.Bus)`, has no other spelling than its own."""
    key = CALL_ARGUMENTS.sub('', span).strip()
    if key == span or not re.search(r'[A-Z]', key):
        return None
    return key


def doors_of(text):
    seen = []
    for span in CODE_SPAN.findall(text):
        if is_door(span) and span not in seen:
            seen.append(span)
    return seen


def door_found(door, target_doors):
    """A door is found in the target spelled as it was, or, for a call form, spelled with its arguments dropped or changed."""
    if door in target_doors:
        return True
    key = door_key(door)
    if None == key:
        return False
    return any(key == candidate or key == door_key(candidate) for candidate in target_doors)


def bold_balanced(text):
    return 0 == text.count('**') % 2


def backticks_even(text):
    return 0 == text.count('`') % 2


def inventory_of(path):
    """Answers the inventory of the block of one changelog: its entries keyed on their ids, in block order."""
    with open(path, encoding='utf-8') as handle:
        text = handle.read()
    lines, first_line_number = block_of(text)
    if None == lines:
        raise SystemExit(f'{path}: no [Unreleased] block')
    body = '\n'.join(lines)
    entries = []
    seen = {}
    for section, entry, line in entries_of(lines, first_line_number):
        digest = hashlib.sha1(normalized(entry).encode('utf-8')).hexdigest()[:12]
        ordinal = seen.get(digest, 0)
        seen[digest] = ordinal + 1
        identifier = digest if 0 == ordinal else f'{digest}-{ordinal}'
        entries.append({
            'id': identifier,
            'line': line,
            'section': section,
            'module': module_of(entry),
            'chars': len(entry),
            'markers': markers_of(entry),
            'upgrade': entry.count('UPGRADE.md'),
            'security': entry.count('SECURITY.md'),
            'links': LINK.findall(entry),
            'doors': doors_of(entry),
            'backticks_even': backticks_even(entry),
            'bold_balanced': bold_balanced(entry),
            'head': entry[:120],
        })
    return {
        'path': path,
        'chars': len(body),
        'entries': entries,
        'totals': {
            'entries': len(entries),
            'markers': {marker: without_code_spans(body).count(marker) for marker in MARKERS},
            'upgrade': body.count('UPGRADE.md'),
            'security': body.count('SECURITY.md'),
            'links': len(LINK.findall(body)),
        },
    }


def read_map(path):
    """Answers {after id: [before ids]}, {before id: {dropped spans}} and {before id: {folded markers}} read from a map file."""
    mapping = {}
    drops = {}
    folds = {}
    if None == path:
        return mapping, drops, folds
    with open(path, encoding='utf-8') as handle:
        for number, raw in enumerate(handle, 1):
            line = raw.strip()
            if line.startswith('drop ') and '`' in line:
                closing = line.rfind('`')
                line = line[:closing + 1] if line.find('`') < closing else line.split('#', 1)[0].strip()
            else:
                line = line.split('#', 1)[0].strip()
            if '' == line:
                continue
            words = line.split(None, 2)
            if 'map' == words[0] and 3 <= len(words):
                mapping.setdefault(words[1], []).extend(words[2].split())
            elif 'drop' == words[0] and 3 <= len(words):
                span = words[2].strip()
                if span.startswith('`') and span.endswith('`') and 2 < len(span):
                    span = span[1:-1]
                drops.setdefault(words[1], set()).add(span)
            elif 'fold' == words[0] and 3 <= len(words) and words[2].strip() in MARKERS:
                folds.setdefault(words[1], set()).add(words[2].strip())
            else:
                raise SystemExit(f'{path}:{number}: not a directive: {raw.rstrip()}')
    return mapping, drops, folds


def check(changelog, snapshot_path, map_path, max_chars, allow_new, no_markers_expected):
    with open(snapshot_path, encoding='utf-8') as handle:
        before = json.load(handle)
    after = inventory_of(changelog)
    mapping, drops, folds = read_map(map_path)
    marker_total = sum(before['totals']['markers'].values())
    if 0 == marker_total and not no_markers_expected:
        print(f'REFUSED: the snapshot {snapshot_path} holds no marker to count; a check on it proves nothing about markers')
        return 2
    after_by_id = {entry['id']: entry for entry in after['entries']}
    before_by_id = {entry['id']: entry for entry in before['entries']}
    targets_of = {}
    failures = []
    for after_id, before_ids in mapping.items():
        if after_id not in after_by_id:
            failures.append(f'STALE MAP: {after_id} is no entry of the block (mapped from {" ".join(before_ids)})')
            continue
        for before_id in before_ids:
            if before_id not in before_by_id:
                failures.append(f'STALE MAP: {before_id} is no entry of the snapshot (mapped to {after_id})')
            elif after_id not in targets_of.setdefault(before_id, []):
                targets_of[before_id].append(after_id)
    for before_id in before_by_id:
        if before_id not in targets_of and before_id in after_by_id:
            targets_of[before_id] = [before_id]
    for before_id in list(drops) + list(folds):
        if before_id not in before_by_id:
            failures.append(f'STALE DROP: {before_id} is no entry of the snapshot')
    compared = {'entries': 0, 'markers': 0, 'upgrade': 0, 'security': 0, 'links': 0, 'doors': 0, 'dropped': 0, 'folded': 0}
    map_only = []
    marked_members = {}
    for entry in before['entries']:
        identifier = entry['id']
        if identifier not in targets_of:
            failures.append(f'LOST ENTRY: {identifier} {entry["section"]} :{entry["line"]} {entry["head"]!r}')
            continue
        targets = [after_by_id[target_id] for target_id in targets_of[identifier]]
        compared['entries'] += 1
        where = f'{identifier} → {" + ".join(target["id"] for target in targets)} :{" + :".join(str(target["line"]) for target in targets)}'
        if 1 < len(targets):
            print(f'SPLIT: {where}')
        if not (entry['doors'] or entry['markers'] or entry['links'] or entry['upgrade'] or entry['security']):
            if [identifier] != targets_of[identifier]:
                map_only.append(entry)
        for target in targets:
            if target['section'] != entry['section']:
                failures.append(f'SECTION MOVED: {identifier} → {target["id"]} :{target["line"]} {entry["section"]} → {target["section"]}')
        for marker in entry['markers']:
            compared['markers'] += 1
            if not any(marker in target['markers'] for target in targets):
                failures.append(f'LOST MARKER: {where} {marker} {entry["head"]!r}')
            elif marker in folds.get(identifier, set()):
                compared['folded'] += 1
            else:
                for target in targets:
                    marked_members.setdefault((target['id'], marker), []).append(identifier)
        for kind in ['upgrade', 'security']:
            if 0 < entry[kind]:
                compared[kind] += 1
                if all(0 == target[kind] for target in targets):
                    failures.append(f'LOST REFERENCE: {where} {"UPGRADE.md" if "upgrade" == kind else "SECURITY.md"}')
        for link in entry['links']:
            compared['links'] += 1
            if not any(link in target['links'] for target in targets):
                failures.append(f'LOST LINK: {where} {link}')
        dropped = drops.get(identifier, set())
        target_doors = [door for target in targets for door in target['doors']]
        for door in entry['doors']:
            if door in dropped:
                compared['dropped'] += 1
                continue
            compared['doors'] += 1
            if not door_found(door, target_doors):
                failures.append(f'LOST DOOR: {where} `{door}`')
        for span in dropped:
            if span not in entry['doors']:
                failures.append(f'STALE DROP: {identifier} `{span}` is no door span of the snapshot entry')
    for (target_id, marker), members in marked_members.items():
        present = after_by_id[target_id]['markers'].get(marker, 0)
        if 1 < len(members) and present < len(members):
            failures.append(f'FOLDED MARKER: {target_id} carries {marker} {present} time(s) for {len(members)} marked entries '
                            f'({" ".join(members)}); keep one per entry or declare `fold <before id> {marker}`')
    for target in after['entries']:
        if not target['backticks_even']:
            failures.append(f'ODD BACKTICKS: {target["id"]} :{target["line"]}')
        if not target['bold_balanced']:
            failures.append(f'UNBALANCED BOLD: {target["id"]} :{target["line"]}')
    carried = {target_id for target_ids in targets_of.values() for target_id in target_ids}
    new = [entry for entry in after['entries'] if entry['id'] not in carried]
    for entry in new:
        line = f'NEW ENTRY: {entry["id"]} {entry["section"]} :{entry["line"]} {entry["head"]!r}'
        if allow_new:
            print(line)
        else:
            failures.append(line)
    print(f'block {changelog}: {before["totals"]["entries"]} entries in the snapshot, {after["totals"]["entries"]} in the block, '
          f'{before["chars"]} → {after["chars"]} chars')
    print('compared: ' + ', '.join(f'{key} {value}' for key, value in compared.items()))
    if map_only:
        print(f'carried on the map alone: {len(map_only)} entr(ies) with no door, marker, link or reference; re-read each against its target')
        for entry in map_only:
            print(f'  {entry["id"]} {entry["section"]} :{entry["line"]} {entry["head"]!r}')
    print('markers in the snapshot: ' + ', '.join(f'{marker} {count}' for marker, count in before['totals']['markers'].items()))
    print('markers in the block:    ' + ', '.join(f'{marker} {count}' for marker, count in after['totals']['markers'].items()))
    if None != max_chars and after['chars'] > max_chars:
        failures.append(f'TOO LONG: {after["chars"]} chars against {max_chars}')
    for failure in failures:
        print(failure)
    if failures:
        print(f'FAILED: {len(failures)} finding(s)')
        return 1
    print('OK')
    return 0


def template(changelog, snapshot_path, map_path):
    with open(snapshot_path, encoding='utf-8') as handle:
        before = json.load(handle)
    after = inventory_of(changelog)
    mapping, _, _ = read_map(map_path)
    before_ids = {entry['id'] for entry in before['entries']}
    mapped_before = {before_id for before_ids_of in mapping.values() for before_id in before_ids_of}
    carried_after = set(mapping) | {entry['id'] for entry in after['entries'] if entry['id'] in before_ids}
    orphans = [entry for entry in before['entries'] if entry['id'] not in mapped_before and entry['id'] not in carried_after]
    for entry in after['entries']:
        if entry['id'] in carried_after:
            continue
        doors = set(entry['doors'])
        candidates = []
        for orphan in orphans:
            if orphan['section'] != entry['section']:
                continue
            shared = len(doors & set(orphan['doors']))
            if 0 < shared or orphan['module'] == entry['module']:
                candidates.append((shared, orphan))
        candidates.sort(key=lambda item: -item[0])
        print(f'# :{entry["line"]} {entry["section"]} {entry["head"]!r}')
        print(f'map {entry["id"]} ' + ' '.join(orphan['id'] for shared, orphan in candidates if 0 < shared))
        for shared, orphan in candidates[:12]:
            bare = '' if (orphan['doors'] or orphan['markers'] or orphan['links'] or orphan['upgrade'] or orphan['security']) else ' [nothing to compare: re-read]'
            print(f'#   {orphan["id"]} shares {shared} :{orphan["line"]} {orphan["head"][:90]!r}{bare}')
    return 0


def spans_of(text):
    return [match.span() for match in CODE_SPAN.finditer(text)]


def sentences_of(text):
    """Splits on `. ` before a capital, a backtick or an asterisk, outside a code span and outside a pair of em-dashes."""
    spans = spans_of(text)
    pieces = []
    start = 0
    for match in SENTENCE_BOUNDARY.finditer(text):
        position = match.start()
        if any(low <= position < high for low, high in spans):
            continue
        if 1 == text[start:position].count('—') % 2:
            continue
        pieces.append(text[start:position + 1])
        start = position + 2
    pieces.append(text[start:])
    return pieces


def hoisted(text):
    """Moves the first marker that is not at the head of the entry to the head, after the module prefix."""
    for marker in MARKERS[:3]:
        if marker not in text:
            continue
        head = text[2:]
        prefix = head.split(':', 1)[0] + ': ' if ':' in head[:80] else ''
        rest = head[len(prefix):]
        if rest.startswith(marker):
            return text
        rest = rest.replace(marker + ': ', '', 1) if marker + ': ' in rest else rest.replace(marker + ' ', '', 1).replace(marker, '', 1)
        return '- ' + prefix + marker + ': ' + rest
    return text


def draft_of(section, text):
    """Answers the starting text of a condensed entry and why it kept what it kept; the hand pass decides the result."""
    entry = hoisted(text.replace('\n', ' ').strip())
    marked = any(marker in entry for marker in MARKERS[:3]) or 'Security' == section
    cap = MARKED_CAP if marked else UNMARKED_CAP
    sentences = sentences_of(entry)
    kept = [sentences[0]]
    reasons = ['first']
    for index, sentence in enumerate(sentences[1:], 1):
        if 'UPGRADE.md' in sentence or 'SECURITY.md' in sentence or None != LINK.search(sentence):
            kept.append(sentence)
            reasons.append(f'{index}:reference')
        elif any(marker in sentence for marker in MARKERS):
            kept.append(sentence)
            reasons.append(f'{index}:marker')
        elif 'Security' == section and 1 == index and len(sentences[0].encode('utf-8')) < SECURITY_SHORT_FIRST_SENTENCE:
            kept.append(sentence)
            reasons.append(f'{index}:attack')
        elif len(' '.join(kept + [sentence])) <= cap:
            kept.append(sentence)
            reasons.append(f'{index}:cap')
    result = ' '.join(kept)
    refused = backticks_even(entry) != backticks_even(result) or bold_balanced(entry) != bold_balanced(result)
    if refused:
        return entry, ['REFUSED: the cut changes the backtick parity or the bold balance'], len(entry)
    return result, reasons, len(entry)


def draft(changelog, section_filter, module_filter):
    inventory = inventory_of(changelog)
    with open(changelog, encoding='utf-8') as handle:
        lines, first_line_number = block_of(handle.read())
    for section, text, line in entries_of(lines, first_line_number):
        if None != section_filter and section != section_filter:
            continue
        if None != module_filter and module_of(text) != module_filter:
            continue
        identifier = next(entry['id'] for entry in inventory['entries'] if entry['line'] == line)
        result, reasons, before_chars = draft_of(section, text)
        print(f'# {identifier} :{line} {section} {before_chars} → {len(result)} chars, kept {" ".join(reasons)}')
        print(result)
        print()
    return 0


def families(changelog, minimum):
    inventory = inventory_of(changelog)
    groups = {}
    for entry in inventory['entries']:
        key = (entry['section'], entry['module'])
        count, chars = groups.get(key, (0, 0))
        groups[key] = (count + 1, chars + entry['chars'])
    ranked = sorted(groups.items(), key=lambda item: (SECTIONS.index(item[0][0]) if item[0][0] in SECTIONS else 99, -item[1][1]))
    print(f'{len(groups)} families, {sum(1 for _, (count, _) in ranked if count >= minimum)} with at least {minimum} members')
    for (section, module), (count, chars) in ranked:
        flag = '*' if count >= minimum else ' '
        print(f'{flag} {section}\t{module}\t{count}\t{chars}')
    return 0


def stats(changelog):
    inventory = inventory_of(changelog)
    print(f'{changelog}: {inventory["chars"]} chars, {inventory["totals"]["entries"]} entries')
    print('markers: ' + ', '.join(f'{marker} {count}' for marker, count in inventory['totals']['markers'].items()))
    print(f'UPGRADE.md {inventory["totals"]["upgrade"]}, SECURITY.md {inventory["totals"]["security"]}, links {inventory["totals"]["links"]}')
    sections = {}
    for entry in inventory['entries']:
        count, chars = sections.get(entry['section'], (0, 0))
        sections[entry['section']] = (count + 1, chars + entry['chars'])
    for section, (count, chars) in sections.items():
        print(f'{section}\t{count}\t{chars}')
    unknown = [section for section in sections if section not in SECTIONS]
    if unknown:
        print(f'UNKNOWN SECTIONS: {unknown}')
    return 0


def option(arguments, name, default=None):
    if name not in arguments:
        return default
    index = arguments.index(name)
    value = arguments[index + 1]
    del arguments[index:index + 2]
    return value


def flag(arguments, name):
    if name not in arguments:
        return False
    arguments.remove(name)
    return True


def main(arguments):
    if 2 > len(arguments):
        print(__doc__)
        return 2
    command = arguments.pop(0)
    if 'snapshot' == command and 2 == len(arguments):
        inventory = inventory_of(arguments[0])
        if 0 == sum(inventory['totals']['markers'].values()):
            print(f'NOTE: {arguments[0]} holds no marker to count')
        with open(arguments[1], 'w', encoding='utf-8') as handle:
            json.dump(inventory, handle, ensure_ascii=False, indent=1)
        print(f'{arguments[1]}: {inventory["totals"]["entries"]} entries, {inventory["chars"]} chars, markers '
              + ', '.join(f'{marker} {count}' for marker, count in inventory['totals']['markers'].items()))
        return 0
    if 'check' == command:
        map_path = option(arguments, '--map')
        max_chars = option(arguments, '--max-chars')
        allow_new = flag(arguments, '--allow-new')
        no_markers_expected = flag(arguments, '--no-markers-expected')
        if 2 == len(arguments):
            return check(arguments[0], arguments[1], map_path, None if None == max_chars else int(max_chars), allow_new, no_markers_expected)
    if 'template' == command:
        map_path = option(arguments, '--map')
        if 2 == len(arguments):
            return template(arguments[0], arguments[1], map_path)
    if 'draft' == command:
        section_filter = option(arguments, '--section')
        module_filter = option(arguments, '--module')
        if 1 == len(arguments):
            return draft(arguments[0], section_filter, module_filter)
    if 'families' == command:
        minimum = int(option(arguments, '--minimum', '3'))
        if 1 == len(arguments):
            return families(arguments[0], minimum)
    if 'stats' == command and 1 == len(arguments):
        return stats(arguments[0])
    print(__doc__)
    return 2


if '__main__' == __name__:
    sys.exit(main(sys.argv[1:]))
