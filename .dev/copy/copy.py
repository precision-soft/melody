#!/usr/bin/env python3
"""Copy B: builds the fourth major from the third, into an output root, with a manifest of every rewrite."""
import os
import posixpath
import re
import subprocess
import sys

REPOSITORY = subprocess.run(['git', '-C', os.path.dirname(os.path.abspath(__file__)), 'rev-parse', '--show-toplevel'], check=True, capture_output=True, text=True).stdout.strip()
if '__main__' == __name__:
    SOURCE_REVISION = sys.argv[1]
    OUTPUT_ROOT = sys.argv[2]
    MANIFEST = sys.argv[3]

MODULE_ROOTS = [
    ('v3', 'v4'),
    ('integrations/amqp/v3', 'integrations/amqp/v4'),
    ('integrations/awss3/v3', 'integrations/aws/s3/v4'),
    ('integrations/bunorm/v3', 'integrations/bunorm/v4'),
    ('integrations/bunorm/migrate/v3', 'integrations/bunorm/migrate/v4'),
    ('integrations/bunorm/mysql/v3', 'integrations/bunorm/mysql/v4'),
    ('integrations/bunorm/pgsql/v3', 'integrations/bunorm/pgsql/v4'),
    ('integrations/cron/v3', 'integrations/cron/v4'),
    ('integrations/opentelemetry/v3', 'integrations/opentelemetry/v4'),
    ('integrations/outbox/v3', 'integrations/outbox/v4'),
    ('integrations/rueidis/v3', 'integrations/rueidis/v4'),
    ('integrations/websocket/v3', 'integrations/websocket/v4'),
]

MODULE_PATH_PREFIX = 'precision-soft/melody/'


def map_path(path):
    """Maps a repository path under a third-major module root to its fourth-major path; None outside every root."""
    best = None
    for old, new in MODULE_ROOTS:
        if path == old or path.startswith(old + '/'):
            if best is None or len(old) > len(best[0]):
                best = (old, new)
    if best is None:
        return None
    return best[1] + path[len(best[0]):]


def map_module_path(match):
    rest = match.group(1)
    mapped = map_path(rest)
    if mapped is None:
        return match.group(0)
    return MODULE_PATH_PREFIX + mapped


MODULE_PATH_PATTERN = re.compile(
    re.escape(MODULE_PATH_PREFIX) + r'((?:integrations/[a-z0-9]+(?:/[a-z0-9]+)?/)?v3)(?![0-9A-Za-z_]|\.[0-9])'
)
EXAMPLE_DATABASE_PATTERN = re.compile(r'melody([_-])example\1v3(?![0-9])')
GO_MOD_VERSION_PATTERN = re.compile(r'(' + re.escape(MODULE_PATH_PREFIX) + r'\S*v4) v3\.[0-9]+\.[0-9]+\S*')
MARKDOWN_LINK_PATTERN = re.compile(r'\]\(([^)\s]+)\)')
CODE_SPAN_MODULE_PATTERN = re.compile(
    r'`(integrations/)?((?:amqp|awss3|bunorm|cron|opentelemetry|outbox|rueidis|websocket)(?:/(?:migrate|mysql|pgsql))?/v3)((?:/[a-z0-9_./]*)?)`'
)
GO_COMMENT_MODULE_PATTERN = re.compile(r'\(melody/v3, ')
FENCED_BLOCK_PATTERN = re.compile(r'```.*?```', re.S)
CODE_SPAN_PATTERN = re.compile(r'`[^`\n]+`')
CODE_PATH_PATTERN = re.compile(r'(?<![0-9A-Za-z_.@-])((?:\.\.?/)*(?:[a-z0-9]+/)*)v3(?=/)')
EXPECTED_COUNTS = ['build_version', 'changelog_fresh', 'code_path', 'code_span_module', 'example_database', 'go_comment_module',
                   'go_mod_replace_path', 'go_mod_version', 'go_sum_dropped', 'markdown_link', 'module_path']


def map_code_span_module(match):
    mapped = map_path('integrations/' + match.group(2))
    if match.group(1) is None:
        mapped = mapped[len('integrations/'):]
    return '`' + mapped + match.group(3) + '`'


def rewrite_markdown_links(text, source_path, destination_path, counts):
    source_directory = posixpath.dirname(source_path)
    destination_directory = posixpath.dirname(destination_path)

    def replace(match):
        target = match.group(1)
        if re.match(r'^[a-z]+:', target) or target.startswith('#') or target.startswith('/'):
            return match.group(0)
        anchor = ''
        if '#' in target:
            target, anchor = target.split('#', 1)
            anchor = '#' + anchor
        if '' == target:
            return match.group(0)
        trailing_slash = target.endswith('/')
        resolved = posixpath.normpath(posixpath.join(source_directory, target))
        mapped = map_path(resolved)
        final_target = mapped if mapped is not None else resolved
        if posixpath.normpath(posixpath.join(destination_directory, target)) == final_target:
            return match.group(0)
        relative = posixpath.relpath(final_target, destination_directory or '.')
        if trailing_slash and not relative.endswith('/'):
            relative += '/'
        if relative == target:
            return match.group(0)
        counts['markdown_link'] = counts.get('markdown_link', 0) + 1
        return '](' + relative + anchor + ')'

    return MARKDOWN_LINK_PATTERN.sub(replace, text)


GO_MOD_REPLACE_PATH_PATTERN = re.compile(r'(=>\s*)(\.\.?/\S*)')


def rewrite_go_mod_replace_paths(text, source_path, destination_path, counts):
    """A replace directive's local path is resolved against the source module and re-spelled from the copy."""
    source_directory = posixpath.dirname(source_path)
    destination_directory = posixpath.dirname(destination_path)

    def replace(match):
        target = match.group(2)
        resolved = posixpath.normpath(posixpath.join(source_directory, target))
        mapped = map_path(resolved)
        final_target = mapped if mapped is not None else resolved
        relative = posixpath.relpath(final_target, destination_directory)
        if not relative.startswith('.'):
            relative = './' + relative
        if relative.rstrip('/') == target.rstrip('/'):
            return match.group(0)
        counts['go_mod_replace_path'] = counts.get('go_mod_replace_path', 0) + 1
        return match.group(1) + relative

    return GO_MOD_REPLACE_PATH_PATTERN.sub(replace, text)


def rewrite_code_paths(text, source_path, destination_path, counts):
    """A path spelled inside a code span or a fenced block of a document (`cd v3/.example`, `../mysql/v3/`) is re-spelled when it
    names a third-major module root: a relative path from the copy's place, a bare one from the repository root."""
    source_directory = posixpath.dirname(source_path)
    destination_directory = posixpath.dirname(destination_path)

    def replace_path(match):
        prefix = match.group(1)
        relative = prefix.startswith('./') or prefix.startswith('../')
        resolved = posixpath.normpath(posixpath.join(source_directory, prefix + 'v3')) if relative else prefix + 'v3'
        mapped = map_path(resolved)
        if mapped is None:
            return match.group(0)
        if relative:
            mapped = posixpath.relpath(mapped, destination_directory or '.')
            if prefix.startswith('./') and not mapped.startswith('.'):
                mapped = './' + mapped
        counts['code_path'] = counts.get('code_path', 0) + 1
        return mapped

    def replace_segment(match):
        return CODE_PATH_PATTERN.sub(replace_path, match.group(0))

    parts = []
    position = 0
    for fence in FENCED_BLOCK_PATTERN.finditer(text):
        parts.append(CODE_SPAN_PATTERN.sub(replace_segment, text[position:fence.start()]))
        parts.append(replace_segment(fence))
        position = fence.end()
    parts.append(CODE_SPAN_PATTERN.sub(replace_segment, text[position:]))
    return ''.join(parts)


def fresh_changelog(original, source_revision):
    """Keeps the header up to the first release section and replaces the body with one entry."""
    header = original.split('\n## ', 1)[0].rstrip('\n')
    header = re.sub(r'\n\nThe example application inside this major keeps no changelog:.*', '', header, flags=re.S)
    return (
        header
        + '\n\n## [Unreleased]\n\n### Added\n\n'
        + '- The major is created as a complete copy of the third major at `' + source_revision + '`, with its module paths'
        + ' moved to the fourth; the history before this line is in the third major\'s changelog.\n'
    )


def main():
    if os.path.isdir(OUTPUT_ROOT) and os.listdir(OUTPUT_ROOT):
        sys.exit('output root ' + OUTPUT_ROOT + ' is not empty; a file of an earlier run would survive unlisted')
    listed = subprocess.run(
        ['git', '-C', REPOSITORY, 'ls-tree', '-r', '--name-only', SOURCE_REVISION],
        check=True, capture_output=True, text=True,
    ).stdout.splitlines()
    sources = [path for path in listed if map_path(path) is not None]
    short_revision = subprocess.run(
        ['git', '-C', REPOSITORY, 'rev-parse', '--short=8', SOURCE_REVISION],
        check=True, capture_output=True, text=True,
    ).stdout.strip()

    manifest = []
    totals = {}
    for source_path in sources:
        destination_path = map_path(source_path)
        content = subprocess.run(
            ['git', '-C', REPOSITORY, 'show', SOURCE_REVISION + ':' + source_path],
            check=True, capture_output=True,
        ).stdout
        counts = {}
        try:
            text = content.decode('utf-8')
        except UnicodeDecodeError:
            text = None
        if text is not None:
            base = posixpath.basename(source_path)
            if 'CHANGELOG.md' == base:
                text = fresh_changelog(text, short_revision)
                counts['changelog_fresh'] = 1
            text, hit = MODULE_PATH_PATTERN.subn(map_module_path, text)
            if hit:
                counts['module_path'] = hit
            text, hit = EXAMPLE_DATABASE_PATTERN.subn(lambda match: 'melody' + match.group(1) + 'example' + match.group(1) + 'v4', text)
            if hit:
                counts['example_database'] = hit
            if base.endswith('.go'):
                text, hit = GO_COMMENT_MODULE_PATTERN.subn('(melody/v4, ', text)
                if hit:
                    counts['go_comment_module'] = hit
            if 'go.mod' == base:
                text, hit = GO_MOD_VERSION_PATTERN.subn(lambda match: match.group(1) + ' v4.0.0', text)
                if hit:
                    counts['go_mod_version'] = hit
            if 'go.mod' == base:
                text = rewrite_go_mod_replace_paths(text, source_path, destination_path, counts)
            if 'go.sum' == base:
                lines = text.splitlines(keepends=True)
                kept = [line for line in lines if MODULE_PATH_PREFIX not in line]
                if len(kept) != len(lines):
                    counts['go_sum_dropped'] = len(lines) - len(kept)
                text = ''.join(kept)
            if 'version.go' == base and source_path.endswith('/version/version.go'):
                text, hit = re.subn(r'buildVersion = "v3\.[0-9]+\.[0-9]+"', 'buildVersion = "v4.0.0"', text)
                if hit:
                    counts['build_version'] = hit
            if base.endswith('.md'):
                text, hit = CODE_SPAN_MODULE_PATTERN.subn(map_code_span_module, text)
                if hit:
                    counts['code_span_module'] = hit
                text = rewrite_code_paths(text, source_path, destination_path, counts)
                text = rewrite_markdown_links(text, source_path, destination_path, counts)
            content = text.encode('utf-8')
        output = os.path.join(OUTPUT_ROOT, destination_path)
        os.makedirs(os.path.dirname(output), exist_ok=True)
        with open(output, 'wb') as handle:
            handle.write(content)
        for key, value in counts.items():
            totals[key] = totals.get(key, 0) + value
        manifest.append((source_path, destination_path, counts))

    with open(MANIFEST, 'w') as handle:
        for source_path, destination_path, counts in manifest:
            handle.write(source_path + '\t' + destination_path + '\t' + ','.join(k + '=' + str(v) for k, v in sorted(counts.items())) + '\n')

    print('files', len(sources))
    for key in EXPECTED_COUNTS:
        print(key, totals.get(key, 0))
    changelog_sources = len([path for path in sources if 'CHANGELOG.md' == posixpath.basename(path)])
    failures = []
    if 1 != totals.get('build_version', 0):
        failures.append('build_version ' + str(totals.get('build_version', 0)) + ', expected 1')
    if changelog_sources != totals.get('changelog_fresh', 0):
        failures.append('changelog_fresh ' + str(totals.get('changelog_fresh', 0)) + ', expected ' + str(changelog_sources))
    for failure in failures:
        print('FAILED', failure)
    if failures:
        sys.exit(1)


if '__main__' == __name__:
    main()
