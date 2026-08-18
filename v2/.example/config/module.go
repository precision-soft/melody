package config

import (
    melodybunorm "github.com/precision-soft/melody/integrations/bunorm/v2"
    melodyrueidis "github.com/precision-soft/melody/integrations/rueidis/v2"
    melodyrueidiscache "github.com/precision-soft/melody/integrations/rueidis/v2/cache"
    melodyapplicationcontract "github.com/precision-soft/melody/v2/application/contract"
    "github.com/redis/rueidis"
)

const (
    /* the parameter names the live integrations read. An unset endpoint leaves the integration unwired: no service, no route, nothing dialled — which is what keeps the example bootable with no backend at all. */
    ParameterDatabaseHost     = "app.database.host"
    ParameterDatabasePort     = "app.database.port"
    ParameterDatabaseName     = "app.database.name"
    ParameterDatabaseUser     = "app.database.user"
    ParameterDatabasePassword = "app.database.password"
    ParameterDatabaseInsecure = "app.database.insecure"

    ParameterRedisAddress  = "app.redis.address"
    ParameterRedisUser     = "app.redis.user"
    ParameterRedisPassword = "app.redis.password"

    ParameterApiToken         = "app.api_token"
    ParameterCorsAllowOrigins = "app.cors.allow_origins"
    ParameterSessionFile      = "app.session_file"
)

type Module struct {
    /* the database is held as its registry rather than as a *bun.DB: the registry opens on first use — after the framework's own services exist — and its Close reaches the pool, which a bare handle on a module field would never get. */
    databaseRegistry *melodybunorm.ManagerRegistry

    /* redis is opened while the modules are wired, because the rate-limit middleware needs a live limiter at the moment a route is declared. The asymmetry with the database is the point: each integration is wired the way its own API allows. */
    redisClient       rueidis.Client
    redisCacheBackend *melodyrueidiscache.BackendService
    redisRateLimiter  *melodyrueidis.RateLimiter

    /* the api token is captured at service registration, because RegisterSecurity receives only the builder and the firewall declaration still needs the configured value */
    apiToken string
}

func NewExampleModule() *Module {
    return &Module{}
}

func (instance *Module) Name() string {
    return "example"
}

func (instance *Module) Description() string {
    return "melody product catalog example application"
}

var _ melodyapplicationcontract.Module = (*Module)(nil)
