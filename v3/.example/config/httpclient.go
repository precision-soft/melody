package config

import (
    "time"

    "github.com/precision-soft/melody/v3/.example/service"
    melodyapplicationcontract "github.com/precision-soft/melody/v3/application/contract"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/httpclient"
)

const outboundRequestTimeout = 3 * time.Second

func (instance *Module) registerRatesHttpClientService(registrar melodyapplicationcontract.ServiceRegistrar) {
    baseUrl := instance.environmentValue(parameterRatesBaseUrl)
    if "" == baseUrl {
        return
    }

    registrar.RegisterService(
        service.ServiceRatesHttpClient,
        func(resolver melodycontainercontract.Resolver) (*httpclient.HttpClient, error) {

            return httpclient.NewHttpClient(
                httpclient.NewHttpClientConfig(
                    baseUrl,
                    outboundRequestTimeout,
                    map[string]string{
                        "accept": "application/json",
                    },
                ),
            ), nil
        },
        outboundClientRegisterOptions()...,
    )
}

func (instance *Module) registerReportExportHttpClientService(registrar melodyapplicationcontract.ServiceRegistrar) {
    if "" == instance.environmentValue(parameterReportExportEndpoint) {
        return
    }

    registrar.RegisterService(
        service.ServiceReportExportHttpClient,
        func(resolver melodycontainercontract.Resolver) (*httpclient.HttpClient, error) {
            return httpclient.NewHttpClient(
                httpclient.NewHttpClientConfig(
                    "",
                    outboundRequestTimeout,
                    map[string]string{
                        "accept": "application/json",
                    },
                ).WithoutRedirects(),
            ), nil
        },
        outboundClientRegisterOptions()...,
    )
}

func outboundClientRegisterOptions() []melodycontainercontract.RegisterOption {
    return []melodycontainercontract.RegisterOption{
        melodycontainer.WithoutTypeRegistration(),
    }
}
