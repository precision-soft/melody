package serializer

import (
    "github.com/precision-soft/melody/v2/exception"
    "github.com/precision-soft/melody/v2/logging"
    "github.com/precision-soft/melody/v2/runtime"
    runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
    serializercontract "github.com/precision-soft/melody/v2/serializer/contract"
)

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
            logging.LoggerMustFromRuntime(runtimeInstance).Error(
                "failed to resolve the serializer manager",
                exception.LogContext(err),
            )
        }

        return nil
    }

    return serializerManagerInstance
}

func SerializerMustFromRuntime(runtimeInstance runtimecontract.Runtime) serializercontract.Serializer {
    return runtime.MustFromRuntime[serializercontract.Serializer](runtimeInstance, ServiceSerializer)
}

func SerializerFromRuntime(runtimeInstance runtimecontract.Runtime) serializercontract.Serializer {
    serializerInstance, err := runtime.FromRuntime[serializercontract.Serializer](runtimeInstance, ServiceSerializer)
    if nil == serializerInstance || nil != err {
        if nil != err {
            logging.LoggerMustFromRuntime(runtimeInstance).Error(
                "failed to resolve the serializer",
                exception.LogContext(err),
            )
        }

        return nil
    }

    return serializerInstance
}
