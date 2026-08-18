module github.com/precision-soft/melody/.dev/e2e

go 1.25.0

require (
	github.com/coder/websocket v1.8.12
	github.com/go-sql-driver/mysql v1.8.1
	github.com/precision-soft/melody/integrations/amqp/v3 v3.2.0
	github.com/precision-soft/melody/integrations/awss3/v3 v3.0.3
	github.com/precision-soft/melody/integrations/bunorm/migrate/v3 v3.0.0
	github.com/precision-soft/melody/integrations/bunorm/mysql/v3 v3.0.0
	github.com/precision-soft/melody/integrations/bunorm/pgsql/v3 v3.2.0
	github.com/precision-soft/melody/integrations/bunorm/v3 v3.2.0
	github.com/precision-soft/melody/integrations/outbox/v3 v3.0.0
	github.com/precision-soft/melody/integrations/rueidis/v3 v3.4.0
	github.com/precision-soft/melody/integrations/websocket/v3 v3.0.0
	github.com/precision-soft/melody/v3 v3.11.0
	github.com/redis/rueidis v1.0.71
	github.com/uptrace/bun v1.2.17
	github.com/uptrace/bun/dialect/pgdialect v1.2.17
	github.com/uptrace/bun/driver/pgdriver v1.2.17
)

require (
	filippo.io/edwards25519 v1.1.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/go-ini/ini v1.67.0 // indirect
	github.com/goccy/go-json v0.10.3 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/joho/godotenv v1.5.1 // indirect
	github.com/klauspost/compress v1.17.9 // indirect
	github.com/klauspost/cpuid/v2 v2.2.8 // indirect
	github.com/minio/md5-simd v1.1.2 // indirect
	github.com/minio/minio-go/v7 v7.0.77 // indirect
	github.com/puzpuzpuz/xsync/v3 v3.5.1 // indirect
	github.com/rabbitmq/amqp091-go v1.10.0 // indirect
	github.com/rs/xid v1.6.0 // indirect
	github.com/tmthrgd/go-hex v0.0.0-20190904060850-447a3041c3bc // indirect
	github.com/uptrace/bun/dialect/mysqldialect v1.2.17 // indirect
	github.com/urfave/cli/v3 v3.6.1 // indirect
	github.com/vmihailenco/msgpack/v5 v5.4.1 // indirect
	github.com/vmihailenco/tagparser/v2 v2.0.0 // indirect
	go.opentelemetry.io/otel v1.40.0 // indirect
	go.opentelemetry.io/otel/trace v1.40.0 // indirect
	golang.org/x/crypto v0.48.0 // indirect
	golang.org/x/mod v0.33.0 // indirect
	golang.org/x/net v0.49.0 // indirect
	golang.org/x/sys v0.41.0 // indirect
	golang.org/x/text v0.34.0 // indirect
	mellium.im/sasl v0.3.2 // indirect
)

// dev-only harness: resolve the framework and integrations from the local working tree
// (run with GOWORK=off so these replaces, not the workspace, drive resolution).
replace github.com/precision-soft/melody/v3 => ../../v3

replace github.com/precision-soft/melody/integrations/rueidis/v3 => ../../integrations/rueidis/v3

replace github.com/precision-soft/melody/integrations/outbox/v3 => ../../integrations/outbox/v3

replace github.com/precision-soft/melody/integrations/bunorm/pgsql/v3 => ../../integrations/bunorm/pgsql/v3

replace github.com/precision-soft/melody/integrations/amqp/v3 => ../../integrations/amqp/v3

replace github.com/precision-soft/melody/integrations/awss3/v3 => ../../integrations/awss3/v3

replace github.com/precision-soft/melody/integrations/websocket/v3 => ../../integrations/websocket/v3

replace github.com/precision-soft/melody/integrations/bunorm/migrate/v3 => ../../integrations/bunorm/migrate/v3

replace github.com/precision-soft/melody/integrations/bunorm/v3 => ../../integrations/bunorm/v3

replace github.com/precision-soft/melody/integrations/bunorm/mysql/v3 => ../../integrations/bunorm/mysql/v3
