package config

import (
    "fmt"
    "strings"
    "testing"
)

func TestRegisterParameters_MarksAnOutboundUrlSecretWhenItCarriesAUserinfo(t *testing.T) {
    registrar := newRecordingParameterRegistrar()

    moduleWithEnvironment(t, map[string]string{
        environmentKeyRatesBaseUrl:         "http://rates:ratespass@rates.example.test/v1/",
        environmentKeyReportExportEndpoint: "http://sink:sinkpass@sink.example.test/v1/report-sink",
    }).RegisterParameters(registrar)

    for _, key := range []string{environmentKeyRatesBaseUrl, environmentKeyReportExportEndpoint} {
        if false == registrar.isMarked(key) {
            t.Fatalf("expected %s to be marked secret when it carries a userinfo, marked: %v", key, registrar.marked)
        }
    }
}

func TestRegisterParameters_LeavesAnOutboundUrlWithoutAUserinfoReadable(t *testing.T) {
    registrar := newRecordingParameterRegistrar()

    moduleWithEnvironment(t, map[string]string{
        environmentKeyRatesBaseUrl:         "http://rates.melody.localhost.precision-soft.com/v1/",
        environmentKeyReportExportEndpoint: "http://rates.melody.localhost.precision-soft.com/v1/report-sink",
    }).RegisterParameters(registrar)

    for _, key := range []string{environmentKeyRatesBaseUrl, environmentKeyReportExportEndpoint} {
        if true == registrar.isMarked(key) {
            t.Fatalf("expected %s to stay readable without a userinfo, marked: %v", key, registrar.marked)
        }
    }

    if false == registrar.isMarked("MYSQL_PASSWORD") {
        t.Fatalf("expected the credentials to stay marked, marked: %v", registrar.marked)
    }
}

func TestRegisterParameters_NoTemplateReadsARemovableIntegrationKey(t *testing.T) {
    registrar := newRecordingParameterRegistrar()

    moduleWithEnvironment(t, map[string]string{}).RegisterParameters(registrar)

    if 0 == len(registrar.registered) {
        t.Fatalf("expected the module to register its parameters")
    }

    for name, value := range registrar.registered {
        template := fmt.Sprint(value)

        for _, integrationKeyPrefix := range []string{"MYSQL_", "PGSQL_"} {
            if true == strings.Contains(template, integrationKeyPrefix) {
                t.Fatalf("expected no template to read a %s key, %s reads %q", integrationKeyPrefix, name, template)
            }
        }
    }
}
