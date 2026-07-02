package mysql

import (
    "context"
    "errors"
    "net"
    "os"
    "testing"
    "time"

    driver "github.com/go-sql-driver/mysql"
    "github.com/precision-soft/melody/integrations/bunorm/v2"
)

/* @info provider construction and option resolution */

func TestNewProviderAppliesOptions(t *testing.T) {
    hook := func(ctx context.Context, driverConfig *driver.Config) error {
        return nil
    }

    provider := NewProvider(WithPostBuildHook(hook))

    if nil == provider.postBuildHook {
        t.Fatalf("expected WithPostBuildHook to set the post-build hook")
    }

    if nil != provider.poolConfig || nil != provider.timeoutConfig || nil != provider.retryConfig {
        t.Fatalf("expected pool/timeout/retry configs to default to nil")
    }
}

func TestProviderBuilderMethodsSetConfigs(t *testing.T) {
    poolConfig := NewPoolConfig(10, 2, time.Minute, time.Second)
    timeoutConfig := NewTimeoutConfig(time.Second, 2*time.Second, 3*time.Second)
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

/* @info Open builds the driver config from the connection params and aborts on a post-build hook error before dialing */

func TestProviderOpenBuildsDriverConfigAndAbortsOnPostBuildHookError(t *testing.T) {
    hookErr := errors.New("hook rejected the connector")

    var seenConfig *driver.Config
    provider := NewProvider(
        WithPostBuildHook(func(ctx context.Context, driverConfig *driver.Config) error {
            seenConfig = driverConfig

            return hookErr
        }),
    )

    params := bunorm.ConnectionParams{
        Host:     "db.internal",
        Port:     "3306",
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

    if nil == seenConfig {
        t.Fatalf("expected the post-build hook to receive the driver config")
    }

    if "db.internal:3306" != seenConfig.Addr {
        t.Fatalf("expected the resolved address db.internal:3306, got %q", seenConfig.Addr)
    }
    if "melody" != seenConfig.DBName {
        t.Fatalf("expected the resolved database melody, got %q", seenConfig.DBName)
    }
    if "melody_user" != seenConfig.User {
        t.Fatalf("expected the resolved user melody_user, got %q", seenConfig.User)
    }
    if "melody_password" != seenConfig.Passwd {
        t.Fatalf("expected the resolved password to be passed to the driver, got %q", seenConfig.Passwd)
    }
}

/* @info provider open zero connect timeout */

func TestProviderOpenWithZeroConnectTimeoutConnects(t *testing.T) {
    dsn := os.Getenv("MYSQL_DSN")
    if "" == dsn {
        t.Skip("MYSQL_DSN not set; skipping mysql provider integration test")
    }

    dsnConfig, parseErr := driver.ParseDSN(dsn)
    if nil != parseErr {
        t.Fatalf("parse dsn: %v", parseErr)
    }

    host, port, splitErr := net.SplitHostPort(dsnConfig.Addr)
    if nil != splitErr {
        t.Skipf("MYSQL_DSN address %q is not host:port; skipping", dsnConfig.Addr)
    }

    params := bunorm.ConnectionParams{
        Host:     host,
        Port:     port,
        Database: dsnConfig.DBName,
        User:     dsnConfig.User,
        Password: dsnConfig.Passwd,
    }

    provider := NewProvider().
        WithTimeoutConfig(
            NewTimeoutConfig(0, 30*time.Second, 30*time.Second),
        )

    database, openErr := provider.Open(params, nil)
    if nil != openErr {
        t.Fatalf("expected open to succeed with a zero ConnectTimeout (no deadline) against a reachable database, got: %v", openErr)
    }
    defer database.Close()
}
