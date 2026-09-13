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

    /* defaults for the built-in k8s template (melody:cron:generate --template=k8s); the crontab template ignores them */
    registrar.RegisterParameter(melodycron.ParameterImage, "%APP_CRON_IMAGE%")
    registrar.RegisterParameter(melodycron.ParameterNamespace, "%APP_CRON_NAMESPACE%")
    registrar.RegisterParameter(melodycron.ParameterRestartPolicy, "%APP_CRON_RESTART_POLICY%")

    registrar.RegisterParameter("app.max_items_per_page", "%APP_MAX_ITEMS_PER_PAGE%")
    registrar.RegisterParameter("app.catalog_title", "%APP_CATALOG_TITLE%")
    registrar.RegisterParameter("app.cron.product_user", "%APP_CRON_PRODUCT_USER%")

    /* the reporting interval is optional in the environment: the default processor falls back to the parameter below when APP_REPORTING_REFRESH_INTERVAL is unset, which is how a deployment overrides it without every environment having to define it */
    registrar.RegisterParameter("app.reporting.default_refresh_interval", "5m")
    registrar.RegisterParameter(
        "app.reporting.refresh_interval",
        "%env(default:app.reporting.default_refresh_interval:APP_REPORTING_REFRESH_INTERVAL)%",
    )

    /* the empty-string fallback, used by both outbound endpoints: they are genuinely absent in most
       environments, so the parameter resolves to "" instead of every environment having to define a key it
       does not use. The distinction matters because a constructor argument BOUND to a parameter reads it
       through MustGet, which panics on a parameter that was never registered — an auto-registered .env key
       disappears with its line, a parameter declared here does not. */
    registrar.RegisterParameter(parameterReportExportEndpoint, "%env(default::"+environmentKeyReportExportEndpoint+")%")
    registrar.RegisterParameter(parameterRatesBaseUrl, "%env(default::"+environmentKeyRatesBaseUrl+")%")

    /* the base currency defaults to the seed's rather than to the empty string: an empty base would refuse
       every document, and a catalogue with no base named is a catalogue quoted against the one it ships with */
    registrar.RegisterParameter(parameterRatesDefaultBaseCurrency, defaultRatesBaseCurrency)
    registrar.RegisterParameter(
        parameterRatesBaseCurrency,
        "%env(default:"+parameterRatesDefaultBaseCurrency+":"+environmentKeyRatesBaseCurrency+")%",
    )

    /* the two outbound urls are where the process points, which an operator reads in debug:parameters, so they are not redacted as a rule; written with a userinfo — the shape the amqp dsn is marked for, and one the client sends as a credential — the url IS a credential, and the mark covers it together with the parameter whose template reads it. The value is read raw here, before resolution: a .env key is a literal, and the userinfo is in the literal or nowhere. */
    for _, environmentKey := range []string{environmentKeyRatesBaseUrl, environmentKeyReportExportEndpoint} {
        if true == urlCarriesUserinfo(instance.environmentValue(environmentKey)) {
            registrar.MarkParameterSecret(environmentKey)
        }
    }

    /* the credentials melody registers automatically from .env are marked here, so debug:parameters redacts them along with anything whose template reads them. AMQP_DSN is on the list because it carries its credentials INLINE: the amqp credentials sit whole in this one key and no marked source exists to propagate from.

       No parameter of this application assembles a template out of the integration keys — MYSQL_*, PGSQL_* — and none may: those keys are the switches the readme says to REMOVE to boot the fallbacks, and a template that read one without a default made the boot fail the moment its line was gone, over a value nothing consumed. The mysql provider assembles its own connection from the keys it reads directly. */
    registrar.MarkParameterSecret("MYSQL_PASSWORD")
    registrar.MarkParameterSecret(environmentKeyPgsqlPassword)
    registrar.MarkParameterSecret("S3_SECRET_KEY")
    registrar.MarkParameterSecret(environmentKeyAmqpDsn)
}

var _ melodyapplicationcontract.ParameterModule = (*Module)(nil)

/* urlCarriesUserinfo answers whether a url names a credential in its authority, "scheme://user:secret@host"; a value that does not parse carries none the client would send. */
func urlCarriesUserinfo(value string) bool {
    parsed, parseErr := url.Parse(strings.TrimSpace(value))
    if nil != parseErr {
        return false
    }

    return nil != parsed.User
}
