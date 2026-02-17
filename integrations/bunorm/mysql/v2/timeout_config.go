package mysql

import "time"

func DefaultTimeoutConfig() *TimeoutConfig {
	return &TimeoutConfig{
		ConnectTimeout: 10 * time.Second,
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

type TimeoutConfig struct {
	ConnectTimeout time.Duration
	ReadTimeout    time.Duration
	WriteTimeout   time.Duration
}
