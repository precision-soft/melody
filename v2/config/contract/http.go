package contract

type HttpConfiguration interface {
	Address() string

	DefaultLocale() string

	PublicDir() string

	StaticIndexFile() string

	MaxRequestBodyBytes() int

	StaticEnableCache() bool

	StaticCacheMaxAge() int
}
