package exception

import (
	"errors"

	exceptioncontract "github.com/precision-soft/melody/exception/contract"
	loggingcontract "github.com/precision-soft/melody/logging/contract"
)

func LogContext(err error, extra ...exceptioncontract.Context) exceptioncontract.Context {
	if nil == err {
		if 0 == len(extra) || nil == extra[0] {
			return nil
		}

		mergedContext := make(exceptioncontract.Context, len(extra[0]))
		for key, value := range extra[0] {
			mergedContext[key] = value
		}

		return mergedContext
	}

	context := exceptioncontract.Context{
		"error": err.Error(),
	}

	var provider exceptioncontract.ContextProvider
	if true == errors.As(err, &provider) {
		errorContext := provider.Context()
		for key, value := range errorContext {
			if "error" == key {
				continue
			}

			context[key] = value
		}
	}

	if 0 == len(extra) || nil == extra[0] {
		return context
	}

	for key, value := range extra[0] {
		context[key] = value
	}

	return context
}

func FromError(err error) *Error {
	if nil == err {
		return nil
	}

	exceptionError, ok := err.(*Error)
	if true == ok {
		return exceptionError
	}

	var context exceptioncontract.Context

	var provider exceptioncontract.ContextProvider
	if true == errors.As(err, &provider) {
		context = provider.Context()
	}

	return NewError(err.Error(), context, err)
}

func FromErrorWithLevel(err error, level loggingcontract.Level) *Error {
	if nil == err {
		return nil
	}

	var context exceptioncontract.Context

	var provider exceptioncontract.ContextProvider
	if true == errors.As(err, &provider) {
		context = provider.Context()
	}

	return newWithLevel(err.Error(), context, err, level)
}

func MarkLogged(err error) error {
	if nil == err {
		return nil
	}

	exceptionErr, ok := err.(exceptioncontract.AlreadyLogged)
	if true == ok && nil != exceptionErr {
		exceptionErr.MarkAsLogged()
	}

	return err
}

func copyStringMap[T any](input map[string]T) map[string]T {
	if nil == input {
		return make(map[string]T)
	}

	copied := make(map[string]T, len(input))

	for key, value := range input {
		copied[key] = value
	}

	return copied
}
