package contract

type Middleware func(next Handler) Handler

type RateLimiter interface {
	Allow(key string) bool

	Reset(key string)
}
