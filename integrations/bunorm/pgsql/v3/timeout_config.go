package pgsql

import "time"

func DefaultTimeoutConfig() *TimeoutConfig {
    return &TimeoutConfig{
        ConnectTimeout: 5 * time.Second,
        ReadTimeout:    30 * time.Second,
        WriteTimeout:   30 * time.Second,
    }
}

func NewTimeoutConfig(
    connectTimeout time.Duration,
    readTimeout time.Duration,
    writeTimeout time.Duration,
) *TimeoutConfig {
    return &TimeoutConfig{
        ConnectTimeout: connectTimeout,
        ReadTimeout:    readTimeout,
        WriteTimeout:   writeTimeout,
    }
}

/* TimeoutConfig names every deadline the driver applies, so none governs invisibly: without ReadTimeout and WriteTimeout here, pgdriver's own defaults — 10 seconds per read, 5 per write — cut every query that legitimately runs past them, with nothing in this package's configuration to even mention they exist. */
type TimeoutConfig struct {
    ConnectTimeout time.Duration
    ReadTimeout    time.Duration
    WriteTimeout   time.Duration
}
