package internal

import (
	loggingcontract "github.com/precision-soft/melody/logging/contract"
)

func NewEventListenerContext(
	eventName string,
	eventType string,
	listenerName string,
	listenerType string,
	listenerPriority int,
	durationMs int64,
) loggingcontract.Context {
	return loggingcontract.Context{
		"eventName":        eventName,
		"eventType":        eventType,
		"listenerName":     listenerName,
		"listenerType":     listenerType,
		"listenerPriority": listenerPriority,
		"durationMs":       durationMs,
	}
}

func NewEventListenerPanicContext(
	baseContext loggingcontract.Context,
	panicValue any,
	panicType string,
	panicStack string,
) loggingcontract.Context {
	context := CopyStringMap[any](baseContext)

	context["panicValue"] = panicValue
	context["panicType"] = panicType
	context["panicStack"] = panicStack

	return context
}
