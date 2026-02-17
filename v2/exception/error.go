package exception

import (
	exceptioncontract "github.com/precision-soft/melody/v2/exception/contract"
	loggingcontract "github.com/precision-soft/melody/v2/logging/contract"
)

type Error struct {
	message       string
	context       exceptioncontract.Context
	causeErr      error
	level         loggingcontract.Level
	alreadyLogged bool
}

func (instance *Error) Error() string {
	return instance.message
}

func (instance *Error) Unwrap() error {
	return instance.causeErr
}

func (instance *Error) Message() string {
	return instance.message
}

func (instance *Error) Context() exceptioncontract.Context {
	return copyStringMap(instance.context)
}

func (instance *Error) SetContext(context exceptioncontract.Context) {
	instance.context = copyStringMap(context)
}

func (instance *Error) SetContextValue(key string, value any) {
	instance.context[key] = value
}

func (instance *Error) CauseErr() error {
	return instance.causeErr
}

func (instance *Error) Level() loggingcontract.Level {
	return instance.level
}

func (instance *Error) AlreadyLogged() bool {
	return true == instance.alreadyLogged
}

func (instance *Error) MarkAsLogged() {
	instance.alreadyLogged = true
}

var _ exceptioncontract.ContextProvider = (*Error)(nil)
var _ exceptioncontract.AlreadyLogged = (*Error)(nil)
