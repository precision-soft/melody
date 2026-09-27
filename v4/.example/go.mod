module github.com/precision-soft/melody/v4/.example

go 1.25.0

require (
	github.com/minio/minio-go/v7 v7.0.77
	github.com/precision-soft/melody/integrations/amqp/v4 v4.0.0
	github.com/precision-soft/melody/integrations/aws/s3/v4 v4.0.0
	github.com/precision-soft/melody/integrations/bunorm/migrate/v4 v4.0.0
	github.com/precision-soft/melody/integrations/bunorm/mysql/v4 v4.0.0
	github.com/precision-soft/melody/integrations/bunorm/pgsql/v4 v4.0.0
	github.com/precision-soft/melody/integrations/bunorm/v4 v4.0.0
	github.com/precision-soft/melody/integrations/cron/v4 v4.0.0
	github.com/precision-soft/melody/integrations/opentelemetry/v4 v4.0.0
	github.com/precision-soft/melody/integrations/outbox/v4 v4.0.0
	github.com/precision-soft/melody/integrations/rueidis/v4 v4.0.0
	github.com/precision-soft/melody/integrations/websocket/v4 v4.0.0
	github.com/precision-soft/melody/v4 v4.0.0
	github.com/redis/rueidis v1.0.71
	github.com/uptrace/bun v1.2.17
	github.com/uptrace/bun/dialect/mysqldialect v1.2.17
	github.com/uptrace/bun/dialect/pgdialect v1.2.17
	github.com/uptrace/bun/driver/pgdriver v1.2.17
	golang.org/x/crypto v0.51.0
)

require (
	filippo.io/edwards25519 v1.1.0 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cenkalti/backoff/v5 v5.0.3 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/coder/websocket v1.8.12 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/go-ini/ini v1.67.0 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-sql-driver/mysql v1.8.1 // indirect
	github.com/goccy/go-json v0.10.3 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.29.0 // indirect
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
	github.com/rabbitmq/amqp091-go v1.13.0 // indirect
	github.com/rs/xid v1.6.0 // indirect
	github.com/tmthrgd/go-hex v0.0.0-20190904060850-447a3041c3bc // indirect
	github.com/urfave/cli/v3 v3.6.1 // indirect
	github.com/vmihailenco/msgpack/v5 v5.4.1 // indirect
	github.com/vmihailenco/tagparser/v2 v2.0.0 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/otel v1.44.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.44.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.44.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp v1.44.0 // indirect
	go.opentelemetry.io/otel/exporters/prometheus v0.66.0 // indirect
	go.opentelemetry.io/otel/metric v1.44.0 // indirect
	go.opentelemetry.io/otel/sdk v1.44.0 // indirect
	go.opentelemetry.io/otel/sdk/metric v1.44.0 // indirect
	go.opentelemetry.io/otel/trace v1.44.0 // indirect
	go.opentelemetry.io/proto/otlp v1.10.0 // indirect
	go.yaml.in/yaml/v2 v2.4.4 // indirect
	golang.org/x/mod v0.35.0 // indirect
	golang.org/x/net v0.55.0 // indirect
	golang.org/x/sys v0.45.0 // indirect
	golang.org/x/text v0.37.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260526163538-3dc84a4a5aaa // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260526163538-3dc84a4a5aaa // indirect
	google.golang.org/grpc v1.83.1 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
	mellium.im/sasl v0.3.2 // indirect
)

replace github.com/precision-soft/melody/v4 => ../

replace github.com/precision-soft/melody/integrations/cron/v4 => ../../integrations/cron/v4

replace github.com/precision-soft/melody/integrations/amqp/v4 => ../../integrations/amqp/v4

replace github.com/precision-soft/melody/integrations/aws/s3/v4 => ../../integrations/aws/s3/v4

replace github.com/precision-soft/melody/integrations/opentelemetry/v4 => ../../integrations/opentelemetry/v4

replace github.com/precision-soft/melody/integrations/websocket/v4 => ../../integrations/websocket/v4

replace github.com/precision-soft/melody/integrations/rueidis/v4 => ../../integrations/rueidis/v4

replace github.com/precision-soft/melody/integrations/bunorm/v4 => ../../integrations/bunorm/v4

replace github.com/precision-soft/melody/integrations/bunorm/mysql/v4 => ../../integrations/bunorm/mysql/v4

replace github.com/precision-soft/melody/integrations/bunorm/pgsql/v4 => ../../integrations/bunorm/pgsql/v4

replace github.com/precision-soft/melody/integrations/bunorm/migrate/v4 => ../../integrations/bunorm/migrate/v4

replace github.com/precision-soft/melody/integrations/outbox/v4 => ../../integrations/outbox/v4
