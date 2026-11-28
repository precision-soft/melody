package config

import (
    "testing"
)

func TestHttpKernelConfig_CarriesTheNotFoundPageAndTheForwardedList(t *testing.T) {
    kernelConfig := HttpKernelConfig()

    if nil == kernelConfig.NotFoundHandler {
        t.Fatalf("expected the example's not found page")
    }

    if false == kernelConfig.ForwardedHeadersPolicy.TrustForwardedHeaders || len(exampleForwardedHeadersPolicy().TrustedProxyList) != len(kernelConfig.ForwardedHeadersPolicy.TrustedProxyList) {
        t.Fatalf("expected the forwarded list, got %+v", kernelConfig.ForwardedHeadersPolicy)
    }

    if 0 != len(kernelConfig.Middlewares) {
        t.Fatalf("expected no middleware, which the application refuses in its kernel config")
    }
}
