package contract

type EnvironmentSource interface {
	Load() (map[string]string, error)
}
