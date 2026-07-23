package bunorm

type ConnectionParameters struct {
    Host     string
    Port     string
    Database string
    User     string
    Password string
}

func (instance *ConnectionParameters) SafeContext() map[string]any {
    return map[string]any{
        "host":     instance.Host,
        "port":     instance.Port,
        "database": instance.Database,
        "user":     instance.User,
    }
}

/* Deprecated: use ConnectionParameters. */
type ConnectionParams = ConnectionParameters
