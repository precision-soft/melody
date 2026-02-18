package exception

import (
	"errors"

	exceptioncontract "github.com/precision-soft/melody/v2/exception/contract"
	loggingcontract "github.com/precision-soft/melody/v2/logging/contract"
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

	var exceptionValue *Error
	if true == errors.As(err, &exceptionValue) && nil != exceptionValue {
		causeErr := exceptionValue.CauseErr()
		if nil != causeErr {
			_, hasCause := context["cause"]
			_, hasCauseChain := context["causeChain"]

			if false == hasCause || false == hasCauseChain {
				causeChain := buildCauseChain(causeErr, 8)
				if 0 < len(causeChain) {
					if false == hasCause {
						context["cause"] = causeChain[0]
					}
					if false == hasCauseChain {
						context["causeChain"] = causeChain
					}
				} else if false == hasCause {
					context["cause"] = causeErr.Error()
				}
			}

			_, hasCauseContextChain := context["causeContextChain"]
			if false == hasCauseContextChain {
				causeContextChain := buildCauseContextChain(causeErr, 8)
				if 0 < len(causeContextChain) {
					context["causeContextChain"] = causeContextChain
				}
			}
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

func FromErrorWithLevelAndContext(err error, level loggingcontract.Level, context exceptioncontract.Context) *Error {
	if nil == err {
		return nil
	}

	mergedContext := make(exceptioncontract.Context)

	var provider exceptioncontract.ContextProvider
	if true == errors.As(err, &provider) {
		for key, value := range provider.Context() {
			mergedContext[key] = value
		}
	}

	for key, value := range context {
		mergedContext[key] = value
	}

	return newWithLevel(err.Error(), mergedContext, err, level)
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

func buildCauseChain(causeErr error, maxDepth int) []string {
	if nil == causeErr {
		return nil
	}

	if 0 >= maxDepth {
		return []string{causeErr.Error()}
	}

	chain := make([]string, 0, maxDepth)

	current := causeErr
	for depth := 0; depth < maxDepth && nil != current; depth++ {
		chain = append(chain, current.Error())
		current = errors.Unwrap(current)
	}

	return chain
}

func buildCauseContextChain(causeErr error, maxDepth int) []map[string]any {
	if nil == causeErr {
		return nil
	}

	if 0 >= maxDepth {
		maxDepth = 1
	}

	chain := make([]map[string]any, 0, maxDepth)
	hasAnyContext := false

	current := causeErr
	for depth := 0; depth < maxDepth && nil != current; depth++ {
		var causeException *Error
		if true == errors.As(current, &causeException) {
			causeContext := causeException.Context()
			if nil != causeContext && 0 < len(causeContext) {
				chain = append(chain, causeContext)
				hasAnyContext = true
			} else {
				chain = append(chain, nil)
			}
		} else {
			chain = append(chain, nil)
		}

		current = errors.Unwrap(current)
	}

	if false == hasAnyContext {
		return nil
	}

	return chain
}
