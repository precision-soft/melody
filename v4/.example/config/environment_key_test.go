package config

import (
    "bufio"
    "context"
    "io/fs"
    "os"
    "path/filepath"
    "regexp"
    "strings"
    "testing"
    "testing/fstest"

    melodyconfig "github.com/precision-soft/melody/v4/config"
)

func exampleConfiguration(t *testing.T, values map[string]string) *melodyconfig.Configuration {
    t.Helper()

    environment, environmentErr := melodyconfig.NewEnvironment(context.Background(), &stubEnvironmentSource{values: values})
    if nil != environmentErr {
        t.Fatalf("new environment: %v", environmentErr)
    }

    configuration, configurationErr := melodyconfig.NewConfiguration(
        environment,
        "/tmp/melody",
        melodyconfig.ConfigurationConfig{EnvironmentKeys: EnvironmentKeys(), ParameterOverrides: ParameterOverrides()},
    )
    if nil != configurationErr {
        t.Fatalf("new configuration: %v", configurationErr)
    }

    return configuration
}

func TestExampleKeys_EveryEnvLineOutsideMelodyIsDeclared(t *testing.T) {
    declared := map[string]bool{}
    for _, key := range EnvironmentKeys() {
        declared[key.Key] = true
    }

    file, openErr := os.Open(filepath.Join("..", ".env"))
    if nil != openErr {
        t.Fatalf("open .env: %v", openErr)
    }
    defer file.Close()

    scanner := bufio.NewScanner(file)
    for scanner.Scan() {
        line := strings.TrimSpace(scanner.Text())
        if "" == line || true == strings.HasPrefix(line, "#") {
            continue
        }

        key, _, _ := strings.Cut(line, "=")
        key = strings.TrimSpace(key)
        if true == strings.HasPrefix(key, "MELODY_") {
            continue
        }

        if false == declared[key] {
            t.Errorf("the .env key %s is not declared in EnvironmentKeys()", key)
        }
    }

    if scanErr := scanner.Err(); nil != scanErr {
        t.Fatalf("read .env: %v", scanErr)
    }
}

func TestExampleKeys_NoTemplateReadsAKeyAsParameter(t *testing.T) {
    patterns := []*regexp.Regexp{
        regexp.MustCompile(`%[A-Z][A-Z0-9_]*%`),
        regexp.MustCompile(`%env\(default:`),
        regexp.MustCompile(`Get\("[A-Z][A-Z0-9_]*"\)`),
    }

    patterns = append(
        patterns,
        regexp.MustCompile(`Get\(environmentKey[A-Za-z0-9]*\)`),
        regexp.MustCompile(`"%" \+ environmentKey`),
    )

    sourceList := []string{}
    walkErr := filepath.WalkDir("..", func(path string, entry fs.DirEntry, err error) error {
        if nil != err {
            return err
        }

        if true == entry.IsDir() && ("vendor" == entry.Name() || "node_modules" == entry.Name() || true == strings.HasPrefix(entry.Name(), ".")) && ".." != path {
            return filepath.SkipDir
        }

        if false == entry.IsDir() && true == strings.HasSuffix(path, ".go") && false == strings.HasSuffix(path, "_test.go") {
            sourceList = append(sourceList, path)
        }

        return nil
    })
    if nil != walkErr {
        t.Fatalf("walk: %v", walkErr)
    }

    for _, source := range sourceList {

        content, readErr := os.ReadFile(source)
        if nil != readErr {
            t.Fatalf("read %s: %v", source, readErr)
        }

        for _, pattern := range patterns {
            if match := pattern.Find(content); nil != match {
                t.Errorf("%s reads a key as a parameter: %s", source, match)
            }
        }
    }
}

func TestExampleKeys_HttpAddressComesFromTheApplicationKey(t *testing.T) {
    configuration := exampleConfiguration(t, map[string]string{"APP_HTTP_ADDRESS": ":18123"})

    if ":18123" != configuration.Http().Address() {
        t.Fatalf("expected the address of APP_HTTP_ADDRESS, got %q", configuration.Http().Address())
    }

    if overridden := configuration.OverriddenEnvironmentKeys(); 0 != len(overridden) {
        t.Fatalf("expected no overridden key set, got %v", overridden)
    }

    shadowed := exampleConfiguration(t, map[string]string{"APP_HTTP_ADDRESS": ":18123", "MELODY_HTTP_ADDRESS": ":8080"})

    overridden := shadowed.OverriddenEnvironmentKeys()
    if 1 != len(overridden) || "MELODY_HTTP_ADDRESS" != overridden[0].Key {
        t.Fatalf("expected MELODY_HTTP_ADDRESS listed as overridden, got %v", overridden)
    }
}

func TestExampleKeys_HttpAddressDefault(t *testing.T) {
    if address := exampleConfiguration(t, map[string]string{}).Http().Address(); defaultHttpAddress != address {
        t.Fatalf("expected the declared default %q, got %q", defaultHttpAddress, address)
    }
}

func TestExampleKeys_EmptyBaseCurrencyStaysEmpty(t *testing.T) {
    if value := exampleConfiguration(t, map[string]string{environmentKeyRatesBaseCurrency: ""}).MustGet(parameterRatesBaseCurrency).String(); "" != value {
        t.Fatalf("expected a present empty base to stay empty, got %q", value)
    }

    if value := exampleConfiguration(t, map[string]string{}).MustGet(parameterRatesBaseCurrency).String(); defaultRatesBaseCurrency != value {
        t.Fatalf("expected an absent base to take the seed's %q, got %q", defaultRatesBaseCurrency, value)
    }
}

func TestExampleKeys_CredentialsAndOutboundUrlsAreSecret(t *testing.T) {
    secretKeyList := []string{
        environmentKeyMysqlPassword,
        environmentKeyPgsqlPassword,
        environmentKeyS3SecretKey,
        environmentKeyAmqpDsn,
        environmentKeyMetricsToken,
        environmentKeyRatesApiKey,
        environmentKeyEncryptKeys,
        environmentKeyJwtSecret,
        environmentKeyInternalAuthSecret,
        environmentKeySmtpPassword,
        environmentKeyRatesBaseUrl,
        environmentKeyReportExportEndpoint,
    }

    declaredSecret := map[string]bool{}
    for _, key := range EnvironmentKeys() {
        declaredSecret[key.Key] = key.Secret
    }

    for _, key := range secretKeyList {
        if false == declaredSecret[key] {
            t.Errorf("expected %s declared secret", key)
        }
    }

    configuration := exampleConfiguration(t, map[string]string{environmentKeyRatesBaseUrl: "http://rates.example.test/v1/"})
    if false == configuration.MustGet(parameterRatesBaseUrl).IsSecret() {
        t.Fatalf("expected the rates url secret without a userinfo")
    }
}

func TestExampleKeys_ANameKeyTakesItsDefaultOnABlankLine(t *testing.T) {
    configuration := exampleConfiguration(t, map[string]string{environmentKeyInternalAuthKeyId: "  ", environmentKeyInternalAuthApplication: ""})

    if value := configuration.MustGet(parameterInternalAuthKeyId).String(); internalCallerKeyId != value {
        t.Fatalf("expected the key id default %q, got %q", internalCallerKeyId, value)
    }

    if value := configuration.MustGet(parameterInternalAuthApplication).String(); internalCallerApplication != value {
        t.Fatalf("expected the application default %q, got %q", internalCallerApplication, value)
    }
}

func TestExampleKeys_ABoundParameterAnswersWithoutItsLine(t *testing.T) {
    configuration := exampleConfiguration(t, map[string]string{})

    for _, parameter := range []string{parameterRatesBaseUrl, parameterReportExportEndpoint, parameterMysqlHost, parameterPgsqlHost} {
        if value := configuration.MustGet(parameter).String(); "" != value {
            t.Errorf("expected %s empty without its line, got %q", parameter, value)
        }
    }
}

func TestExampleKeys_AnIntegrationSwitchHasNoDefault(t *testing.T) {
    for _, key := range EnvironmentKeys() {
        for _, integrationKeyPrefix := range []string{"MYSQL_", "PGSQL_", "S3_", "SMTP_", "AMQP_", "REDIS_", "OTEL_"} {
            if true == strings.HasPrefix(key.Key, integrationKeyPrefix) && "" != key.Default {
                t.Errorf("expected the integration key %s without a default, got %q", key.Key, key.Default)
            }
        }
    }
}

func TestExampleKeys_TheCommittedEnvPassesEveryRule(t *testing.T) {
    content, readErr := os.ReadFile(filepath.Join("..", ".env"))
    if nil != readErr {
        t.Fatalf("read .env: %v", readErr)
    }

    environment, environmentErr := melodyconfig.NewEnvironment(
        context.Background(),
        melodyconfig.NewEnvironmentSource(melodyconfig.EnvironmentSourceConfig{FileSystem: fstest.MapFS{".env": &fstest.MapFile{Data: content}}, BaseDirectory: "."}),
    )
    if nil != environmentErr {
        t.Fatalf("new environment: %v", environmentErr)
    }

    configuration, configurationErr := melodyconfig.NewConfiguration(
        environment,
        "/tmp/melody",
        melodyconfig.ConfigurationConfig{EnvironmentKeys: EnvironmentKeys(), ParameterOverrides: ParameterOverrides()},
    )
    if nil != configurationErr {
        t.Fatalf("new configuration: %v", configurationErr)
    }

    if checkErr := configuration.CheckEnvironmentKeys(); nil != checkErr {
        t.Fatalf("expected the committed .env to pass every rule, got %v", checkErr)
    }

    if ":8080" != configuration.Http().Address() {
        t.Fatalf("expected the committed address, got %q", configuration.Http().Address())
    }
}

func TestExampleKeys_EachRuleRefusesWhatItsReaderRefuses(t *testing.T) {
    for _, testCase := range []struct {
        environmentKey string
        refused        []string
        admitted       []string
    }{
        {environmentKeyMysqlInsecure, []string{"TRUE", "1", "yes", " true"}, []string{"", "true", "false"}},
        {environmentKeyPgsqlInsecure, []string{"on"}, []string{"true"}},
        {environmentKeyS3Insecure, []string{"treu"}, []string{"false"}},
        {environmentKeySmtpAllowPlaintext, []string{"yes"}, []string{"true"}},
        {environmentKeySmtpImplicitTls, []string{"1"}, []string{"false"}},
        {environmentKeyDeviceIdentifierMaxBytes, []string{"0", "256", "abc", "unlimited"}, []string{"", "1", "255"}},
        {environmentKeyRequestBudgetPerHour, []string{"0", "-5", "1.5", "unlimited"}, []string{"", "1", "100000"}},
        {environmentKeyMaxItemsPerPage, []string{"0", "abc"}, []string{"", "1"}},
        {environmentKeyReportingRefreshInterval, []string{"-5m", "abc", "unlimited"}, []string{"", "0s", "1h"}},
    } {
        for _, value := range testCase.refused {
            if nil == exampleConfiguration(t, map[string]string{testCase.environmentKey: value}).CheckEnvironmentKeys() {
                t.Errorf("expected %s=%q refused by its rule", testCase.environmentKey, value)
            }
        }

        for _, value := range testCase.admitted {
            if checkErr := exampleConfiguration(t, map[string]string{testCase.environmentKey: value}).CheckEnvironmentKeys(); nil != checkErr {
                t.Errorf("expected %s=%q admitted, got %v", testCase.environmentKey, value, checkErr)
            }
        }
    }
}

func TestExampleKeys_AnAbsentOrBlankLineTakesTheDeclaredDefault(t *testing.T) {
    for _, testCase := range []struct {
        environmentKey string
        parameter      string
        expected       string
    }{
        {environmentKeyHttpAddress, parameterHttpAddress, defaultHttpAddress},
        {environmentKeyMaxItemsPerPage, parameterMaxItemsPerPage, "25"},
        {environmentKeyReportingRefreshInterval, parameterReportingRefreshInterval, defaultReportingRefreshInterval.String()},
        {environmentKeyDeviceIdentifierMaxBytes, parameterDeviceIdentifierMaxBytes, "64"},
    } {
        for _, values := range []map[string]string{{}, {testCase.environmentKey: ""}} {
            if value := exampleConfiguration(t, values).MustGet(testCase.parameter).String(); testCase.expected != value {
                t.Errorf("expected %s to answer %q for %v, got %q", testCase.parameter, testCase.expected, values, value)
            }
        }
    }

    absent := exampleConfiguration(t, map[string]string{})
    for parameter, expected := range map[string]string{parameterCatalogTitle: defaultCatalogTitle, parameterCronProductUser: defaultCronProductUser} {
        if value := absent.MustGet(parameter).String(); expected != value {
            t.Errorf("expected %s to answer %q when absent, got %q", parameter, expected, value)
        }
    }
}
