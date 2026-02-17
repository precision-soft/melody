package container

import (
	"fmt"
	"reflect"
	"runtime"
	"runtime/debug"

	containercontract "github.com/precision-soft/melody/v2/container/contract"
	"github.com/precision-soft/melody/v2/exception"
	exceptioncontract "github.com/precision-soft/melody/v2/exception/contract"
	"github.com/precision-soft/melody/v2/internal"
)

type creationState struct {
	waitChannel     chan struct{}
	ownerContextId  uint64
	lastCreationErr error
}

type createWithGuardLookupFunc func() (any, bool)
type createWithGuardCreateFunc func(resolver containercontract.Resolver) (any, error, *providerDebugInfo)
type createWithGuardStoreFunc func(value any)

func (instance *container) serviceWithCreationGuardLocked(
	requestedKey string,
	creatingKey string,
	getCreatingState func() (*creationState, bool),
	setCreatingState func(state *creationState),
	clearCreatingState func(),
	lookup createWithGuardLookupFunc,
	create createWithGuardCreateFunc,
	store createWithGuardStoreFunc,
	resolver *resolverContext,
) (any, error) {
	value, exists := lookup()
	if true == exists {
		return value, nil
	}

	currentState, isBeingCreated := getCreatingState()
	if true == isBeingCreated {
		if nil == currentState || nil == currentState.waitChannel {
			return nil, exception.NewError(
				"service has invalid creation state",
				map[string]any{
					"creatingKey": creatingKey,
				},
				nil,
			)
		}

		registerResolverWaitLockedErr := instance.registerResolverWaitLocked(
			resolver.contextId,
			currentState.ownerContextId,
			creatingKey,
			resolver.stackStringWithRepeat(creatingKey),
		)
		if nil != registerResolverWaitLockedErr {
			return nil, registerResolverWaitLockedErr
		}

		instance.mutex.Unlock()
		<-currentState.waitChannel
		instance.mutex.Lock()

		instance.clearResolverWaitLocked(
			resolver.contextId,
			currentState.ownerContextId,
		)

		if nil != currentState.lastCreationErr {
			return nil, exception.NewError(
				"service creation failed",
				map[string]any{
					"creatingKey":       creatingKey,
					"ownerContextId":    currentState.ownerContextId,
					"resolverContextId": resolver.contextId,
				},
				currentState.lastCreationErr,
			)
		}

		value, exists = lookup()
		if false == exists {
			return nil, exception.NewError(
				"service was not available after creation finished",
				map[string]any{
					"name": creatingKey,
				},
				nil,
			)
		}

		return value, nil
	}

	newState := &creationState{
		waitChannel:     make(chan struct{}),
		ownerContextId:  resolver.contextId,
		lastCreationErr: nil,
	}

	setCreatingState(newState)

	instance.mutex.Unlock()

	createdValue, err, debugInfo := func() (createdValue any, err error, debugInfo *providerDebugInfo) {
		defer func() {
			recoveredValue := recover()
			if nil == recoveredValue {
				return
			}

			recoveredTypeString := fmt.Sprintf("%T", recoveredValue)
			recoveredValueString := fmt.Sprintf("%v", recoveredValue)

			var recoveredErr error
			recoveredErr, _ = recoveredValue.(error)

			context := exceptioncontract.Context{
				"requestedKey":   requestedKey,
				"creatingKey":    creatingKey,
				"recoveredType":  recoveredTypeString,
				"recoveredValue": recoveredValueString,
				"stack":          resolver.stackStringWithRepeat(creatingKey),
				"panicStack":     string(debug.Stack()),
			}

			if nil != recoveredErr {
				context["recoveredContext"] = exception.LogContext(recoveredErr)
			}

			err = exception.NewError(
				"service provider panicked",
				context,
				recoveredErr,
			)
		}()

		createdValue, err, debugInfo = create(resolver)
		if true == internal.IsNilInterface(createdValue) {
			return nil, exception.NewError(
				"service provider returned nil",
				exceptioncontract.Context{
					"requestedKey": requestedKey,
					"creatingKey":  creatingKey,
					"stack":        resolver.stackStringWithRepeat(creatingKey),
					"providerType": func() string {
						if nil != debugInfo && "" != debugInfo.providerTypeString {
							return debugInfo.providerTypeString
						}
						return reflect.TypeOf(create).String()
					}(),
					"providerFunc": func() string {
						if nil != debugInfo && "" != debugInfo.providerFunctionString {
							return debugInfo.providerFunctionString
						}

						createPointer := reflect.ValueOf(create).Pointer()
						if 0 == createPointer {
							return ""
						}

						createFunction := runtime.FuncForPC(createPointer)
						if nil == createFunction {
							return ""
						}

						return createFunction.Name()
					}(),
				},
				err,
			), nil
		}

		return createdValue, err, debugInfo
	}()

	instance.mutex.Lock()

	if nil == createdValue && nil == err {
		providerTypeString := ""
		providerFunctionString := ""

		if nil != debugInfo {
			providerTypeString = debugInfo.providerTypeString
			providerFunctionString = debugInfo.providerFunctionString
		}

		if "" == providerTypeString {
			providerTypeString = reflect.TypeOf(create).String()
		}

		if "" == providerFunctionString {
			createPointer := reflect.ValueOf(create).Pointer()
			if 0 != createPointer {
				createFunction := runtime.FuncForPC(createPointer)
				if nil != createFunction {
					providerFunctionString = createFunction.Name()
				}
			}
		}

		err = exception.NewError(
			"service provider returned nil for created value",
			exceptioncontract.Context{
				"requestedKey": requestedKey,
				"creatingKey":  creatingKey,
				"providerType": providerTypeString,
				"providerFunc": providerFunctionString,
				"stack":        resolver.stackStringWithRepeat(creatingKey),
			},
			nil,
		)
	}

	if nil == err {
		store(createdValue)
	}

	newState.lastCreationErr = err
	clearCreatingState()
	close(newState.waitChannel)

	if nil != err {
		return nil, err
	}

	return createdValue, nil
}

func (instance *container) registerResolverWaitLocked(
	fromContextId uint64,
	toContextId uint64,
	creatingKey string,
	stack string,
) error {
	if 0 == fromContextId || 0 == toContextId {
		return exception.NewError(
			"resolver context id is invalid",
			exceptioncontract.Context{
				"creatingKey":   creatingKey,
				"fromContextId": fromContextId,
				"toContextId":   toContextId,
				"resolverStack": stack,
			},
			nil,
		)
	}

	if fromContextId == toContextId {
		return exception.NewError(
			"circular service dependency detected",
			exceptioncontract.Context{
				"creatingKey":   creatingKey,
				"fromContextId": fromContextId,
				"toContextId":   toContextId,
				"resolverStack": stack,
			},
			nil,
		)
	}

	if true == instance.hasResolverPathLocked(toContextId, fromContextId) {
		return exception.NewError(
			"circular service dependency detected across concurrent resolutions",
			exceptioncontract.Context{
				"creatingKey":   creatingKey,
				"fromContextId": fromContextId,
				"toContextId":   toContextId,
				"resolverStack": stack,
			},
			nil,
		)
	}

	children, exists := instance.resolverWaitGraph[fromContextId]
	if false == exists || nil == children {
		children = make(map[uint64]struct{})
		instance.resolverWaitGraph[fromContextId] = children
	}

	children[toContextId] = struct{}{}

	return nil
}

func (instance *container) clearResolverWaitLocked(
	fromContextId uint64,
	toContextId uint64,
) {
	children, exists := instance.resolverWaitGraph[fromContextId]
	if false == exists || nil == children {
		return
	}

	delete(children, toContextId)
	if 0 == len(children) {
		delete(instance.resolverWaitGraph, fromContextId)
	}
}

func (instance *container) hasResolverPathLocked(
	startContextId uint64,
	targetContextId uint64,
) bool {
	if startContextId == targetContextId {
		return true
	}

	visited := make(map[uint64]struct{}, 8)
	work := make([]uint64, 0, 8)
	work = append(work, startContextId)

	for 0 < len(work) {
		current := work[len(work)-1]
		work = work[:len(work)-1]

		if _, exists := visited[current]; true == exists {
			continue
		}

		visited[current] = struct{}{}

		children, exists := instance.resolverWaitGraph[current]
		if false == exists || nil == children {
			continue
		}

		for child := range children {
			if child == targetContextId {
				return true
			}

			if _, alreadyVisited := visited[child]; false == alreadyVisited {
				work = append(work, child)
			}
		}
	}

	return false
}
