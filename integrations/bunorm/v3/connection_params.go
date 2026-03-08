package bunorm

type ConnectionParams struct {
    Host     string
    Port     string
    Database string
    User     string
    Password string
}

func (instance *ConnectionParams) SafeContext() map[string]any {
    return map[string]any{
        "host":     instance.Host,
        "port":     instance.Port,
        "database": instance.Database,
        "user":     instance.User,
    }
}
