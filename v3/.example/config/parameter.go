package config

import (
    melodycron "github.com/precision-soft/melody/integrations/cron/v3"
    melodyapplicationcontract "github.com/precision-soft/melody/v3/application/contract"
)

func (instance *Module) RegisterParameters(registrar melodyapplicationcontract.ParameterRegistrar) {
    registrar.RegisterParameter(melodycron.ParameterDestinationFile, "%kernel.project_dir%/generated_conf/cron/crontab")
    registrar.RegisterParameter(melodycron.ParameterLogsDir, "%kernel.logs_dir%/cron")
    registrar.RegisterParameter(melodycron.ParameterTemplate, melodycron.TemplateNameCrontab)
    registrar.RegisterParameter(melodycron.ParameterUser, "%APP_CRON_USER%")
    registrar.RegisterParameter(melodycron.ParameterHeartbeatAutoEnabled, "%APP_CRON_HEARTBEAT_AUTO_ENABLED%")

    /* @info defaults for the built-in k8s template (melody:cron:generate --template=k8s); the crontab template ignores them */
    registrar.RegisterParameter(melodycron.ParameterImage, "%APP_CRON_IMAGE%")
    registrar.RegisterParameter(melodycron.ParameterNamespace, "%APP_CRON_NAMESPACE%")
    registrar.RegisterParameter(melodycron.ParameterRestartPolicy, "%APP_CRON_RESTART_POLICY%")

    registrar.RegisterParameter("app.max_items_per_page", "%APP_MAX_ITEMS_PER_PAGE%")
    registrar.RegisterParameter("app.catalog_title", "%APP_CATALOG_TITLE%")
    registrar.RegisterParameter("app.cron.product_user", "%APP_CRON_PRODUCT_USER%")
}

var _ melodyapplicationcontract.ParameterModule = (*Module)(nil)
