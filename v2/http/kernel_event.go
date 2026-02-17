package http

import (
	httpcontract "github.com/precision-soft/melody/v2/http/contract"
	runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
)

func NewKernelRequestEvent(
	runtimeInstance runtimecontract.Runtime,
	request httpcontract.Request,
) *KernelRequestEvent {
	return &KernelRequestEvent{
		runtimeInstance: runtimeInstance,
		request:         request,
		response:        nil,
	}
}

type KernelRequestEvent struct {
	runtimeInstance runtimecontract.Runtime
	request         httpcontract.Request
	response        httpcontract.Response
}

func (instance *KernelRequestEvent) Runtime() runtimecontract.Runtime {
	return instance.runtimeInstance
}

func (instance *KernelRequestEvent) Request() httpcontract.Request {
	return instance.request
}

func (instance *KernelRequestEvent) Response() httpcontract.Response {
	return instance.response
}

func (instance *KernelRequestEvent) SetResponse(response httpcontract.Response) {
	instance.response = response
}

func NewKernelControllerEvent(
	runtimeInstance runtimecontract.Runtime,
	request httpcontract.Request,
) *KernelControllerEvent {
	return &KernelControllerEvent{
		runtime:  runtimeInstance,
		request:  request,
		response: nil,
	}
}

type KernelControllerEvent struct {
	runtime  runtimecontract.Runtime
	request  httpcontract.Request
	response httpcontract.Response
}

func (instance *KernelControllerEvent) Runtime() runtimecontract.Runtime {
	return instance.runtime
}

func (instance *KernelControllerEvent) Request() httpcontract.Request {
	return instance.request
}

func (instance *KernelControllerEvent) Response() httpcontract.Response {
	return instance.response
}

func (instance *KernelControllerEvent) SetResponse(response httpcontract.Response) {
	instance.response = response
}

func NewKernelResponseEvent(request httpcontract.Request, response httpcontract.Response) *KernelResponseEvent {
	return &KernelResponseEvent{
		request:  request,
		response: response,
	}
}

type KernelResponseEvent struct {
	request  httpcontract.Request
	response httpcontract.Response
}

func (instance *KernelResponseEvent) Request() httpcontract.Request {
	return instance.request
}

func (instance *KernelResponseEvent) Response() httpcontract.Response {
	return instance.response
}

func (instance *KernelResponseEvent) SetResponse(response httpcontract.Response) {
	instance.response = response
}

func NewKernelTerminateEvent(
	runtimeInstance runtimecontract.Runtime,
	request httpcontract.Request,
	response httpcontract.Response,
) *KernelTerminateEvent {
	return &KernelTerminateEvent{
		runtime:  runtimeInstance,
		request:  request,
		response: response,
	}
}

type KernelTerminateEvent struct {
	runtime  runtimecontract.Runtime
	request  httpcontract.Request
	response httpcontract.Response
}

func (instance *KernelTerminateEvent) Runtime() runtimecontract.Runtime {
	return instance.runtime
}

func (instance *KernelTerminateEvent) Request() httpcontract.Request {
	return instance.request
}

func (instance *KernelTerminateEvent) Response() httpcontract.Response {
	return instance.response
}

func NewKernelExceptionEvent(
	runtimeInstance runtimecontract.Runtime,
	request httpcontract.Request,
	err error,
) *KernelExceptionEvent {
	return &KernelExceptionEvent{
		runtime:  runtimeInstance,
		request:  request,
		err:      err,
		response: nil,
	}
}

type KernelExceptionEvent struct {
	runtime  runtimecontract.Runtime
	request  httpcontract.Request
	err      error
	response httpcontract.Response
}

func (instance *KernelExceptionEvent) Runtime() runtimecontract.Runtime {
	return instance.runtime
}

func (instance *KernelExceptionEvent) Request() httpcontract.Request {
	return instance.request
}

func (instance *KernelExceptionEvent) Err() error {
	return instance.err
}

func (instance *KernelExceptionEvent) Response() httpcontract.Response {
	return instance.response
}

func (instance *KernelExceptionEvent) SetResponse(response httpcontract.Response) {
	instance.response = response
}
