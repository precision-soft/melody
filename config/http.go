package config

import (
	"net"
	"regexp"
	"strconv"
	"strings"

	configcontract "github.com/precision-soft/melody/config/contract"
	"github.com/precision-soft/melody/exception"
	exceptioncontract "github.com/precision-soft/melody/exception/contract"
)

var (
	defaultLocalePattern = regexp.MustCompile(`^[a-z]{2}(-[A-Za-z]{2})?$`)
)

func newHttpConfiguration(
	address string,
	defaultLocale string,
	publicDir string,
	staticIndexFile string,
	maxRequestBodyBytes int,
	staticEnableCache bool,
	staticCacheMaxAge int,
) (*httpConfiguration, error) {
	if -1 == strings.Index(address, ":") {
		address = ":" + address
	}

	httpConfigurationInstance := &httpConfiguration{
		address:             address,
		defaultLocale:       defaultLocale,
		publicDir:           publicDir,
		staticIndexFile:     staticIndexFile,
		maxRequestBodyBytes: maxRequestBodyBytes,
		staticEnableCache:   staticEnableCache,
		staticCacheMaxAge:   staticCacheMaxAge,
	}

	validateErr := httpConfigurationInstance.validate()
	if nil != validateErr {
		return nil, validateErr
	}

	return httpConfigurationInstance, nil
}

type httpConfiguration struct {
	address             string
	defaultLocale       string
	publicDir           string
	staticIndexFile     string
	maxRequestBodyBytes int
	staticEnableCache   bool
	staticCacheMaxAge   int
}

func (instance *httpConfiguration) Address() string {
	return instance.address
}

func (instance *httpConfiguration) DefaultLocale() string {
	return instance.defaultLocale
}

func (instance *httpConfiguration) PublicDir() string {
	return instance.publicDir
}

func (instance *httpConfiguration) StaticIndexFile() string {
	return instance.staticIndexFile
}

func (instance *httpConfiguration) MaxRequestBodyBytes() int {
	return instance.maxRequestBodyBytes
}

func (instance *httpConfiguration) StaticEnableCache() bool {
	return instance.staticEnableCache
}

func (instance *httpConfiguration) StaticCacheMaxAge() int {
	return instance.staticCacheMaxAge
}

func (instance *httpConfiguration) validate() error {
	validateAddressErr := instance.validateAddress()
	if nil != validateAddressErr {
		return validateAddressErr
	}

	validateDefaultLocaleErr := instance.validateDefaultLocale()
	if nil != validateDefaultLocaleErr {
		return validateDefaultLocaleErr
	}

	validatePublicDirErr := instance.validatePublicDir()
	if nil != validatePublicDirErr {
		return validatePublicDirErr
	}

	validateStaticIndexFileErr := instance.validateStaticIndexFile()
	if nil != validateStaticIndexFileErr {
		return validateStaticIndexFileErr
	}

	validateMaxRequestBodyBytesErr := instance.validateMaxRequestBodyBytes()
	if nil != validateMaxRequestBodyBytesErr {
		return validateMaxRequestBodyBytesErr
	}

	validateStaticCacheMaxAgeErr := instance.validateStaticCacheMaxAge()
	if nil != validateStaticCacheMaxAgeErr {
		return validateStaticCacheMaxAgeErr
	}

	return nil
}

func (instance *httpConfiguration) validateAddress() error {
	address := instance.address
	if "" == address {
		return exception.NewError("http address may not be empty", nil, nil)
	}

	_, portString, splitHostPortErr := net.SplitHostPort(address)
	if nil != splitHostPortErr {
		return exception.NewError(
			"http address is invalid",
			exceptioncontract.Context{
				"address": address,
			},
			splitHostPortErr,
		)
	}

	port, atoiErr := strconv.Atoi(portString)
	if nil != atoiErr {
		return exception.NewError(
			"http port is invalid",
			exceptioncontract.Context{
				"address": address,
				"port":    portString,
			},
			atoiErr,
		)
	}

	if 1 > port || 65535 < port {
		return exception.NewError(
			"http port is out of range",
			exceptioncontract.Context{
				"address": address,
				"port":    port,
			},
			nil,
		)
	}

	return nil
}

func (instance *httpConfiguration) validateDefaultLocale() error {
	defaultLocale := instance.defaultLocale
	if "" == defaultLocale {
		return exception.NewError("default locale may not be empty", nil, nil)
	}

	if false == defaultLocalePattern.MatchString(defaultLocale) {
		return exception.NewError(
			"default locale is invalid",
			exceptioncontract.Context{
				"defaultLocale": defaultLocale,
			},
			nil,
		)
	}

	return nil
}

func (instance *httpConfiguration) validatePublicDir() error {
	publicDir := instance.publicDir
	if "" == publicDir {
		return exception.NewError("public directory may not be empty", nil, nil)
	}

	if true == strings.Contains(publicDir, "..") {
		return exception.NewError(
			"public directory is invalid",
			exceptioncontract.Context{
				"publicDir": publicDir,
			},
			nil,
		)
	}

	return nil
}

func (instance *httpConfiguration) validateStaticIndexFile() error {
	staticIndexFile := instance.staticIndexFile
	if "" == staticIndexFile {
		return exception.NewError("static index file may not be empty", nil, nil)
	}

	if true == strings.Contains(staticIndexFile, "/") || true == strings.Contains(staticIndexFile, `\`) {
		return exception.NewError(
			"static index file is invalid",
			exceptioncontract.Context{
				"staticIndexFile": staticIndexFile,
			},
			nil,
		)
	}

	return nil
}

func (instance *httpConfiguration) validateMaxRequestBodyBytes() error {
	if 0 >= instance.maxRequestBodyBytes {
		return exception.NewError(
			"invalid http max request body bytes",
			exceptioncontract.Context{
				"value": instance.maxRequestBodyBytes,
			},
			nil,
		)
	}

	return nil
}

func (instance *httpConfiguration) validateStaticCacheMaxAge() error {
	if 0 > instance.staticCacheMaxAge {
		return exception.NewError(
			"static cache max age must be zero or positive",
			exceptioncontract.Context{
				"staticCacheMaxAge": instance.staticCacheMaxAge,
			},
			nil,
		)
	}

	return nil
}

var _ configcontract.HttpConfiguration = (*httpConfiguration)(nil)
