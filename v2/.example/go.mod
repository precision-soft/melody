module github.com/precision-soft/melody/v2/.example

go 1.24.9

require (
	github.com/precision-soft/melody/integrations/bunorm/mysql/v2 v2.0.5
	github.com/precision-soft/melody/integrations/bunorm/v2 v2.1.0
	github.com/precision-soft/melody/integrations/cron/v2 v2.1.0
	github.com/precision-soft/melody/integrations/rueidis/v2 v2.1.0
	github.com/precision-soft/melody/v2 v2.11.0
	github.com/redis/rueidis v1.0.71
	github.com/uptrace/bun v1.2.17
)

require (
	filippo.io/edwards25519 v1.1.0 // indirect
	github.com/go-sql-driver/mysql v1.8.1 // indirect
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/joho/godotenv v1.5.1 // indirect
	github.com/puzpuzpuz/xsync/v3 v3.5.1 // indirect
	github.com/tmthrgd/go-hex v0.0.0-20190904060850-447a3041c3bc // indirect
	github.com/uptrace/bun/dialect/mysqldialect v1.2.17 // indirect
	github.com/urfave/cli/v3 v3.6.1 // indirect
	github.com/vmihailenco/msgpack/v5 v5.4.1 // indirect
	github.com/vmihailenco/tagparser/v2 v2.0.0 // indirect
	golang.org/x/mod v0.33.0 // indirect
	golang.org/x/sys v0.41.0 // indirect
)

replace github.com/precision-soft/melody/v2 => ../

replace github.com/precision-soft/melody/integrations/cron/v2 => ../../integrations/cron/v2

replace github.com/precision-soft/melody/integrations/bunorm/v2 => ../../integrations/bunorm/v2

replace github.com/precision-soft/melody/integrations/bunorm/mysql/v2 => ../../integrations/bunorm/mysql/v2

replace github.com/precision-soft/melody/integrations/rueidis/v2 => ../../integrations/rueidis/v2
