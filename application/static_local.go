//go:build !melody_static_embedded

package application

import (
	"io/fs"

	configcontract "github.com/precision-soft/melody/config/contract"
	"github.com/precision-soft/melody/http/static"
)

func newStaticFileServerOptions(
	embeddedPublicFiles fs.FS,
	configuration configcontract.Configuration,
) *static.Options {
	_ = embeddedPublicFiles

	fileServerConfig := static.NewFileServerConfig(
		static.ModeFilesystem,
		configuration.Http().PublicDir(),
		configuration.Http().StaticIndexFile(),
		"",
		configuration.Http().StaticEnableCache(),
		configuration.Http().StaticCacheMaxAge(),
		false,
	)

	return static.NewOptions(
		fileServerConfig,
		configuration.Kernel().ProjectDir(),
		nil,
	)
}
