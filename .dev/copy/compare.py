#!/usr/bin/env python3
"""Normalized comparison: every line that differs between a third-major source and its fourth-major copy must become equal under the
inverse normalization; CHANGELOG.md, go.sum and version.go are reported separately with their figures."""
import os
import re
import subprocess
import sys

REPOSITORY = subprocess.run(['git', '-C', os.path.dirname(os.path.abspath(__file__)), 'rev-parse', '--show-toplevel'], check=True, capture_output=True, text=True).stdout.strip()
SOURCE_REVISION = sys.argv[1]
COPY_ROOT = sys.argv[2]
MANIFEST = sys.argv[3]


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
for row in open(MANIFEST):
    source_path, destination_path, _ = row.rstrip('\n').split('\t')
    base = source_path.rsplit('/', 1)[-1]
    old = subprocess.run(['git', '-C', REPOSITORY, 'show', SOURCE_REVISION + ':' + source_path], check=True, capture_output=True).stdout
    new = open(COPY_ROOT + '/' + destination_path, 'rb').read()
    if old == new:
        continue
    if base in ('CHANGELOG.md', 'go.sum') or source_path.endswith('/version/version.go'):
        special[destination_path] = (old.count(b'\n'), new.count(b'\n'))
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
