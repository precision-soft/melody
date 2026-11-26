package config

import (
    nethttp "net/http"
    "time"

    melodyrueidis "github.com/precision-soft/melody/integrations/rueidis/v4"
    melodywebsocket "github.com/precision-soft/melody/integrations/websocket/v4"
    "github.com/precision-soft/melody/v4/.example/subscriber"
    "github.com/precision-soft/melody/v4/bound"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    melodyhttpcontract "github.com/precision-soft/melody/v4/http/contract"
    melodylogging "github.com/precision-soft/melody/v4/logging"
)

/* newServerSentEventHub is the hub's provider: the hub is built where the application's logger exists, and with redis the backplane is installed on it over the connection, whose resolution records the edge that closes the connection after the hub. */
func (instance *Module) newServerSentEventHub(resolver melodycontainercontract.Resolver) (*melodyhttp.ServerSentEventHub, error) {
    logger, loggerErr := melodylogging.LoggerFromResolver(resolver.Runtime().Context(), resolver)
    if nil != loggerErr {
        return nil, loggerErr
    }

    var connection *melodyrueidis.Connection
    if true == instance.redisConfigured() {
        resolvedConnection, connectionErr := redisConnectionFromResolver(resolver)
        if nil != connectionErr {
            return nil, connectionErr
        }

        connection = resolvedConnection
    }

    serverSentEventHub := melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{Logger: logger})

    if nil != connection {
        melodyrueidis.NewServerSentEventBackplane(connection, serverSentEventHub, melodyrueidis.ServerSentEventBackplaneConfig{
            Logger: logger,
        })
    }

    instance.builtServerSentEventHub.Store(serverSentEventHub)

    return serverSentEventHub, nil
}

/* websocketRouteName names the /ws route, which StreamSlotMiddleware counts in the event stream's slots */
const websocketRouteName = "example.websocket"

/* websocketStreamHandlerConfig is the /ws stream's handler configuration. IdleTimeout is written out although thirty seconds is its default: the keepalive ping is the only thing that reaps a tab that went away without a fin. OriginPatterns stays unset on purpose: the upgrade authenticates through the session cookie, which a browser sends cross-site too, so the library's same-origin default is what stops a foreign page riding a visitor's session; a client that sends no Origin header is not origin-checked. */
func websocketStreamHandlerConfig() melodywebsocket.StreamHandlerConfig {
    return melodywebsocket.StreamHandlerConfig{
        IdleTimeout: bound.Of(30 * time.Second),
    }
}

/* websocketStreamHandler serves the hub over /ws through the websocket integration's handler, the hub resolved per request since its provider builds it at boot, after the routes are registered. */
func websocketStreamHandler(config melodywebsocket.StreamHandlerConfig) melodyhttpcontract.Handler {
    return func(runtimeInstance melodycontainercontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        hub, hubErr := subscriber.CatalogNotificationHubFromRuntime(runtimeInstance)
        if nil != hubErr {
            return nil, hubErr
        }

        return melodywebsocket.NewStreamHandler(hub, config)(runtimeInstance, writer, request)
    }
}
