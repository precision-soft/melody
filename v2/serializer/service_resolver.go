package serializer

import (
    "github.com/precision-soft/melody/v2/exception"
    "github.com/precision-soft/melody/v2/internal"
    "github.com/precision-soft/melody/v2/logging"
    "github.com/precision-soft/melody/v2/runtime"
    runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
    serializercontract "github.com/precision-soft/melody/v2/serializer/contract"
)

/* ServiceSerializer is the default json serializer, registered behind a Has gate so an application may register its own first; SerializerMustFromRuntime and SerializerFromRuntime answer it. ServiceSerializerManager is what content negotiation reads, so the serializer under ServiceSerializer reaches a response only on the result handler's fallback path; a media type is added by registering a manager built by NewSerializerManager with the wider map. */
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
            /* reported through the soft logger resolver, so the reporting branch of a return-nil resolver cannot panic */
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

/* SerializerFromRuntime resolves the request serializer and answers nil when it cannot, logging the failure; a typed nil answers nil too. */
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
