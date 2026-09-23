package config

import (
    "fmt"
    "strings"
    "testing"
)

/* recordingParameterRegistrar keeps what RegisterParameters declared and marked, which is the whole question here: the marks are what debug:parameters redacts by. */
type recordingParameterRegistrar struct {
    registered map[string]any
    marked     []string
}

func newRecordingParameterRegistrar() *recordingParameterRegistrar {
    return &recordingParameterRegistrar{registered: map[string]any{}}
}

func (instance *recordingParameterRegistrar) RegisterParameter(name string, value any) {
    instance.registered[name] = value
}

func (instance *recordingParameterRegistrar) RegisterSecretParameter(name string, value any) {
    instance.registered[name] = value
    instance.marked = append(instance.marked, name)
}

func (instance *recordingParameterRegistrar) MarkParameterSecret(name string) {
    instance.marked = append(instance.marked, name)
}

func (instance *recordingParameterRegistrar) isMarked(name string) bool {
    for _, marked := range instance.marked {
        if name == marked {
            return true
        }
    }

    return false
}

/* the two outbound urls can carry a credential in their userinfo — the shape the amqp dsn is marked for — and then debug:parameters printed it in clear, twice: under the key and under the parameter that reads it. Marked as written, the mark propagates to that parameter through its template. */
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

/* the sister case, so the mark is a measurement of the value and not a blanket: the shipped urls carry no credential, and an operator reads them in debug:parameters to see where the process points. */
func TestRegisterParameters_LeavesAnOutboundUrlWithoutAUserinfoReadable(t *testing.T) {
    registrar := newRecordingParameterRegistrar()

    moduleWithEnvironment(t, map[string]string{
        environmentKeyRatesBaseUrl:         "http://rates.melody.localhost.precision-soft.com/v1/",
        environmentKeyReportExportEndpoint: "http://rates.melody.localhost.precision-soft.com/v1/report-sink",
        "MYSQL_PASSWORD":                   "melody",
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

/* the integration keys are switches the readme says to remove — the line gone, not blank — to boot the in-process fallbacks, and a parameter that read one of them in its template without a default failed the boot's resolution the moment the line was gone: the database dsn did exactly that, over a value nothing in the application consumed. No template may read them. */
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

/* "EUR by default, to match the seed" is a template, not a sentence: the default parameter holds the seed's
   base and the base parameter reads it through the env processor's default clause under RATES_BASE_CURRENCY */
func TestRegisterParameters_DefaultsTheRatesBaseToTheSeeds(t *testing.T) {
    registrar := newRecordingParameterRegistrar()

    moduleWithEnvironment(t, map[string]string{}).RegisterParameters(registrar)

    if "EUR" != fmt.Sprint(registrar.registered[parameterRatesDefaultBaseCurrency]) {
        t.Fatalf("the default base is %q, wanted the seed's EUR", fmt.Sprint(registrar.registered[parameterRatesDefaultBaseCurrency]))
    }

    if "%env(default:"+parameterRatesDefaultBaseCurrency+":RATES_BASE_CURRENCY)%" != fmt.Sprint(registrar.registered[parameterRatesBaseCurrency]) {
        t.Fatalf("the base parameter reads %q, wanted the env key with the seed's default", fmt.Sprint(registrar.registered[parameterRatesBaseCurrency]))
    }
}

/* a credential key the environment does not define is not marked: the integration blocks are removed to boot the
   fallbacks, and a mark that matched no parameter warned at every boot of a mysql-only checkout. A key present —
   blank included, which melody still registers as a parameter — is marked */
func TestRegisterParameters_MarksACredentialOnlyWhereTheEnvironmentDefinesIt(t *testing.T) {
    mysqlOnly := newRecordingParameterRegistrar()
    moduleWithEnvironment(t, map[string]string{"MYSQL_PASSWORD": "melody"}).RegisterParameters(mysqlOnly)

    if false == mysqlOnly.isMarked("MYSQL_PASSWORD") {
        t.Fatalf("expected the defined credential marked, marked: %v", mysqlOnly.marked)
    }

    for _, absentKey := range []string{environmentKeyPgsqlPassword, "S3_SECRET_KEY", environmentKeyAmqpDsn} {
        if true == mysqlOnly.isMarked(absentKey) {
            t.Errorf("expected %s left unmarked where the environment does not define it, marked: %v", absentKey, mysqlOnly.marked)
        }
    }

    everyBlock := newRecordingParameterRegistrar()
    moduleWithEnvironment(t, map[string]string{
        "MYSQL_PASSWORD":            "melody",
        environmentKeyPgsqlPassword: "",
        "S3_SECRET_KEY":             "secret",
        environmentKeyAmqpDsn:       "amqp://guest:guest@rabbitmq:5672/",
    }).RegisterParameters(everyBlock)

    for _, definedKey := range []string{"MYSQL_PASSWORD", environmentKeyPgsqlPassword, "S3_SECRET_KEY", environmentKeyAmqpDsn} {
        if false == everyBlock.isMarked(definedKey) {
            t.Errorf("expected %s marked where the environment defines it, marked: %v", definedKey, everyBlock.marked)
        }
    }
}
