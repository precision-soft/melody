package config

import (
    "net/url"
    "strings"

    melodycron "github.com/precision-soft/melody/integrations/cron/v3"
    melodyapplicationcontract "github.com/precision-soft/melody/v3/application/contract"
)

func (instance *Module) RegisterParameters(registrar melodyapplicationcontract.ParameterRegistrar) {
    registrar.RegisterParameter(melodycron.ParameterDestinationFile, "%kernel.project_dir%/generated_conf/cron/crontab")
    registrar.RegisterParameter(melodycron.ParameterLogsDir, "%kernel.logs_dir%/cron")
    registrar.RegisterParameter(melodycron.ParameterTemplate, melodycron.TemplateNameCrontab)
    registrar.RegisterParameter(melodycron.ParameterUser, "%APP_CRON_USER%")
    registrar.RegisterParameter(melodycron.ParameterHeartbeatAutoEnabled, "%APP_CRON_HEARTBEAT_AUTO_ENABLED%")

    registrar.RegisterParameter(melodycron.ParameterImage, "%APP_CRON_IMAGE%")
    registrar.RegisterParameter(melodycron.ParameterNamespace, "%APP_CRON_NAMESPACE%")
    registrar.RegisterParameter(melodycron.ParameterRestartPolicy, "%APP_CRON_RESTART_POLICY%")

    registrar.RegisterParameter("app.max_items_per_page", "%APP_MAX_ITEMS_PER_PAGE%")
    registrar.RegisterParameter("app.catalog_title", "%APP_CATALOG_TITLE%")
    registrar.RegisterParameter("app.cron.product_user", "%APP_CRON_PRODUCT_USER%")

    registrar.RegisterParameter("app.reporting.default_refresh_interval", "5m")
    registrar.RegisterParameter(
        "app.reporting.refresh_interval",
        "%env(default:app.reporting.default_refresh_interval:APP_REPORTING_REFRESH_INTERVAL)%",
    )

    registrar.RegisterParameter(parameterReportExportEndpoint, "%env(default::"+environmentKeyReportExportEndpoint+")%")
    registrar.RegisterParameter(parameterRatesBaseUrl, "%env(default::"+environmentKeyRatesBaseUrl+")%")

    for _, environmentKey := range []string{environmentKeyRatesBaseUrl, environmentKeyReportExportEndpoint} {
        if true == urlCarriesUserinfo(instance.environmentValue(environmentKey)) {
            registrar.MarkParameterSecret(environmentKey)
        }
    }

    registrar.MarkParameterSecret("MYSQL_PASSWORD")
    registrar.MarkParameterSecret(environmentKeyPgsqlPassword)
    registrar.MarkParameterSecret("S3_SECRET_KEY")
    registrar.MarkParameterSecret(environmentKeyAmqpDsn)
}

var _ melodyapplicationcontract.ParameterModule = (*Module)(nil)

func urlCarriesUserinfo(value string) bool {
    parsed, parseErr := url.Parse(strings.TrimSpace(value))
    if nil != parseErr {
        return false
    }

    return nil != parsed.User
}
