package config

import (
    melodyapplicationcontract "github.com/precision-soft/melody/application/contract"
    melodybunorm "github.com/precision-soft/melody/integrations/bunorm"
    melodyrueidis "github.com/precision-soft/melody/integrations/rueidis"
    melodyrueidiscache "github.com/precision-soft/melody/integrations/rueidis/cache"
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

    ParameterJournalDatabaseHost     = "app.journal_database.host"
    ParameterJournalDatabasePort     = "app.journal_database.port"
    ParameterJournalDatabaseName     = "app.journal_database.name"
    ParameterJournalDatabaseUser     = "app.journal_database.user"
    ParameterJournalDatabasePassword = "app.journal_database.password"
    ParameterJournalDatabaseInsecure = "app.journal_database.insecure"

    ParameterRedisAddress  = "app.redis.address"
    ParameterRedisUser     = "app.redis.user"
    ParameterRedisPassword = "app.redis.password"

    ParameterApiToken         = "app.api_token"
    ParameterCorsAllowOrigins = "app.cors.allow_origins"
    ParameterSessionFile      = "app.session_file"
)

type Module struct {
    /* the databases are held as one registry rather than as *bun.DB handles: the registry opens each connection on first use — after the framework's own services exist — and its Close reaches every pool, which bare handles on module fields would never get. The wiring beside it remembers which of the two connections the environment armed, because the registry alone cannot say whether its default is the catalog or a lone journal. */
    databaseRegistry *melodybunorm.ManagerRegistry
    databaseWiring   databaseWiring

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
