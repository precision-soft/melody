package rueidis

import (
	"crypto/tls"
	"time"
)

func DefaultClientConfig() *ClientConfig {
	return &ClientConfig{
		ClientName:       "",
		SelectDb:         0,
		DisableCache:     true,
		TlsConfig:        nil,
		PingOnStart:      true,
		DialTimeout:      5 * time.Second,
		ConnWriteTimeout: 5 * time.Second,
	}
}

type ClientConfig struct {
	ClientName       string
	SelectDb         int
	DisableCache     bool
	TlsConfig        *tls.Config
	PingOnStart      bool
	DialTimeout      time.Duration
	ConnWriteTimeout time.Duration
}
