#!/usr/bin/env bash
set -euo pipefail
IFS=$'\n\t'

# writes the files the tree does not commit because a generator produces them from the code and a build needs them:
# the v3 and v4 examples' service wiring (generated/wiring_gen.go), without which the example does not compile. It needs go
# and nothing else — no backend, since the generator reads the source, not a running application — so it runs in
# the dev container or on a ci runner: the container's entrypoint runs it at start, all.sh before its lanes, and the
# ci job that builds the example before the build. The example's route manifest (assets/routes.json) is not written
# here: exporting it boots the application against its backends, and only `npm run typecheck` reads it, which
# generates it first. The three dev containers start this at once over one bind-mounted tree, so the runs take a lock
# in its scratch.

REPOSITORY_ROOT_DIRECTORY_STRING="$(cd -P "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

mkdir -p "${REPOSITORY_ROOT_DIRECTORY_STRING}/.temp"
exec 9>"${REPOSITORY_ROOT_DIRECTORY_STRING}/.temp/generate.lock"
flock 9

for EXAMPLE_RELATIVE_PATH_STRING in v3/.example v4/.example; do
    (cd "${REPOSITORY_ROOT_DIRECTORY_STRING}/${EXAMPLE_RELATIVE_PATH_STRING}" && go generate ./generated)
done
