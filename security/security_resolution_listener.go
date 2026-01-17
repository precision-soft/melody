package security

import (
	eventcontract "github.com/precision-soft/melody/event/contract"
	"github.com/precision-soft/melody/exception"
	"github.com/precision-soft/melody/http"
	kernelcontract "github.com/precision-soft/melody/kernel/contract"
	runtimecontract "github.com/precision-soft/melody/runtime/contract"
)

func RegisterKernelSecurityResolutionListener(kernelInstance kernelcontract.Kernel, registry *FirewallRegistry) {
	if nil == registry {
		exception.Panic(exception.NewError("firewall registry is nil for security resolution listener", nil, nil))
	}

	eventDispatcher := kernelInstance.EventDispatcher()

	eventDispatcher.AddListener(
		kernelcontract.EventKernelRequest,
		func(runtimeInstance runtimecontract.Runtime, eventValue eventcontract.Event) error {
			requestEvent, ok := eventValue.Payload().(*http.KernelRequestEvent)
			if false == ok {
				return nil
			}

			if nil == requestEvent || nil == requestEvent.Request() {
				return nil
			}

			if nil != requestEvent.Response() {
				return nil
			}

			firewall, matched := registry.Match(requestEvent.Request())
			if false == matched || nil == firewall {
				return nil
			}

			firewallRules := firewall.Rules()
			if 0 != len(firewallRules) {
				firewallInstance := NewFirewall(firewallRules...)
				err := firewallInstance.Check(requestEvent.Request())
				if nil != err {
					exceptionEvent := http.NewKernelExceptionEvent(runtimeInstance, requestEvent.Request(), err)

					_, err = eventDispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelException, exceptionEvent)
					if nil != err {
						return err
					}

					requestEvent.SetResponse(exceptionEvent.Response())
					return nil
				}
			}

			token, err := firewall.TokenSource().Resolve(runtimeInstance, requestEvent.Request())
			if nil != err {
				exceptionEvent := http.NewKernelExceptionEvent(runtimeInstance, requestEvent.Request(), err)

				_, err = eventDispatcher.DispatchName(runtimeInstance, kernelcontract.EventKernelException, exceptionEvent)
				if nil != err {
					return err
				}

				requestEvent.SetResponse(exceptionEvent.Response())
				return nil
			}

			securityContext := NewSecurityContext(firewall, token)

			SecurityContextSetOnRuntime(runtimeInstance, securityContext)

			return nil
		},
		KernelFirewallListenerPriority,
	)
}
