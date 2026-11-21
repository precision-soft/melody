package httpclient

import (
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/exception"
    httpclientcontract "github.com/precision-soft/melody/v4/httpclient/contract"
    "github.com/precision-soft/melody/v4/internal"
)

/* Bound is an http client bound to one runtime: every request runs under its context. It is a value for one request, one run or one delivery, never kept in a service, a goroutine that outlives the runtime or a field: a service holds the client and binds it at the top of each method. */
type Bound struct {
    runtimeInstance containercontract.Runtime
    clientInstance  httpclientcontract.Client
}

/* For binds the client to the runtime; it allocates nothing and never panics, a nil of either being refused by the first door called. */
func For(runtimeInstance containercontract.Runtime, clientInstance httpclientcontract.Client) Bound {
    return Bound{
        runtimeInstance: runtimeInstance,
        clientInstance:  clientInstance,
    }
}

/* FromRuntime resolves the client the application registered under serviceName through the runtime's scope and binds it; the framework registers none, and a missing one panics as runtime.MustFromRuntime does. */
func FromRuntime(runtimeInstance containercontract.Runtime, serviceName string) Bound {
    return For(runtimeInstance, runtime.MustFromRuntime[httpclientcontract.Client](runtimeInstance, serviceName))
}

/* Runtime answers the runtime the view is bound to, for a callee that binds something else to it. */
func (instance Bound) Runtime() containercontract.Runtime {
    return instance.runtimeInstance
}

/* Client answers the unbound client, for a callee that binds it to another runtime. */
func (instance Bound) Client() httpclientcontract.Client {
    return instance.clientInstance
}

func (instance Bound) Get(urlString string, options RequestOptions) (httpclientcontract.Response, error) {
    if nilErr := instance.refuseNilClient(); nil != nilErr {
        return nil, nilErr
    }

    return instance.clientInstance.Get(instance.runtimeInstance, urlString, options)
}

func (instance Bound) Post(urlString string, body any, options RequestOptions) (httpclientcontract.Response, error) {
    if nilErr := instance.refuseNilClient(); nil != nilErr {
        return nil, nilErr
    }

    return instance.clientInstance.Post(instance.runtimeInstance, urlString, body, options)
}

func (instance Bound) Put(urlString string, body any, options RequestOptions) (httpclientcontract.Response, error) {
    if nilErr := instance.refuseNilClient(); nil != nilErr {
        return nil, nilErr
    }

    return instance.clientInstance.Put(instance.runtimeInstance, urlString, body, options)
}

func (instance Bound) Patch(urlString string, body any, options RequestOptions) (httpclientcontract.Response, error) {
    if nilErr := instance.refuseNilClient(); nil != nilErr {
        return nil, nilErr
    }

    return instance.clientInstance.Patch(instance.runtimeInstance, urlString, body, options)
}

func (instance Bound) Delete(urlString string, options RequestOptions) (httpclientcontract.Response, error) {
    if nilErr := instance.refuseNilClient(); nil != nilErr {
        return nil, nilErr
    }

    return instance.clientInstance.Delete(instance.runtimeInstance, urlString, options)
}

func (instance Bound) Request(method string, urlString string, options RequestOptions) (httpclientcontract.Response, error) {
    if nilErr := instance.refuseNilClient(); nil != nilErr {
        return nil, nilErr
    }

    return instance.clientInstance.Request(instance.runtimeInstance, method, urlString, options)
}

func (instance Bound) RequestStream(method string, urlString string, options RequestOptions) (httpclientcontract.StreamResponse, error) {
    if nilErr := instance.refuseNilClient(); nil != nilErr {
        return nil, nilErr
    }

    return instance.clientInstance.RequestStream(instance.runtimeInstance, method, urlString, options)
}

/* a typed-nil Client passes a plain comparison and panics on its first method call */
func (instance Bound) refuseNilClient() error {
    if true == internal.IsNilInterface(instance.clientInstance) {
        return exception.NewError("http client instance is nil", nil, nil)
    }

    return nil
}
