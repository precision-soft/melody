package serializer

import (
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/internal"
    "github.com/precision-soft/melody/v3/logging"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    serializercontract "github.com/precision-soft/melody/v3/serializer/contract"
)

/* ServiceSerializer supplies the default JSON serializer and may be registered before application boot. Content negotiation uses ServiceSerializerManager; register a manager with additional serializers to expose more media types. */
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

/* SerializerFromRuntime returns nil on resolution failure. Custom resolution paths may also return a typed nil. */
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
