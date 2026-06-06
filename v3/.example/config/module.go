package config

import (
    melodyapplicationcontract "github.com/precision-soft/melody/v3/application/contract"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodymessagebus "github.com/precision-soft/melody/v3/messagebus"
    melodymailercontract "github.com/precision-soft/melody/v3/mailer/contract"
    melodymessagebuscontract "github.com/precision-soft/melody/v3/messagebus/contract"
    melodyopenapi "github.com/precision-soft/melody/v3/openapi"
    melodysecurity "github.com/precision-soft/melody/v3/security"
    melodysecuritycontract "github.com/precision-soft/melody/v3/security/contract"
    melodytranslationcontract "github.com/precision-soft/melody/v3/translation/contract"
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

    serverSentEventHub *melodyhttp.ServerSentEventHub

    openApiInfo     melodyopenapi.Info
    openApiRegistry *melodyopenapi.Registry

    mailer melodymailercontract.Mailer
}

func NewExampleModule() *Module {
    moduleInstance := &Module{}
    moduleInstance.buildServerSentEvent()
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
