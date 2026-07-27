package config

import (
    melodyapplicationcontract "github.com/precision-soft/melody/application/contract"
    melodycron "github.com/precision-soft/melody/integrations/cron"
)

func (instance *Module) RegisterParameters(registrar melodyapplicationcontract.ParameterRegistrar) {
    registrar.RegisterParameter(melodycron.ParameterDestinationFile, "%kernel.project_dir%/generated_conf/cron/crontab")
    registrar.RegisterParameter(melodycron.ParameterLogsDir, "%kernel.logs_dir%/cron")
    registrar.RegisterParameter(melodycron.ParameterTemplate, melodycron.TemplateNameCrontab)
    registrar.RegisterParameter(melodycron.ParameterUser, "%APP_CRON_USER%")
    registrar.RegisterParameter(melodycron.ParameterHeartbeatAutoEnabled, "%APP_CRON_HEARTBEAT_AUTO_ENABLED%")

    registrar.RegisterParameter("app.max_items_per_page", "%APP_MAX_ITEMS_PER_PAGE%")
    registrar.RegisterParameter("app.catalog_title", "%APP_CATALOG_TITLE%")
    registrar.RegisterParameter("app.cron.product_user", "%APP_CRON_PRODUCT_USER%")

    /* @info the optional-and-secret shapes the configuration supports: the refresh interval falls back to the parameter below when APP_REFRESH_INTERVAL is unset, and the api token registered automatically from .env is marked as a credential so debug:parameters redacts it along with anything whose template reads it */
    registrar.RegisterParameter("app.default_refresh_interval", "5m")
    registrar.RegisterParameter("app.refresh_interval", "%env(default:app.default_refresh_interval:APP_REFRESH_INTERVAL)%")

    /* @info the live integrations read their endpoints from parameters, each with a registered fallback so a partially set environment cannot leave a name the providers resolve undefined. An empty value is the switch: the example then wires the integration not at all. */
    registrar.RegisterParameter("app.database.default_port", "3306")
    registrar.RegisterParameter(ParameterDatabaseHost, "%env(default::MYSQL_HOST)%")
    registrar.RegisterParameter(ParameterDatabasePort, "%env(default:app.database.default_port:MYSQL_PORT)%")
    registrar.RegisterParameter(ParameterDatabaseName, "%env(default::MYSQL_DATABASE)%")
    registrar.RegisterParameter(ParameterDatabaseUser, "%env(default::MYSQL_USER)%")
    registrar.RegisterParameter(ParameterDatabasePassword, "%env(default::MYSQL_PASSWORD)%")

    registrar.RegisterParameter(ParameterRedisAddress, "%env(default::REDIS_ADDRESS)%")
    registrar.RegisterParameter(ParameterRedisUser, "%env(default::REDIS_USER)%")
    registrar.RegisterParameter(ParameterRedisPassword, "%env(default::REDIS_PASSWORD)%")

    registrar.MarkParameterSecret("APP_API_TOKEN")
    registrar.MarkParameterSecret("MYSQL_PASSWORD")
    registrar.MarkParameterSecret("REDIS_PASSWORD")
}

var _ melodyapplicationcontract.ParameterModule = (*Module)(nil)
