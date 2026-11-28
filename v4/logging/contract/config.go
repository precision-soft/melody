package contract

const LoggingConfigurationName = "logging"

type LoggingConfiguration interface {
    LevelLabels() LevelLabels
}
