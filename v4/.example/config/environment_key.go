package config

import (
    "strconv"
    "time"

    "github.com/precision-soft/melody/v4/.example/wiring"
    melodybound "github.com/precision-soft/melody/v4/bound"
    melodyconfig "github.com/precision-soft/melody/v4/config"
    configcontract "github.com/precision-soft/melody/v4/config/contract"
)

const (
    environmentKeyHttpAddress              = "APP_HTTP_ADDRESS"
    environmentKeyMaxItemsPerPage          = "APP_MAX_ITEMS_PER_PAGE"
    environmentKeyCatalogTitle             = "APP_CATALOG_TITLE"
    environmentKeyCronProductUser          = "APP_CRON_PRODUCT_USER"
    environmentKeyReportingRefreshInterval = "APP_REPORTING_REFRESH_INTERVAL"
)

const (
    parameterHttpAddress              = "app.http.address"
    parameterMaxItemsPerPage          = "app.max_items_per_page"
    parameterCatalogTitle             = "app.catalog_title"
    parameterCronProductUser          = "app.cron.product_user"
    parameterReportingRefreshInterval = "app.reporting.refresh_interval"

    parameterMysqlHost     = "app.mysql.host"
    parameterMysqlPort     = "app.mysql.port"
    parameterMysqlDatabase = "app.mysql.database"
    parameterMysqlUser     = "app.mysql.user"
    parameterMysqlPassword = "app.mysql.password"
    parameterMysqlInsecure = "app.mysql.insecure"

    parameterPgsqlHost     = "app.pgsql.host"
    parameterPgsqlPort     = "app.pgsql.port"
    parameterPgsqlDatabase = "app.pgsql.database"
    parameterPgsqlUser     = "app.pgsql.user"
    parameterPgsqlPassword = "app.pgsql.password"
    parameterPgsqlInsecure = "app.pgsql.insecure"

    parameterRedisAddress      = "app.redis.address"
    parameterSessionFile       = "app.session.file"
    parameterMetricsToken      = "app.metrics.token"
    parameterEncryptKeys       = "app.encrypt.keys"
    parameterEncryptCurrentKey = "app.encrypt.current_key"

    parameterAmqpDsn = "app.amqp.dsn"

    parameterS3Endpoint  = "app.s3.endpoint"
    parameterS3AccessKey = "app.s3.access_key"
    parameterS3SecretKey = "app.s3.secret_key"
    parameterS3Insecure  = "app.s3.insecure"
    parameterS3Region    = "app.s3.region"
    parameterS3Bucket    = "app.s3.bucket"

    parameterSmtpAddress     = "app.smtp.address"
    parameterSmtpUsername    = "app.smtp.username"
    parameterSmtpPassword    = "app.smtp.password"
    parameterSmtpRequireTls  = "app.smtp.require_tls"
    parameterSmtpImplicitTls = "app.smtp.implicit_tls"

    parameterDeviceIdentifierMaxBytes = "app.device_identifier.max_bytes"
    parameterLocalStorageDirectory    = "app.local_storage.directory"
    parameterOtelExporterEndpoint     = "app.otel.exporter_endpoint"

    parameterCorsAllowOrigins     = "app.cors.allow_origins"
    parameterRequestBudgetPerHour = "app.request_budget.per_hour"
    parameterTrustedProxyList     = "app.trusted_proxy.list"
    parameterSessionCookieSecure  = "app.session.cookie_secure"

    parameterRatesBaseUrl         = wiring.ParameterRatesBaseUrl
    parameterRatesApiKey          = "app.rates.api_key"
    parameterRatesBaseCurrency    = wiring.ParameterRatesBaseCurrency
    parameterReportExportEndpoint = wiring.ParameterReportExportEndpoint

    parameterJwtSecret               = "app.jwt.secret"
    parameterInternalAuthKeyId       = "app.internal_auth.key_id"
    parameterInternalAuthApplication = "app.internal_auth.application"
    parameterInternalAuthSecret      = "app.internal_auth.secret"
)

const (
    defaultHttpAddress              = ":8080"
    defaultMaxItemsPerPage          = 25
    defaultCatalogTitle             = "Melody Example Catalog"
    defaultCronProductUser          = "catalog"
    defaultReportingRefreshInterval = 5 * time.Minute

    /* the seed quotes against the euro; a deployment names another base only when it reseeds against another */
    defaultRatesBaseCurrency = "EUR"
)

/* a key with no default and no rule is an integration switch: absent or blank leaves its door unwired */
func EnvironmentKeys() []configcontract.EnvironmentKey {
    return []configcontract.EnvironmentKey{
        exampleKey(environmentKeyHttpAddress, parameterHttpAddress, defaultHttpAddress, false, nameRule),
        exampleKey(environmentKeyMaxItemsPerPage, parameterMaxItemsPerPage, strconv.Itoa(defaultMaxItemsPerPage), false, melodyconfig.LimitKeyRule(melodybound.Rule[int]{Default: defaultMaxItemsPerPage, Minimum: 1})),
        exampleKey(environmentKeyCatalogTitle, parameterCatalogTitle, defaultCatalogTitle, false, nil),
        exampleKey(environmentKeyCronProductUser, parameterCronProductUser, defaultCronProductUser, false, nil),
        exampleKey(environmentKeyReportingRefreshInterval, parameterReportingRefreshInterval, defaultReportingRefreshInterval.String(), false, melodyconfig.LimitKeyRule(melodybound.Rule[time.Duration]{Default: defaultReportingRefreshInterval})),

        exampleKey(environmentKeyMysqlHost, parameterMysqlHost, "", false, nil),
        exampleKey(environmentKeyMysqlPort, parameterMysqlPort, "", false, nil),
        exampleKey(environmentKeyMysqlDatabase, parameterMysqlDatabase, "", false, nil),
        exampleKey(environmentKeyMysqlUser, parameterMysqlUser, "", false, nil),
        exampleKey(environmentKeyMysqlPassword, parameterMysqlPassword, "", true, nil),
        exampleKey(environmentKeyMysqlInsecure, parameterMysqlInsecure, "", false, switchRule),

        exampleKey(environmentKeyPgsqlHost, parameterPgsqlHost, "", false, nil),
        exampleKey(environmentKeyPgsqlPort, parameterPgsqlPort, "", false, nil),
        exampleKey(environmentKeyPgsqlDatabase, parameterPgsqlDatabase, "", false, nil),
        exampleKey(environmentKeyPgsqlUser, parameterPgsqlUser, "", false, nil),
        exampleKey(environmentKeyPgsqlPassword, parameterPgsqlPassword, "", true, nil),
        exampleKey(environmentKeyPgsqlInsecure, parameterPgsqlInsecure, "", false, switchRule),

        exampleKey(environmentKeyRedisAddress, parameterRedisAddress, "", false, nil),
        exampleKey(environmentKeySessionFile, parameterSessionFile, "", false, nil),
        exampleKey(environmentKeyMetricsToken, parameterMetricsToken, "", true, nil),
        exampleKey(environmentKeyEncryptKeys, parameterEncryptKeys, "", true, nil),
        exampleKey(environmentKeyEncryptCurrentKey, parameterEncryptCurrentKey, "", false, nil),

        exampleKey(environmentKeyAmqpDsn, parameterAmqpDsn, "", true, nil),

        exampleKey(environmentKeyS3Endpoint, parameterS3Endpoint, "", false, nil),
        exampleKey(environmentKeyS3AccessKey, parameterS3AccessKey, "", false, nil),
        exampleKey(environmentKeyS3SecretKey, parameterS3SecretKey, "", true, nil),
        exampleKey(environmentKeyS3Insecure, parameterS3Insecure, "", false, switchRule),
        exampleKey(environmentKeyS3Region, parameterS3Region, "", false, nil),
        exampleKey(environmentKeyS3Bucket, parameterS3Bucket, "", false, nil),

        exampleKey(environmentKeySmtpAddress, parameterSmtpAddress, "", false, nil),
        exampleKey(environmentKeySmtpUsername, parameterSmtpUsername, "", false, nil),
        exampleKey(environmentKeySmtpPassword, parameterSmtpPassword, "", true, nil),
        exampleKey(environmentKeySmtpRequireTls, parameterSmtpRequireTls, "", false, switchRule),
        exampleKey(environmentKeySmtpImplicitTls, parameterSmtpImplicitTls, "", false, switchRule),

        exampleKey(environmentKeyDeviceIdentifierMaxBytes, parameterDeviceIdentifierMaxBytes, strconv.Itoa(defaultDeviceIdentifierMaximumBytes), false, melodyconfig.LimitKeyRule(deviceIdentifierMaximumBytesRule)),
        exampleKey(environmentKeyLocalStorageDirectory, parameterLocalStorageDirectory, "", false, nil),
        exampleKey(environmentKeyOtelExporterEndpoint, parameterOtelExporterEndpoint, "", false, nil),

        exampleKey(environmentKeyCorsAllowOrigins, parameterCorsAllowOrigins, "", false, nil),
        exampleKey(environmentKeyRequestBudgetPerHour, parameterRequestBudgetPerHour, "", false, melodyconfig.LimitKeyRule(requestBudgetPerHourRule)),
        exampleKey(environmentKeyTrustedProxyList, parameterTrustedProxyList, "", false, nil),
        exampleKey(environmentKeySessionCookieSecure, parameterSessionCookieSecure, "", false, melodyconfig.ChoiceKeyRule([]string{sessionCookieSecureFromScheme, sessionCookieSecureAlways})),

        /* the two outbound urls are secret whatever they hold: a userinfo in either is a credential the client sends */
        exampleKey(environmentKeyRatesBaseUrl, parameterRatesBaseUrl, "", true, nil),
        exampleKey(environmentKeyRatesApiKey, parameterRatesApiKey, "", true, nil),
        /* without a rule a present blank line is the value: an empty base the refresh refuses, never folded onto the seed's */
        exampleKey(environmentKeyRatesBaseCurrency, parameterRatesBaseCurrency, defaultRatesBaseCurrency, false, nil),
        exampleKey(environmentKeyReportExportEndpoint, parameterReportExportEndpoint, "", true, nil),

        exampleKey(environmentKeyJwtSecret, parameterJwtSecret, "", true, nil),
        exampleKey(environmentKeyInternalAuthKeyId, parameterInternalAuthKeyId, internalCallerKeyId, false, nameRule),
        exampleKey(environmentKeyInternalAuthApplication, parameterInternalAuthApplication, internalCallerApplication, false, nameRule),
        exampleKey(environmentKeyInternalAuthSecret, parameterInternalAuthSecret, "", true, nil),
    }
}

/* a .env line that still sets MELODY_HTTP_ADDRESS refuses the boot */
func ParameterOverrides() map[string]string {
    return map[string]string{
        melodyconfig.KernelHttpAddress: "%env(" + environmentKeyHttpAddress + ")%",
    }
}

func exampleKey(
    key string,
    parameter string,
    defaultValue string,
    secret bool,
    rule configcontract.EnvironmentKeyRule,
) configcontract.EnvironmentKey {
    return configcontract.EnvironmentKey{
        Key:       key,
        Parameter: parameter,
        Field:     "config.EnvironmentKeys " + key,
        Default:   defaultValue,
        Secret:    secret,
        Rule:      rule,
    }
}

/* switchRule admits what booleanSwitch reads: empty, "true" or "false", exactly */
var switchRule = melodyconfig.ChoiceKeyRule([]string{"true", "false"})

/* a rule that admits every text, so a blank line takes the declared default */
var nameRule configcontract.EnvironmentKeyRule = nameKeyRule{}

type nameKeyRule struct{}

func (instance nameKeyRule) Check(text string, field string) error {
    return nil
}

func (instance nameKeyRule) Describe() string {
    return "name"
}

func (instance *Module) parameterValue(name string) string {
    return instance.configuration.MustGet(name).String()
}
