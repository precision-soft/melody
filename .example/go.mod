module github.com/precision-soft/melody/.example

go 1.24.9

require (
	github.com/precision-soft/melody v1.17.0
	github.com/precision-soft/melody/integrations/bunorm v1.0.1
	github.com/precision-soft/melody/integrations/bunorm/mysql v1.1.5
	github.com/precision-soft/melody/integrations/cron v1.1.0
	github.com/precision-soft/melody/integrations/rueidis v1.1.0
	github.com/redis/rueidis v1.0.71
	github.com/uptrace/bun v1.2.16
)

require (
	filippo.io/edwards25519 v1.1.0 // indirect
	github.com/go-sql-driver/mysql v1.8.1 // indirect
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/joho/godotenv v1.5.1 // indirect
	github.com/puzpuzpuz/xsync/v3 v3.5.1 // indirect
	github.com/tmthrgd/go-hex v0.0.0-20190904060850-447a3041c3bc // indirect
	github.com/uptrace/bun/dialect/mysqldialect v1.2.16 // indirect
	github.com/urfave/cli/v3 v3.6.1 // indirect
	github.com/vmihailenco/msgpack/v5 v5.4.1 // indirect
	github.com/vmihailenco/tagparser/v2 v2.0.0 // indirect
	golang.org/x/mod v0.30.0 // indirect
	golang.org/x/sys v0.39.0 // indirect
)

replace github.com/precision-soft/melody => ../

replace github.com/precision-soft/melody/integrations/cron => ../integrations/cron

replace github.com/precision-soft/melody/integrations/bunorm => ../integrations/bunorm

replace github.com/precision-soft/melody/integrations/bunorm/mysql => ../integrations/bunorm/mysql

replace github.com/precision-soft/melody/integrations/rueidis => ../integrations/rueidis
