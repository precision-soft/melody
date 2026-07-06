package pgsql

import (
    "context"
    "errors"
    "os"
    "testing"
    "time"

    "github.com/precision-soft/melody/integrations/bunorm/v2"
    "github.com/uptrace/bun/driver/pgdriver"
)

func TestProviderBuilderMethodsSetConfigs(t *testing.T) {
    poolConfig := NewPoolConfig(10, 2, time.Minute, time.Second)
    timeoutConfig := NewTimeoutConfig(time.Second)
    retryConfig := NewRetryConfig(4, time.Millisecond, time.Second, 3.0)

    provider := NewProvider().
        WithPoolConfig(poolConfig).
        WithTimeoutConfig(timeoutConfig).
        WithRetryConfig(retryConfig)

    if poolConfig != provider.poolConfig {
        t.Fatalf("expected WithPoolConfig to set the pool config")
    }
    if timeoutConfig != provider.timeoutConfig {
        t.Fatalf("expected WithTimeoutConfig to set the timeout config")
    }
    if retryConfig != provider.retryConfig {
        t.Fatalf("expected WithRetryConfig to set the retry config")
    }
}

/* @info Open builds the connector from the connection params and aborts on a post-build hook error before dialing */

func TestProviderOpenBuildsConnectorAndAbortsOnPostBuildHookError(t *testing.T) {
    hookErr := errors.New("hook rejected the connector")

    var seenConnector *pgdriver.Connector
    provider := NewProvider(
        WithInsecure(true),
        WithPostBuildHook(func(ctx context.Context, connector *pgdriver.Connector) error {
            seenConnector = connector

            return hookErr
        }),
    )

    params := bunorm.ConnectionParams{
        Host:     "db.internal",
        Port:     "5432",
        Database: "melody",
        User:     "melody_user",
        Password: "melody_password",
    }

    database, openErr := provider.Open(params, nil)
    if nil != database {
        _ = database.Close()
        t.Fatalf("expected no database handle when the post-build hook fails")
    }

    if false == errors.Is(openErr, hookErr) {
        t.Fatalf("expected the hook error to be wrapped, got: %v", openErr)
    }

    if nil == seenConnector {
        t.Fatalf("expected the post-build hook to receive the connector")
    }

    connectorConfig := seenConnector.Config()
    if "db.internal:5432" != connectorConfig.Addr {
        t.Fatalf("expected the resolved address db.internal:5432, got %q", connectorConfig.Addr)
    }
    if "melody" != connectorConfig.Database {
        t.Fatalf("expected the resolved database melody, got %q", connectorConfig.Database)
    }
    if "melody_user" != connectorConfig.User {
        t.Fatalf("expected the resolved user melody_user, got %q", connectorConfig.User)
    }
    if "melody_password" != connectorConfig.Password {
        t.Fatalf("expected the resolved password to be passed to the driver, got %q", connectorConfig.Password)
    }
}

func TestProviderOpenWithZeroConnectTimeoutConnects(t *testing.T) {
    host := os.Getenv("PGSQL_HOST")
    if "" == host {
        t.Skip("PGSQL_HOST not set; skipping pgsql provider integration test")
    }

    params := bunorm.ConnectionParams{
        Host:     host,
        Port:     os.Getenv("PGSQL_PORT"),
        Database: os.Getenv("PGSQL_DATABASE"),
        User:     os.Getenv("PGSQL_USER"),
        Password: os.Getenv("PGSQL_PASSWORD"),
    }

    provider := NewProvider(WithInsecure(true)).
        WithTimeoutConfig(NewTimeoutConfig(0))

    database, openErr := provider.Open(params, nil)
    if nil != openErr {
        t.Fatalf("expected open to succeed with a zero ConnectTimeout (no deadline) against a reachable database, got: %v", openErr)
    }
    defer database.Close()
}
