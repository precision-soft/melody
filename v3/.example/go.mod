module github.com/precision-soft/melody/v3/.example

go 1.25.0

require (
	github.com/minio/minio-go/v7 v7.0.77
	github.com/precision-soft/melody/integrations/amqp/v3 v3.0.0
	github.com/precision-soft/melody/integrations/awss3/v3 v3.0.0
	github.com/precision-soft/melody/integrations/bunorm/mysql/v3 v3.1.0
	github.com/precision-soft/melody/integrations/bunorm/v3 v3.1.0
	github.com/precision-soft/melody/integrations/cron/v3 v3.0.0
	github.com/precision-soft/melody/integrations/opentelemetry/v3 v3.0.0
	github.com/precision-soft/melody/integrations/rueidis/v3 v3.2.0
	github.com/precision-soft/melody/integrations/websocket/v3 v3.0.0
	github.com/precision-soft/melody/v3 v3.7.0
	github.com/redis/rueidis v1.0.71
	github.com/uptrace/bun v1.2.16
)

require (
	filippo.io/edwards25519 v1.1.0 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/coder/websocket v1.8.12 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/go-ini/ini v1.67.0 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-sql-driver/mysql v1.8.1 // indirect
	github.com/goccy/go-json v0.10.3 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/joho/godotenv v1.5.1 // indirect
	github.com/klauspost/compress v1.18.0 // indirect
	github.com/klauspost/cpuid/v2 v2.2.8 // indirect
	github.com/minio/md5-simd v1.1.2 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/prometheus/client_golang v1.23.2 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.67.5 // indirect
	github.com/prometheus/otlptranslator v1.0.0 // indirect
	github.com/prometheus/procfs v0.20.1 // indirect
	github.com/puzpuzpuz/xsync/v3 v3.5.1 // indirect
	github.com/rabbitmq/amqp091-go v1.10.0 // indirect
	github.com/rs/xid v1.6.0 // indirect
	github.com/tmthrgd/go-hex v0.0.0-20190904060850-447a3041c3bc // indirect
	github.com/uptrace/bun/dialect/mysqldialect v1.2.16 // indirect
	github.com/urfave/cli/v3 v3.6.1 // indirect
	github.com/vmihailenco/msgpack/v5 v5.4.1 // indirect
	github.com/vmihailenco/tagparser/v2 v2.0.0 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/otel v1.44.0 // indirect
	go.opentelemetry.io/otel/exporters/prometheus v0.66.0 // indirect
	go.opentelemetry.io/otel/metric v1.44.0 // indirect
	go.opentelemetry.io/otel/sdk v1.44.0 // indirect
	go.opentelemetry.io/otel/sdk/metric v1.44.0 // indirect
	go.opentelemetry.io/otel/trace v1.44.0 // indirect
	go.yaml.in/yaml/v2 v2.4.4 // indirect
	golang.org/x/crypto v0.46.0 // indirect
	golang.org/x/mod v0.30.0 // indirect
	golang.org/x/net v0.48.0 // indirect
	golang.org/x/sys v0.45.0 // indirect
	golang.org/x/text v0.32.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

replace github.com/precision-soft/melody/v3 => ../

replace github.com/precision-soft/melody/integrations/cron/v3 => ../../integrations/cron/v3

replace github.com/precision-soft/melody/integrations/amqp/v3 => ../../integrations/amqp/v3

replace github.com/precision-soft/melody/integrations/awss3/v3 => ../../integrations/awss3/v3

replace github.com/precision-soft/melody/integrations/opentelemetry/v3 => ../../integrations/opentelemetry/v3

replace github.com/precision-soft/melody/integrations/websocket/v3 => ../../integrations/websocket/v3

replace github.com/precision-soft/melody/integrations/rueidis/v3 => ../../integrations/rueidis/v3

replace github.com/precision-soft/melody/integrations/bunorm/v3 => ../../integrations/bunorm/v3

replace github.com/precision-soft/melody/integrations/bunorm/mysql/v3 => ../../integrations/bunorm/mysql/v3
