package mysql

import (
	"context"
	"database/sql"
	"fmt"

	driver "github.com/go-sql-driver/mysql"
	"github.com/precision-soft/melody/integrations/bunorm"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/mysqldialect"

	"github.com/precision-soft/melody/config"
	containercontract "github.com/precision-soft/melody/container/contract"
	"github.com/precision-soft/melody/exception"
)

func NewProvider(
	hostParameterName string,
	portParameterName string,
	databaseParameterName string,
	userParameterName string,
	passwordParameterName string,
) *Provider {
	return &Provider{
		hostParameterName:     hostParameterName,
		portParameterName:     portParameterName,
		databaseParameterName: databaseParameterName,
		userParameterName:     userParameterName,
		passwordParameterName: passwordParameterName,
		poolConfig:            nil,
		timeoutConfig:         nil,
	}
}

func NewProviderWithConfig(
	hostParameterName string,
	portParameterName string,
	databaseParameterName string,
	userParameterName string,
	passwordParameterName string,
	poolConfig *PoolConfig,
	timeoutConfig *TimeoutConfig,
) *Provider {
	return &Provider{
		hostParameterName:     hostParameterName,
		portParameterName:     portParameterName,
		databaseParameterName: databaseParameterName,
		userParameterName:     userParameterName,
		passwordParameterName: passwordParameterName,
		poolConfig:            poolConfig,
		timeoutConfig:         timeoutConfig,
	}
}

type Provider struct {
	hostParameterName     string
	portParameterName     string
	databaseParameterName string
	userParameterName     string
	passwordParameterName string

	poolConfig    *PoolConfig
	timeoutConfig *TimeoutConfig
}

func (instance *Provider) WithPoolConfig(poolConfig *PoolConfig) *Provider {
	instance.poolConfig = poolConfig

	return instance
}

func (instance *Provider) WithTimeoutConfig(timeoutConfig *TimeoutConfig) *Provider {
	instance.timeoutConfig = timeoutConfig

	return instance
}

func (instance *Provider) Open(resolver containercontract.Resolver) (*bun.DB, error) {
	configuration := config.ConfigMustFromResolver(resolver)

	host := configuration.MustGet(instance.hostParameterName).MustString()
	port := configuration.MustGet(instance.portParameterName).MustString()
	databaseName := configuration.MustGet(instance.databaseParameterName).MustString()
	user := configuration.MustGet(instance.userParameterName).MustString()
	password := configuration.MustGet(instance.passwordParameterName).MustString()

	connectionConfig := NewConnectionConfig(host, port, databaseName, user, password)

	poolConfig := instance.poolConfig
	if nil == poolConfig {
		poolConfig = DefaultPoolConfig()
	}

	timeoutConfig := instance.timeoutConfig
	if nil == timeoutConfig {
		timeoutConfig = DefaultTimeoutConfig()
	}

	address := fmt.Sprintf("%s:%s", host, port)

	driverConfig := driver.NewConfig()
	driverConfig.User = user
	driverConfig.Passwd = password
	driverConfig.Net = "tcp"
	driverConfig.Addr = address
	driverConfig.DBName = databaseName
	driverConfig.ParseTime = true
	driverConfig.Timeout = timeoutConfig.ConnectTimeout
	driverConfig.ReadTimeout = timeoutConfig.ReadTimeout
	driverConfig.WriteTimeout = timeoutConfig.WriteTimeout

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

var _ bunorm.Provider = (*Provider)(nil)
