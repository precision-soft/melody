package cron

import (
    melodyconfig "github.com/precision-soft/melody/v4/config"
    configcontract "github.com/precision-soft/melody/v4/config/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
)

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

const (
    KeyUser             = "MELODY_CRON_USER"
    KeyLogsDir          = "MELODY_CRON_LOGS_DIR"
    KeyBinary           = "MELODY_CRON_BINARY"
    KeyDestinationFile  = "MELODY_CRON_DESTINATION_FILE"
    KeyHeartbeatPath    = "MELODY_CRON_HEARTBEAT_PATH"
    KeyHeartbeatEnabled = "MELODY_CRON_HEARTBEAT_ENABLED"
    KeyTemplate         = "MELODY_CRON_TEMPLATE"
    KeyImage            = "MELODY_CRON_K8S_IMAGE"
    KeyNamespace        = "MELODY_CRON_K8S_NAMESPACE"
    KeyRestartPolicy    = "MELODY_CRON_K8S_RESTART_POLICY"
)

const (
    defaultDestinationFile = "%kernel.project_dir%/generated_conf/cron/crontab"
    defaultLogsDir         = "%kernel.logs_dir%/cron"
)

/* KeyDefaults are the values the ten keys take when no .env file sets them; a zero field takes the module's own default. */
type KeyDefaults struct {
    User             string
    LogsDir          string
    Binary           string
    DestinationFile  string
    HeartbeatPath    string
    HeartbeatEnabled configcontract.Switch
    Template         string
    Image            string
    Namespace        string
    RestartPolicy    string
}

/* EnvironmentKeys answers the ten keys the module owns, for Module.RegisterEnvironmentKeys and for an application module that builds NewGenerateCommand without the cron module. */
func EnvironmentKeys(defaults KeyDefaults) []configcontract.EnvironmentKey {
    heartbeatEnabled := ""
    if true == defaults.HeartbeatEnabled.IsSet {
        heartbeatEnabled = "false"
        if true == defaults.HeartbeatEnabled.Value {
            heartbeatEnabled = "true"
        }
    }

    return []configcontract.EnvironmentKey{
        {Key: KeyUser, Parameter: ParameterUser, Field: "cron.KeyDefaults.User", Default: defaults.User},
        {Key: KeyLogsDir, Parameter: ParameterLogsDir, Field: "cron.KeyDefaults.LogsDir", Default: textOrDefault(defaults.LogsDir, defaultLogsDir)},
        {Key: KeyBinary, Parameter: ParameterBinary, Field: "cron.KeyDefaults.Binary", Default: defaults.Binary},
        {Key: KeyDestinationFile, Parameter: ParameterDestinationFile, Field: "cron.KeyDefaults.DestinationFile", Default: textOrDefault(defaults.DestinationFile, defaultDestinationFile)},
        {Key: KeyHeartbeatPath, Parameter: ParameterHeartbeatPath, Field: "cron.KeyDefaults.HeartbeatPath", Default: defaults.HeartbeatPath},
        {Key: KeyHeartbeatEnabled, Parameter: ParameterHeartbeatAutoEnabled, Field: "cron.KeyDefaults.HeartbeatEnabled", Default: heartbeatEnabled, Rule: melodyconfig.SwitchKeyRule()},
        {Key: KeyTemplate, Parameter: ParameterTemplate, Field: "cron.KeyDefaults.Template", Default: textOrDefault(defaults.Template, TemplateNameCrontab)},
        {Key: KeyImage, Parameter: ParameterImage, Field: "cron.KeyDefaults.Image", Default: defaults.Image},
        {Key: KeyNamespace, Parameter: ParameterNamespace, Field: "cron.KeyDefaults.Namespace", Default: defaults.Namespace, Rule: namespaceKeyRule{}},
        {Key: KeyRestartPolicy, Parameter: ParameterRestartPolicy, Field: "cron.KeyDefaults.RestartPolicy", Default: defaults.RestartPolicy, Rule: melodyconfig.ChoiceKeyRule([]string{"OnFailure", "Never"})},
    }
}

func textOrDefault(text string, defaultText string) string {
    if "" == text {
        return defaultText
    }

    return text
}

/* namespaceKeyRule admits an empty value as unset and otherwise the RFC 1123 label the generator requires */
type namespaceKeyRule struct{}

func (instance namespaceKeyRule) Check(text string, field string) error {
    if "" == text || true == isRfc1123Label(text) {
        return nil
    }

    return exception.NewError(
        "the k8s namespace is not a valid RFC 1123 label (lowercase alphanumerics and '-', starting and ending alphanumeric, at most 63 characters)",
        exceptioncontract.Context{"field": field},
        ErrK8sInvalidNamespace,
    )
}

func (instance namespaceKeyRule) Describe() string {
    return "rfc 1123 label"
}
