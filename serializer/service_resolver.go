package serializer

import (
    "github.com/precision-soft/melody/exception"
    "github.com/precision-soft/melody/internal"
    "github.com/precision-soft/melody/logging"
    "github.com/precision-soft/melody/runtime"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
    serializercontract "github.com/precision-soft/melody/serializer/contract"
)

/*
ServiceSerializer is the default serializer — the json one, registered by the
application boot behind a Has gate so an application or module registering it
first substitutes it. It is what SerializerMustFromRuntime and
SerializerFromRuntime answer.

ServiceSerializerManager is what content negotiation reads: the media type a
response is served under is chosen by the manager from the request's Accept
header. Registering a serializer under ServiceSerializer therefore changes what
the two resolvers hand a caller, and what a request is served only on the
fallback path the result handler takes when the manager is absent or the
negotiation itself failed — to add a media type, register
ServiceSerializerManager with a manager built by NewSerializerManager carrying
the wider map.
*/
const (
    ServiceSerializer        = "service.serializer"
    ServiceSerializerManager = "service.serializer.manager"
)

func SerializerManagerMustFromRuntime(runtimeInstance runtimecontract.Runtime) *SerializerManager {
    return runtime.MustFromRuntime[*SerializerManager](runtimeInstance, ServiceSerializerManager)
}

func SerializerManagerFromRuntime(runtimeInstance runtimecontract.Runtime) *SerializerManager {
    serializerManagerInstance, err := runtime.FromRuntime[*SerializerManager](runtimeInstance, ServiceSerializerManager)
    if nil == serializerManagerInstance || nil != err {
        if nil != err {
            /* the failure is reported through the soft logger resolver: the Must variant panics when the logger itself cannot be resolved, and a runtime broken enough to lose the serializer manager is the runtime most likely to lose the logger with it — the reporting branch of a return-nil-on-failure resolver must not be the line that panics */
            logger := logging.LoggerFromRuntime(runtimeInstance)
            if nil != logger {
                logger.Error(
                    "failed to resolve the serializer manager",
                    exception.LogContext(err),
                )
            }
        }

        return nil
    }

    return serializerManagerInstance
}

func SerializerMustFromRuntime(runtimeInstance runtimecontract.Runtime) serializercontract.Serializer {
    return runtime.MustFromRuntime[serializercontract.Serializer](runtimeInstance, ServiceSerializer)
}

/* SerializerFromRuntime resolves the request serializer and answers nil when it cannot, with the failure logged through the soft logger resolver. The typed-nil branch is latent defense: the container refuses a provider-returned typed nil with an error today, so it is reachable only through a resolution path without that refusal — but a typed nil that slipped through would pass the plain comparison, look live to the result handler and dereference its nil receiver on the first Serialize of the request path. */
func SerializerFromRuntime(runtimeInstance runtimecontract.Runtime) serializercontract.Serializer {
    serializerInstance, err := runtime.FromRuntime[serializercontract.Serializer](runtimeInstance, ServiceSerializer)
    if nil == serializerInstance || true == internal.IsNilInterface(serializerInstance) || nil != err {
        if nil != err {
            logger := logging.LoggerFromRuntime(runtimeInstance)
            if nil != logger {
                logger.Error(
                    "failed to resolve the serializer",
                    exception.LogContext(err),
                )
            }
        }

        return nil
    }

    return serializerInstance
}
