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

    registrar.MarkParameterSecret("APP_API_TOKEN")
}

var _ melodyapplicationcontract.ParameterModule = (*Module)(nil)
