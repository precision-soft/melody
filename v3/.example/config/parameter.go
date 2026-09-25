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

    /* the empty-string fallback of both outbound endpoints: a constructor argument bound to a parameter reads it through MustGet, which panics on an unregistered parameter, and a parameter declared here survives its .env line being removed. */
    registrar.RegisterParameter(parameterReportExportEndpoint, "%env(default::"+environmentKeyReportExportEndpoint+")%")
    registrar.RegisterParameter(parameterRatesBaseUrl, "%env(default::"+environmentKeyRatesBaseUrl+")%")

    /* the base currency defaults to the seed's rather than to the empty string: a catalogue with no base named
       is a catalogue quoted against the one it ships with. The default applies when the key is ABSENT; a key
       present and empty reaches the refresh as an empty base, which the refresh refuses on every document
       rather than folding onto — an empty base would otherwise admit exactly the document that names none */
    registrar.RegisterParameter(parameterRatesDefaultBaseCurrency, defaultRatesBaseCurrency)
    registrar.RegisterParameter(
        parameterRatesBaseCurrency,
        "%env(default:"+parameterRatesDefaultBaseCurrency+":"+environmentKeyRatesBaseCurrency+")%",
    )

    /* the two outbound urls are where the process points, which an operator reads in debug:parameters, so they are not redacted as a rule; written with a userinfo — the shape the amqp dsn is marked for, and one the client sends as a credential — the url IS a credential, and the mark covers it together with the parameter whose template reads it. The value is read raw here, before resolution: a .env key is a literal, and the userinfo is in the literal or nowhere. */
    for _, environmentKey := range []string{environmentKeyRatesBaseUrl, environmentKeyReportExportEndpoint} {
        instance.markEnvironmentSecret(registrar, environmentKey, urlCarriesUserinfo)
    }

    /* the credentials melody registers from .env are marked here, so debug:parameters redacts them and every template that reads them; AMQP_DSN carries its credentials inline. No parameter assembles a template from the MYSQL_* or PGSQL_* keys, because those are the switches the readme says to remove. */
    for _, environmentKey := range []string{environmentKeyMysqlPassword, environmentKeyPgsqlPassword, environmentKeyS3SecretKey, environmentKeyAmqpDsn} {
        instance.markEnvironmentSecret(registrar, environmentKey, nil)
    }
}

/* markEnvironmentSecret marks a key secret only when the environment defines it and, with carriesSecret, only when its value does. An undefined key is not marked, because the integration blocks are switches the readme says to remove and a mark that matches no parameter warns at boot. */
func (instance *Module) markEnvironmentSecret(
    registrar melodyapplicationcontract.ParameterRegistrar,
    environmentKey string,
    carriesSecret func(value string) bool,
) {
    parameter := instance.configuration.Get(environmentKey)
    if nil == parameter {
        return
    }

    if nil != carriesSecret && false == carriesSecret(parameter.String()) {
        return
    }

    registrar.MarkParameterSecret(environmentKey)
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
