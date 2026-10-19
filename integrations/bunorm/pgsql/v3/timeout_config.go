package pgsql

import "time"

func DefaultTimeoutConfig() *TimeoutConfig {
    return &TimeoutConfig{
        ConnectTimeout: 5 * time.Second,
        ReadTimeout:    30 * time.Second,
        WriteTimeout:   30 * time.Second,
    }
}

/* NewTimeoutConfig sets the connect timeout and leaves the read and write deadlines zero, which the provider reads as their defaults; NewTimeoutConfigWithDeadlines sets all three. */
func NewTimeoutConfig(
    connectTimeout time.Duration,
) *TimeoutConfig {
    return &TimeoutConfig{
        ConnectTimeout: connectTimeout,
    }
}

/* NewTimeoutConfigWithDeadlines sets the connect timeout and the driver's read and write deadlines; a zero field takes its default and a negative one reads as Unlimited. */
func NewTimeoutConfigWithDeadlines(
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
