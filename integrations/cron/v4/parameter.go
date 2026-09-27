package cron

const (
    ParameterUser                 = "melody.cron.user"
    ParameterLogsDir              = "melody.cron.logs_dir"
    ParameterBinary               = "melody.cron.binary"
    ParameterDestinationFile      = "melody.cron.destination_file"
    ParameterHeartbeatPath        = "melody.cron.heartbeat_path"
    ParameterHeartbeatAutoEnabled = "melody.cron.heartbeat.enabled"
    ParameterTemplate             = "melody.cron.template"
    ParameterImage                = "melody.cron.k8s.image"
    ParameterNamespace            = "melody.cron.k8s.namespace"
    ParameterRestartPolicy        = "melody.cron.k8s.restart_policy"
)

type ParameterRegistrar interface {
    RegisterParameter(name string, value any)
}

func RegisterDefaultParameters(registrar ParameterRegistrar) {
    registrar.RegisterParameter(ParameterDestinationFile, "%kernel.project_dir%/generated_conf/cron/crontab")
    registrar.RegisterParameter(ParameterLogsDir, "%kernel.logs_dir%/cron")
    registrar.RegisterParameter(ParameterTemplate, TemplateNameCrontab)
}
