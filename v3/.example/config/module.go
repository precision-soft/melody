package config

import (
    nethttp "net/http"

    minio "github.com/minio/minio-go/v7"
    "github.com/precision-soft/melody/v3/.example/twofactor"
    melodyawss3 "github.com/precision-soft/melody/integrations/awss3/v3"
    melodyencrypt "github.com/precision-soft/melody/integrations/bunorm/v3/encrypt"
    outbox "github.com/precision-soft/melody/integrations/outbox/v3"
    melodyrueidis "github.com/precision-soft/melody/integrations/rueidis/v3"
    melodyapplicationcontract "github.com/precision-soft/melody/v3/application/contract"
    melodyconfigcontract "github.com/precision-soft/melody/v3/config/contract"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodymailercontract "github.com/precision-soft/melody/v3/mailer/contract"
    melodymessagebuscontract "github.com/precision-soft/melody/v3/messagebus/contract"
    melodyopenapi "github.com/precision-soft/melody/v3/openapi"
    melodysecurity "github.com/precision-soft/melody/v3/security"
    melodysecuritycontract "github.com/precision-soft/melody/v3/security/contract"
    melodytranslationcontract "github.com/precision-soft/melody/v3/translation/contract"
    rueidis "github.com/redis/rueidis"
    bun "github.com/uptrace/bun"
)

type Module struct {
    configuration melodyconfigcontract.Configuration

    messageBusDispatch  melodymessagebuscontract.Bus
    messageBusConsume   melodymessagebuscontract.Bus
    messageBusTransport melodymessagebuscontract.Transport

    jwtSecret            []byte
    tokenValidator       melodysecuritycontract.TokenValidator
    opaqueTokenStore     *melodysecurity.InMemoryTokenStore
    opaqueTokenValidator melodysecuritycontract.TokenValidator

    hmacSecrets melodysecurity.HmacSecretProvider
    hmacApps    melodysecurity.HmacAppRegistry

    impersonatedUsers melodysecuritycontract.ImpersonatedUserResolver

    twoFactorStore *twofactor.Store

    outboxStore *outbox.Store
    outboxRelay *outbox.Relay

    translator melodytranslationcontract.Translator

    serverSentEventHub       *melodyhttp.ServerSentEventHub
    serverSentEventBackplane *melodyrueidis.ServerSentEventBackplane

    openApiInfo     melodyopenapi.Info
    openApiRegistry *melodyopenapi.Registry

    mailer melodymailercontract.Mailer

    metricsMiddleware melodyhttpcontract.Middleware
    metricsHandler    nethttp.Handler

    redisClient rueidis.Client

    storageClient *minio.Client
    storageBucket string
    storage       *melodyawss3.Storage

    database *bun.DB
    cipher   melodyencrypt.Cipher
}

func NewExampleModule(configuration melodyconfigcontract.Configuration) *Module {
    moduleInstance := &Module{configuration: configuration}
    moduleInstance.buildServerSentEvent()
    moduleInstance.buildObservability()
    moduleInstance.buildEncrypt()
    moduleInstance.buildRedis()
    moduleInstance.buildStorage()
    moduleInstance.buildDatabase()
    moduleInstance.buildMessageBus()
    moduleInstance.buildTokenAuth()
    moduleInstance.buildInternalAuth()
    moduleInstance.buildImpersonation()
    moduleInstance.buildTwoFactor()
    moduleInstance.buildOutbox()
    moduleInstance.buildTranslation()
    moduleInstance.buildOpenApi()
    moduleInstance.buildMailer()

    return moduleInstance
}

/* env-key constants for the example's opt-in live integrations. melody auto-registers
   every .env key as a same-named parameter, so these double as the parameter names the
   eager build steps read through environmentValue. */
const (
    environmentKeyMysqlHost     = "MYSQL_HOST"
    environmentKeyMysqlPort     = "MYSQL_PORT"
    environmentKeyMysqlDatabase = "MYSQL_DATABASE"
    environmentKeyMysqlUser     = "MYSQL_USER"
    environmentKeyMysqlPassword = "MYSQL_PASSWORD"

    environmentKeyRedisAddress = "REDIS_ADDRESS"

    environmentKeyAmqpDsn = "AMQP_DSN"

    environmentKeyS3Endpoint  = "S3_ENDPOINT"
    environmentKeyS3AccessKey = "S3_ACCESS_KEY"
    environmentKeyS3SecretKey = "S3_SECRET_KEY"
    environmentKeyS3Secure    = "S3_SECURE"
    environmentKeyS3Region    = "S3_REGION"
    environmentKeyS3Bucket    = "S3_BUCKET"

    environmentKeySmtpAddress = "SMTP_ADDRESS"
)

/* environmentValue reads a value melody auto-registered from the .env files (every env
   key becomes a same-named parameter). Returns "" when the key is absent so the eager
   build steps keep their "unset means skip this integration" behaviour. */
func (instance *Module) environmentValue(key string) string {
    parameter := instance.configuration.Get(key)
    if nil == parameter {
        return ""
    }

    return parameter.String()
}

func (instance *Module) Name() string {
    return "example"
}

func (instance *Module) Description() string {
    return "melody product catalog example application"
}

var _ melodyapplicationcontract.Module = (*Module)(nil)
