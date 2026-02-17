//go:build melody_static_embedded

package application

import (
	"io/fs"

	configcontract "github.com/precision-soft/melody/v2/config/contract"
	"github.com/precision-soft/melody/v2/exception"
	exceptioncontract "github.com/precision-soft/melody/v2/exception/contract"
	"github.com/precision-soft/melody/v2/http/static"
)

func newStaticFileServerOptions(
	embeddedPublicFiles fs.FS,
	configuration configcontract.Configuration,
) *static.Options {
	if nil == embeddedPublicFiles {
		exception.Panic(
			exception.NewError(
				"embedded public files are not provided",
				exceptioncontract.Context{
					"buildTag":        "melody_static_embedded",
					"publicDirectory": configuration.Http().PublicDir(),
				},
				nil,
			),
		)
	}

	fileServerConfig := static.NewFileServerConfig(
		static.ModeEmbedded,
		configuration.Http().PublicDir(),
		configuration.Http().StaticIndexFile(),
		"",
		configuration.Http().StaticEnableCache(),
		configuration.Http().StaticCacheMaxAge(),
		false,
	)

	return static.NewOptions(
		fileServerConfig,
		"",
		embeddedPublicFiles,
	)
}
