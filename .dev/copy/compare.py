#!/usr/bin/env python3
"""Normalized comparison: every line that differs between a third-major source and its fourth-major copy must become equal under the
inverse normalization; CHANGELOG.md, go.sum and version.go are reported separately with their figures."""
import importlib.util
import os
import re
import subprocess
import sys

REPOSITORY = subprocess.run(['git', '-C', os.path.dirname(os.path.abspath(__file__)), 'rev-parse', '--show-toplevel'], check=True, capture_output=True, text=True).stdout.strip()
SOURCE_REVISION = sys.argv[1]
COPY_ROOT = sys.argv[2]
MANIFEST = sys.argv[3]

sys.dont_write_bytecode = True
copy_specification = importlib.util.spec_from_file_location('copy_tool', os.path.join(os.path.dirname(os.path.abspath(__file__)), 'copy.py'))
copy_tool = importlib.util.module_from_spec(copy_specification)
copy_specification.loader.exec_module(copy_tool)
SHORT_REVISION = subprocess.run(['git', '-C', REPOSITORY, 'rev-parse', '--short=8', SOURCE_REVISION], check=True, capture_output=True, text=True).stdout.strip()
RESIDUAL_PATTERN = re.compile(r'(?<![0-9A-Za-z_./-])v3(?![0-9A-Za-z_.-])|(?<![0-9A-Za-z_.@-])(?:\.\.?/)+(?:[a-z0-9]+/)*v3(?=/)')


def normalize(line):
    line = line.replace('integrations/aws/s3/v4', 'integrations/awss3/v3')
    line = line.replace('aws/s3/v4', 'awss3/v3')
    line = re.sub(r'(?<![0-9A-Za-z_.])v4(?![0-9])', 'v3', line)
    line = line.replace('melody_example_v4', 'melody_example_v3').replace('melody-example-v4', 'melody-example-v3')
    line = re.sub(r'(\.\./)+', '../', line)
    line = re.sub(r'\]\(\./', '](', line)
    return line


unexplained = 0
changed_lines = 0
special = {}
special_failures = []
residual = {}
for row in open(MANIFEST):
    source_path, destination_path, _ = row.rstrip('\n').split('\t')
    base = source_path.rsplit('/', 1)[-1]
    old = subprocess.run(['git', '-C', REPOSITORY, 'show', SOURCE_REVISION + ':' + source_path], check=True, capture_output=True).stdout
    new = open(COPY_ROOT + '/' + destination_path, 'rb').read()
    if 'CHANGELOG.md' != base:
        try:
            for line_number, line in enumerate(new.decode('utf-8').split('\n'), 1):
                if RESIDUAL_PATTERN.search(line):
                    residual.setdefault(destination_path, []).append((line_number, line))
        except UnicodeDecodeError:
            pass
    if old == new:
        continue
    if base in ('CHANGELOG.md', 'go.sum') or source_path.endswith('/version/version.go'):
        special[destination_path] = (old.count(b'\n'), new.count(b'\n'))
        if 'CHANGELOG.md' == base:
            expected = copy_tool.fresh_changelog(old.decode('utf-8'), SHORT_REVISION)
            expected = copy_tool.MODULE_PATH_PATTERN.sub(copy_tool.map_module_path, expected)
            if expected.encode('utf-8') != new:
                special_failures.append(destination_path + ' is not the fresh changelog of ' + SHORT_REVISION)
        elif 'go.sum' == base:
            kept = b''.join(line for line in old.splitlines(keepends=True) if copy_tool.MODULE_PATH_PREFIX.encode('utf-8') not in line)
            if kept != new:
                special_failures.append(destination_path + ' is not its source without the melody lines')
        elif 1 != new.count(b'buildVersion = "v4.0.0"'):
            special_failures.append(destination_path + ' does not read buildVersion = "v4.0.0"')
        continue
    old_lines = old.decode('utf-8').split('\n')
    new_lines = new.decode('utf-8').split('\n')
    if len(old_lines) != len(new_lines):
        print('LINE COUNT', destination_path, len(old_lines), len(new_lines))
        unexplained += 1
        continue
    for index, (old_line, new_line) in enumerate(zip(old_lines, new_lines)):
        if old_line == new_line:
            continue
        changed_lines += 1
        if normalize(old_line) != normalize(new_line):
            unexplained += 1
            print('UNEXPLAINED', destination_path + ':' + str(index + 1))
            print('  -', old_line[:200])
            print('  +', new_line[:200])

print('changed lines', changed_lines, 'unexplained', unexplained)
for path in sorted(special):
    print('special', path, 'lines old/new', special[path][0], special[path][1])
for path in sorted(residual):
    for line_number, line in residual[path]:
        print('RESIDUAL', path + ':' + str(line_number), line.strip()[:200])
print('residual lines', sum(len(lines) for lines in residual.values()), 'in', len(residual), 'file(s)')
for failure in special_failures:
    print('FAILED', failure)
if special_failures:
    sys.exit(1)
