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

/* TimeoutConfig names every deadline the driver applies, so none governs invisibly: left out, pgdriver's own defaults of 10 seconds per read and 5 per write would cut every query that runs past them. */
type TimeoutConfig struct {
    ConnectTimeout time.Duration
    ReadTimeout    time.Duration
    WriteTimeout   time.Duration
}
