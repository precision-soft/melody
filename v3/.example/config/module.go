package config

import (
    nethttp "net/http"

    minio "github.com/minio/minio-go/v7"
    melodyencrypt "github.com/precision-soft/melody/integrations/bunorm/v3/encrypt"
    melodyrueidis "github.com/precision-soft/melody/integrations/rueidis/v3"
    melodyapplicationcontract "github.com/precision-soft/melody/v3/application/contract"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodymailercontract "github.com/precision-soft/melody/v3/mailer/contract"
    melodymessagebus "github.com/precision-soft/melody/v3/messagebus"
    melodymessagebuscontract "github.com/precision-soft/melody/v3/messagebus/contract"
    melodyopenapi "github.com/precision-soft/melody/v3/openapi"
    melodysecurity "github.com/precision-soft/melody/v3/security"
    melodysecuritycontract "github.com/precision-soft/melody/v3/security/contract"
    melodytranslationcontract "github.com/precision-soft/melody/v3/translation/contract"
    rueidis "github.com/redis/rueidis"
    bun "github.com/uptrace/bun"
)

type Module struct {
    messageBusDispatch       melodymessagebuscontract.Bus
    messageBusConsume        melodymessagebuscontract.Bus
    messageBusTransport      melodymessagebuscontract.Transport
    messageBusConsumeCommand *melodymessagebus.ConsumeCommand

    jwtSecret            []byte
    tokenValidator       melodysecuritycontract.TokenValidator
    opaqueTokenStore     *melodysecurity.InMemoryTokenStore
    opaqueTokenValidator melodysecuritycontract.TokenValidator

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
