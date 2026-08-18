-- Runs once, on a FRESH mysql volume, from /docker-entrypoint-initdb.d. It is what a new machine and the
-- CI e2e lane get; a volume provisioned before this file existed never runs it, so the same three databases
-- are also created idempotently by e2e_require_dev_service in .dev/e2e/common.sh. Neither door may be the
-- only one: without the script a fresh volume would depend on the harness having run at least once, and
-- without the harness step every existing development volume would refuse the examples with "unknown
-- database".
--
-- Each .example application holds its schema in a database of its own. They share one mysql container but
-- not one database, because the bunorm/migrate command family builds its migrator on bun's default
-- bookkeeping tables (bun_migrations, bun_migration_locks) and bun matches an applied migration by NAME:
-- three majors sharing one database would share one bookkeeping table, and the first set to land would
-- answer for the others.
--
-- melody_example stays behind for the live integration suites, which point MYSQL_DSN at it and create
-- tables of their own there. It is not an example's database any more.

CREATE
DATABASE IF NOT EXISTS `melody_example_v1` CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
CREATE
DATABASE IF NOT EXISTS `melody_example_v2` CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
CREATE
DATABASE IF NOT EXISTS `melody_example_v3` CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;

-- the wildcard covers the three above in one statement and any later major without another grant
GRANT ALL PRIVILEGES ON `melody\_example\_%`.* TO
'melody'@'%';
FLUSH
PRIVILEGES;
