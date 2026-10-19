#!/usr/bin/env bash
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"
WORK_DIRECTORY=.temp/copy
mkdir -p "${WORK_DIRECTORY}"

pairs="v3:v4 v3/.example:v4/.example integrations/amqp/v3:integrations/amqp/v4 integrations/awss3/v3:integrations/aws/s3/v4 integrations/bunorm/v3:integrations/bunorm/v4 integrations/bunorm/migrate/v3:integrations/bunorm/migrate/v4 integrations/bunorm/mysql/v3:integrations/bunorm/mysql/v4 integrations/bunorm/pgsql/v3:integrations/bunorm/pgsql/v4 integrations/cron/v3:integrations/cron/v4 integrations/cron/v3/.example:integrations/cron/v4/.example integrations/opentelemetry/v3:integrations/opentelemetry/v4 integrations/outbox/v3:integrations/outbox/v4 integrations/rueidis/v3:integrations/rueidis/v4 integrations/websocket/v3:integrations/websocket/v4"
total_old=0; total_new=0
failed_modules=0
for p in $pairs; do
  o=${p%%:*}; n=${p##*:}
  for side in old new; do
    if [ old = "${side}" ]; then directory="${o}"; else directory="${n}"; fi
    (cd "${directory}" && go test -list '.*' ./... > "${WORK_DIRECTORY}/transcript_${side}" 2>&1 || true)
    if grep -qE '\[build failed\]|^FAIL|^# ' "${WORK_DIRECTORY}/transcript_${side}"; then
      echo "BUILD FAILED ${directory} (transcript: ${WORK_DIRECTORY}/$(echo "${directory}" | tr '/' '_').transcript)"
      cp "${WORK_DIRECTORY}/transcript_${side}" "${WORK_DIRECTORY}/$(echo "${directory}" | tr '/' '_').transcript"
      failed_modules=$((failed_modules+1))
    fi
    (grep -E '^(Test|Example|Benchmark|Fuzz)' "${WORK_DIRECTORY}/transcript_${side}" | sort || true) > "${WORK_DIRECTORY}/testlist_${side}"
  done
  co=$(wc -l < "${WORK_DIRECTORY}/testlist_old"); cn=$(wc -l < "${WORK_DIRECTORY}/testlist_new")
  d=$( (diff "${WORK_DIRECTORY}/testlist_old" "${WORK_DIRECTORY}/testlist_new" || true) | grep -c '^[<>]' || true)
  echo "$o=$co $n=$cn diff=$d"
  total_old=$((total_old+co)); total_new=$((total_new+cn))
done
echo "TOTAL v3=$total_old v4=$total_new"
if [ 0 -ne "${failed_modules}" ]; then
  echo "BUILD FAILED in ${failed_modules} module(s)"
  exit 1
fi
