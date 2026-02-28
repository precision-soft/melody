package pgsql

func NewConnectionConfig(host string, port string, database string, user string, password string) *ConnectionConfig {
    return &ConnectionConfig{
        host:     host,
        port:     port,
        database: database,
        user:     user,
        password: password,
    }
}

type ConnectionConfig struct {
    host     string
    port     string
    database string
    user     string
    password string
}

func (instance *ConnectionConfig) Host() string {
    return instance.host
}

func (instance *ConnectionConfig) Port() string {
    return instance.port
}

func (instance *ConnectionConfig) Database() string {
    return instance.database
}

func (instance *ConnectionConfig) User() string {
    return instance.user
}

func (instance *ConnectionConfig) Password() string {
    return instance.password
}

func (instance *ConnectionConfig) SafeContext() map[string]any {
    return map[string]any{
        "host":     instance.host,
        "port":     instance.port,
        "database": instance.database,
        "user":     instance.user,
    }
}
