package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	driver "github.com/go-sql-driver/mysql"
	"github.com/precision-soft/melody/exception"
	"github.com/precision-soft/melody/integrations/bunorm"
	loggingcontract "github.com/precision-soft/melody/logging/contract"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/mysqldialect"
)

func NewProvider(
	providerOptions ...ProviderOption,
) *Provider {
	provider := &Provider{
		poolConfig:    nil,
		timeoutConfig: nil,
		retryConfig:   nil,
		postBuildHook: nil,
	}
	for _, providerOption := range providerOptions {
		providerOption(provider)
	}
	return provider
}

type Provider struct {
	poolConfig    *PoolConfig
	timeoutConfig *TimeoutConfig
	retryConfig   *RetryConfig
	postBuildHook PostBuildHook
}

func (instance *Provider) WithPoolConfig(poolConfig *PoolConfig) *Provider {
	instance.poolConfig = poolConfig

	return instance
}

func (instance *Provider) WithTimeoutConfig(timeoutConfig *TimeoutConfig) *Provider {
	instance.timeoutConfig = timeoutConfig

	return instance
}

func (instance *Provider) WithRetryConfig(retryConfig *RetryConfig) *Provider {
	instance.retryConfig = retryConfig

	return instance
}

func (instance *Provider) Open(params bunorm.ConnectionParams, logger loggingcontract.Logger) (*bun.DB, error) {
	if nil == instance.retryConfig {
		return instance.open(params)
	}

	return instance.openWithRetry(params, logger)
}

func (instance *Provider) openWithRetry(params bunorm.ConnectionParams, logger loggingcontract.Logger) (*bun.DB, error) {
	attempt := uint32(0)
	maxAttempts := instance.retryConfig.MaxAttempts
	if 0 == maxAttempts {
		maxAttempts = 3
	}

	for {
		attempt = attempt + 1

		database, openErr := instance.open(params)
		if nil == openErr {
			if 1 < attempt {
				logger.Info(
					"database connection successful after retry",
					map[string]interface{}{
						"attempt": attempt,
					},
				)
			}

			return database, nil
		}

		if false == instance.isTransientError(openErr) {
			logger.Error(
				"database connection failed with non-transient error",
				map[string]interface{}{
					"attempt": attempt,
					"error":   openErr.Error(),
				},
			)

			return nil, openErr
		}

		if attempt >= maxAttempts {
			logger.Error(
				"database connection failed after max retry attempts",
				map[string]interface{}{
					"attempt":     attempt,
					"maxAttempts": maxAttempts,
					"error":       openErr.Error(),
				},
			)

			return nil, openErr
		}

		delay := instance.computeBackoffDelay(attempt)

		logger.Warning(
			"database connection failed and retrying",
			map[string]interface{}{
				"attempt":     attempt,
				"maxAttempts": maxAttempts,
				"retryIn":     delay.String(),
				"error":       openErr.Error(),
			},
		)

		time.Sleep(delay)
	}
}

func (instance *Provider) open(params bunorm.ConnectionParams) (*bun.DB, error) {
	connectionConfig := NewConnectionConfig(params.Host, params.Port, params.Database, params.User, params.Password)

	poolConfig := instance.poolConfig
	if nil == poolConfig {
		poolConfig = DefaultPoolConfig()
	}

	timeoutConfig := instance.timeoutConfig
	if nil == timeoutConfig {
		timeoutConfig = DefaultTimeoutConfig()
	}

	address := fmt.Sprintf("%s:%s", params.Host, params.Port)

	driverConfig := driver.NewConfig()
	driverConfig.User = params.User
	driverConfig.Passwd = params.Password
	driverConfig.Net = "tcp"
	driverConfig.Addr = address
	driverConfig.DBName = params.Database
	driverConfig.ParseTime = true
	driverConfig.Timeout = timeoutConfig.ConnectTimeout
	driverConfig.ReadTimeout = timeoutConfig.ReadTimeout
	driverConfig.WriteTimeout = timeoutConfig.WriteTimeout

	if nil != instance.postBuildHook {
		hookContext := context.Background()
		hookCancel := func() {}
		if 0 < timeoutConfig.ConnectTimeout {
			hookContext, hookCancel = context.WithTimeout(context.Background(), timeoutConfig.ConnectTimeout)
		}
		defer hookCancel()

		hookErr := instance.postBuildHook(hookContext, driverConfig)
		if nil != hookErr {
			return nil, exception.NewError(
				"mysql database connector configuration failed",
				connectionConfig.SafeContext(),
				hookErr,
			)
		}
	}

	connector, connectorErr := driver.NewConnector(driverConfig)
	if nil != connectorErr {
		return nil, exception.NewError(
			"database connector creation failed",
			connectionConfig.SafeContext(),
			connectorErr,
		)
	}

	sqlDatabase := sql.OpenDB(connector)

	sqlDatabase.SetMaxOpenConns(poolConfig.MaxOpenConnections)
	sqlDatabase.SetMaxIdleConns(poolConfig.MaxIdleConnections)
	sqlDatabase.SetConnMaxLifetime(poolConfig.ConnectionMaxLifetime)
	sqlDatabase.SetConnMaxIdleTime(poolConfig.ConnectionMaxIdleTime)

	database := bun.NewDB(sqlDatabase, mysqldialect.New())

	ctx, cancel := context.WithTimeout(context.Background(), timeoutConfig.ConnectTimeout)
	defer cancel()

	pingErr := database.PingContext(ctx)
	if nil != pingErr {
		_ = database.Close()

		return nil, exception.NewError(
			"database connection failed",
			connectionConfig.SafeContext(),
			pingErr,
		)
	}

	return database, nil
}

func (instance *Provider) computeBackoffDelay(attempt uint32) time.Duration {
	initialDelay := instance.retryConfig.InitialDelay
	if 0 == initialDelay {
		initialDelay = 500 * time.Millisecond
	}

	maxDelay := instance.retryConfig.MaxDelay
	if 0 == maxDelay {
		maxDelay = 5 * time.Second
	}

	backoffMultiplier := instance.retryConfig.BackoffMultiplier
	if 0.0 == backoffMultiplier {
		backoffMultiplier = 2.0
	}

	multiplier := 1.0
	exponent := attempt - 1

	for i := uint32(0); i < exponent; i = i + 1 {
		multiplier = multiplier * backoffMultiplier
	}

	delay := time.Duration(float64(initialDelay) * multiplier)
	if delay > maxDelay {
		delay = maxDelay
	}

	return delay
}

func (instance *Provider) isTransientError(inputErr error) bool {
	if nil == inputErr {
		return false
	}

	var dnsErr *net.DNSError
	if true == errors.As(inputErr, &dnsErr) {
		return true
	}

	var netErr net.Error
	if true == errors.As(inputErr, &netErr) {
		if true == netErr.Timeout() {
			return true
		}

		if temp, ok := netErr.(interface{ Temporary() bool }); true == ok && true == temp.Temporary() {
			return true
		}
	}

	transientMarkers := []string{
		"connection refused",
		"i/o timeout",
		"timeout",
		"temporary failure",
		"no such host",
		"server closed the connection",
		"bad connection",
		"too many connections",
		"network is unreachable",
		"host is down",
		"broken pipe",
	}

	currentErr := inputErr
	for nil != currentErr {
		message := strings.ToLower(currentErr.Error())

		for _, marker := range transientMarkers {
			if "" == marker {
				continue
			}

			if true == strings.Contains(message, marker) {
				return true
			}
		}

		currentErr = errors.Unwrap(currentErr)
	}

	return false
}

var _ bunorm.Provider = (*Provider)(nil)
