package container

import (
    "fmt"
    "reflect"
    goruntime "runtime"
    "sort"
    "strings"

    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/internal"
)

/* site and caller are the first two frames outside this package, so a handle built through a framework helper names the helper's line and its caller */
type lazyHandleRecord struct {
    serviceName string
    serviceType reflect.Type
    handleType  reflect.Type
    site        string
    caller      string
}

/* a resolver of another implementation carries no such door and records nothing */
type lazyHandleRecorder interface {
    recordLazyHandle(record lazyHandleRecord)
}

func recordLazyHandleOf(resolver any, serviceName string, serviceType reflect.Type, handleType reflect.Type) {
    if true == internal.IsNilInterface(resolver) {
        return
    }

    recorder, records := resolver.(lazyHandleRecorder)
    if false == records {
        return
    }

    site, caller := lazyHandleSite()

    recorder.recordLazyHandle(
        lazyHandleRecord{
            serviceName: serviceName,
            serviceType: serviceType,
            handleType:  handleType,
            site:        site,
            caller:      caller,
        },
    )
}

/* a lock of its own: a handle is built inside providers, where the container's lock may be held */
func (instance *container) recordLazyHandle(record lazyHandleRecord) {
    instance.lazyHandleMutex.Lock()
    defer instance.lazyHandleMutex.Unlock()

    if true == instance.lazyHandlesVerified {
        return
    }

    if nil == instance.lazyHandles {
        instance.lazyHandles = make(map[lazyHandleRecord]struct{})
    }

    instance.lazyHandles[record] = struct{}{}
}

/* a view that sees a scope resolves per request, so its handles are not verified */
func (instance *resolverContext) recordLazyHandle(record lazyHandleRecord) {
    if false == instance.readsContainer() {
        return
    }

    instance.containerInstance.recordLazyHandle(record)
}

/* VerifyLazyNames refuses the first Lazy or LazyByType handle built over the container, or over a provider's view reading it, whose service no registration answers: a name nothing registered, a scoped name, a type nothing registered, a type more than one service is registered under, or a service whose built value or declared concrete type is not the handle's type. A registration never built is admitted. The first call ends the recording, so a handle built afterwards, at request time or inside a provider, is never verified; a handle over a scope or over a resolver of another implementation is never recorded. It runs under either teardown mode; a closed container refuses it. */
func (instance *container) VerifyLazyNames() error {
    if true == instance.IsClosed() {
        return exception.NewError("container is closed", exceptioncontract.Context{"door": "VerifyLazyNames"}, ErrContainerClosed)
    }

    instance.lazyHandleMutex.Lock()
    instance.lazyHandlesVerified = true
    records := make([]lazyHandleRecord, 0, len(instance.lazyHandles))
    for record := range instance.lazyHandles {
        records = append(records, record)
    }
    instance.lazyHandles = nil
    instance.lazyHandleMutex.Unlock()

    sort.Slice(
        records,
        func(left int, right int) bool {
            return lazyHandleSortKey(records[left]) < lazyHandleSortKey(records[right])
        },
    )

    for _, record := range records {
        if refusal := instance.refuseLazyHandle(record); nil != refusal {
            return refusal
        }
    }

    return nil
}

func (instance *container) refuseLazyHandle(record lazyHandleRecord) error {
    if nil == record.serviceType {
        if true == instance.Has(record.serviceName) {
            return instance.refuseLazyHandleTypeMismatch(record, record.serviceName)
        }

        return exception.NewError(
            "a container.Lazy handle names a service nothing registered in the container",
            exceptioncontract.Context{"serviceName": record.serviceName, "site": record.site, "caller": record.caller},
            ErrLazyHandleNameNotRegistered,
        )
    }

    canonicalType := canonicalServiceType(record.serviceType)

    instance.mutex.RLock()
    registeredServiceNames := append([]string(nil), instance.typeRegistrationNamesByType[canonicalType]...)
    instance.mutex.RUnlock()

    if 1 < len(registeredServiceNames) {
        sort.Strings(registeredServiceNames)

        return exception.NewError(
            "a container.LazyByType handle names a type more than one service is registered under",
            exceptioncontract.Context{"type": canonicalType.String(), "conflicts": registeredServiceNames, "site": record.site, "caller": record.caller},
            ErrLazyHandleTypeIsAmbiguous,
        )
    }

    if true == instance.HasType(record.serviceType) {
        if 1 == len(registeredServiceNames) {
            return instance.refuseLazyHandleTypeMismatch(record, registeredServiceNames[0])
        }

        return nil
    }

    return exception.NewError(
        "a container.LazyByType handle names a type nothing registered in the container",
        exceptioncontract.Context{"type": canonicalType.String(), "site": record.site, "caller": record.caller},
        ErrLazyHandleNameNotRegistered,
    )
}

/* a provider declaring an interface is admitted: only the value it returns can answer the handle's assertion */
func (instance *container) refuseLazyHandleTypeMismatch(record lazyHandleRecord, serviceName string) error {
    instance.mutex.RLock()
    knownType := instance.providerServiceTypeByName[serviceName]
    if builtValue, isBuilt := instance.instances[serviceName]; true == isBuilt && nil != builtValue {
        knownType = reflect.TypeOf(builtValue)
    }
    instance.mutex.RUnlock()

    if nil == knownType || nil == record.handleType || reflect.Interface == knownType.Kind() || true == knownType.AssignableTo(record.handleType) {
        return nil
    }

    return exception.NewError(
        "a container.Lazy handle asserts a type its service is not",
        exceptioncontract.Context{"serviceName": serviceName, "serviceType": knownType.String(), "handleType": record.handleType.String(), "site": record.site, "caller": record.caller},
        ErrLazyHandleTypeMismatch,
    )
}

func lazyHandleSortKey(record lazyHandleRecord) string {
    typeName := ""
    if nil != record.serviceType {
        typeName = record.serviceType.String()
    }

    return record.site + "\x00" + record.caller + "\x00" + record.serviceName + "\x00" + typeName
}

const lazyHandlePackagePrefix = "github.com/precision-soft/melody/v4/container."

/* this package's tests count as callers */
func lazyHandleSite() (string, string) {
    programCounters := make([]uintptr, 32)
    frameCount := goruntime.Callers(2, programCounters)
    frames := goruntime.CallersFrames(programCounters[:frameCount])

    found := make([]string, 0, 2)

    for {
        frame, more := frames.Next()
        if "" == frame.File {
            break
        }

        insidePackage := true == strings.HasPrefix(frame.Function, lazyHandlePackagePrefix) && false == strings.HasSuffix(frame.File, "_test.go")
        if false == insidePackage {
            found = append(found, fmt.Sprintf("%s:%d", frame.File, frame.Line))
            if 2 == len(found) {
                break
            }
        }

        if false == more {
            break
        }
    }

    for 2 > len(found) {
        found = append(found, "unknown")
    }

    return found[0], found[1]
}
