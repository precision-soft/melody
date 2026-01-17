package subscriber

import (
	"time"

	melodyevent "github.com/precision-soft/melody/event"
	melodyeventcontract "github.com/precision-soft/melody/event/contract"
	melodylogging "github.com/precision-soft/melody/logging"
	loggingcontract "github.com/precision-soft/melody/logging/contract"
	melodyruntimecontract "github.com/precision-soft/melody/runtime/contract"
	melodysecurity "github.com/precision-soft/melody/security"
	melodysecuritycontract "github.com/precision-soft/melody/security/contract"
)

func NewSecurityAuthenticationEventSubscriber() *SecurityAuthenticationEventSubscriber {
	return &SecurityAuthenticationEventSubscriber{}
}

type SecurityAuthenticationEventSubscriber struct{}

func (instance *SecurityAuthenticationEventSubscriber) SubscribedEvents() map[string][]melodyeventcontract.SubscribedEvent {
	return map[string][]melodyeventcontract.SubscribedEvent{
		melodysecuritycontract.EventSecurityLoginSuccess: {
			melodyevent.NewSubscribedEvent(instance.onLoginSuccess(), 0),
		},
		melodysecuritycontract.EventSecurityLoginFailure: {
			melodyevent.NewSubscribedEvent(instance.onLoginFailure(), 0),
		},
	}
}

func (instance *SecurityAuthenticationEventSubscriber) onLoginSuccess() melodyeventcontract.EventListener {
	return func(runtimeInstance melodyruntimecontract.Runtime, eventValue melodyeventcontract.Event) error {
		payloadValue := eventValue.Payload()
		payloadInstance, ok := payloadValue.(*melodysecurity.LoginSuccessEvent)
		if false == ok {
			return nil
		}
		if nil == payloadInstance {
			return nil
		}

		requestInstance := payloadInstance.Request()
		path := ""
		method := ""
		if nil != requestInstance && nil != requestInstance.HttpRequest() && nil != requestInstance.HttpRequest().URL {
			path = requestInstance.HttpRequest().URL.Path
			method = requestInstance.HttpRequest().Method
		}

		token := payloadInstance.Token()
		userIdentifier := ""
		roles := []string{}
		if nil != token {
			userIdentifier = token.UserIdentifier()
			roles = token.Roles()
		}

		logger := melodylogging.LoggerMustFromRuntime(runtimeInstance)
		logger.Info(
			"security login success",
			loggingcontract.Context{
				"method":         method,
				"path":           path,
				"userIdentifier": userIdentifier,
				"roles":          roles,
				"occurredAt":     time.Now().UTC().Format(time.RFC3339),
			},
		)

		return nil
	}
}

func (instance *SecurityAuthenticationEventSubscriber) onLoginFailure() melodyeventcontract.EventListener {
	return func(runtimeInstance melodyruntimecontract.Runtime, eventValue melodyeventcontract.Event) error {
		payloadValue := eventValue.Payload()
		payloadInstance, ok := payloadValue.(*melodysecurity.LoginFailureEvent)
		if false == ok {
			return nil
		}
		if nil == payloadInstance {
			return nil
		}

		requestInstance := payloadInstance.Request()
		path := ""
		method := ""
		if nil != requestInstance && nil != requestInstance.HttpRequest() && nil != requestInstance.HttpRequest().URL {
			path = requestInstance.HttpRequest().URL.Path
			method = requestInstance.HttpRequest().Method
		}

		payloadErr := payloadInstance.Error()
		errString := ""
		if nil != payloadErr {
			errString = payloadErr.Error()
		}

		logger := melodylogging.LoggerMustFromRuntime(runtimeInstance)
		logger.Info(
			"security login failure",
			loggingcontract.Context{
				"method":     method,
				"path":       path,
				"error":      errString,
				"occurredAt": time.Now().UTC().Format(time.RFC3339),
			},
		)

		return nil
	}
}

var _ melodyeventcontract.EventSubscriber = (*SecurityAuthenticationEventSubscriber)(nil)
