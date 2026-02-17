module github.com/precision-soft/melody/integrations/bunorm/mysql

go 1.24.0

require (
	github.com/go-sql-driver/mysql v1.8.1
	github.com/precision-soft/melody v0.0.0
	github.com/precision-soft/melody/integrations/bunorm v0.0.0
	github.com/uptrace/bun v1.2.16
	github.com/uptrace/bun/dialect/mysqldialect v1.2.16
)

require (
	filippo.io/edwards25519 v1.1.0 // indirect
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/puzpuzpuz/xsync/v3 v3.5.1 // indirect
	github.com/tmthrgd/go-hex v0.0.0-20190904060850-447a3041c3bc // indirect
	github.com/vmihailenco/msgpack/v5 v5.4.1 // indirect
	github.com/vmihailenco/tagparser/v2 v2.0.0 // indirect
	golang.org/x/mod v0.30.0 // indirect
	golang.org/x/sys v0.38.0 // indirect
)

replace (
	github.com/precision-soft/melody => ../../..
	github.com/precision-soft/melody/integrations/bunorm => ..
)
