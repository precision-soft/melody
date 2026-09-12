-- Runs once, on a FRESH postgres volume, from /docker-entrypoint-initdb.d. It is the sibling of
-- mysql/init.sql and it carries the same two-door rule: a volume provisioned before this file existed
-- never runs it, so the same databases are also created idempotently by e2e_ensure_example_databases in
-- .dev/e2e/common.sh. Neither door may be the only one — without the script a fresh volume would depend
-- on the harness having run at least once, and without the harness step every existing development
-- volume would refuse the examples with "database does not exist".
--
-- Each .example application that keeps a set on postgres holds it in a database of its own, for the same
-- reason the mysql script gives: the bunorm/migrate command family builds its migrator on bun's default
-- bookkeeping tables (bun_migrations, bun_migration_locks) and bun matches an applied migration BY NAME,
-- so two sets sharing one database share one bookkeeping table and the first to land answers for the
-- other. That is not hypothetical here. The identifier of a bun migration is the leading digits of its
-- file name, and both sets were written on the same day: v1's journal set is 20260907000002, and an
-- archive set added to v3 on that day would naturally have taken the very same number. In one database
-- bun would have found the name already applied and skipped the step with no error, no line in db:status
-- and no effect — the table would simply never have existed.
--
-- melody_test stays behind for the live integration suites, which point POSTGRES_DSN at it and create
-- tables of their own there (melody_outbox among them). It is not an example's database any more.
--
-- Unlike mysql, postgres has no CREATE DATABASE IF NOT EXISTS, so a re-run of this file would fail on
-- the second create. That costs nothing here — the entrypoint runs it exactly once, on an empty volume —
-- but it is why the harness door next to it is written as a conditional over pg_database rather than as
-- the same statement twice.

CREATE DATABASE melody_example_v1 OWNER melody;
CREATE DATABASE melody_example_v3 OWNER melody;

GRANT ALL PRIVILEGES ON DATABASE melody_example_v1 TO melody;
GRANT ALL PRIVILEGES ON DATABASE melody_example_v3 TO melody;
