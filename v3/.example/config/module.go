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

func NewExampleModule() *Module {
    moduleInstance := &Module{}
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

func (instance *Module) Name() string {
    return "example"
}

func (instance *Module) Description() string {
    return "melody product catalog example application"
}

var _ melodyapplicationcontract.Module = (*Module)(nil)
