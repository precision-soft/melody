#!/usr/bin/env bash
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"
WORK_DIRECTORY=.temp/copy
mkdir -p "${WORK_DIRECTORY}"

pairs="v3:v4 v3/.example:v4/.example integrations/amqp/v3:integrations/amqp/v4 integrations/awss3/v3:integrations/aws/s3/v4 integrations/bunorm/v3:integrations/bunorm/v4 integrations/bunorm/migrate/v3:integrations/bunorm/migrate/v4 integrations/bunorm/mysql/v3:integrations/bunorm/mysql/v4 integrations/bunorm/pgsql/v3:integrations/bunorm/pgsql/v4 integrations/cron/v3:integrations/cron/v4 integrations/cron/v3/.example:integrations/cron/v4/.example integrations/opentelemetry/v3:integrations/opentelemetry/v4 integrations/outbox/v3:integrations/outbox/v4 integrations/rueidis/v3:integrations/rueidis/v4 integrations/websocket/v3:integrations/websocket/v4"
total_old=0; total_new=0
for p in $pairs; do
  o=${p%%:*}; n=${p##*:}
  (cd "$o" && go test -list '.*' ./... 2>&1 | grep -vE '^(ok|\?|FAIL|no test files)' | grep -E '^(Test|Example|Benchmark|Fuzz)' | sort || true) > "${WORK_DIRECTORY}/testlist_old"
  (cd "$n" && go test -list '.*' ./... 2>&1 | grep -vE '^(ok|\?|FAIL|no test files)' | grep -E '^(Test|Example|Benchmark|Fuzz)' | sort || true) > "${WORK_DIRECTORY}/testlist_new"
  co=$(wc -l < "${WORK_DIRECTORY}/testlist_old"); cn=$(wc -l < "${WORK_DIRECTORY}/testlist_new")
  d=$( (diff "${WORK_DIRECTORY}/testlist_old" "${WORK_DIRECTORY}/testlist_new" || true) | grep -c '^[<>]' || true)
  echo "$o=$co $n=$cn diff=$d"
  total_old=$((total_old+co)); total_new=$((total_new+cn))
done
echo "TOTAL v3=$total_old v4=$total_new"
