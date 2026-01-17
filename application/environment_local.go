//go:build !melody_env_embedded

package application

import (
	"io/fs"
	"os"

	"github.com/precision-soft/melody/config"
	configcontract "github.com/precision-soft/melody/config/contract"
)

func newEnvironmentSource(
	projectDirectory string,
	embeddedEnvFiles fs.FS,
) configcontract.EnvironmentSource {
	_ = embeddedEnvFiles

	fileSystem := os.DirFS(projectDirectory)

	return config.NewEnvironmentSource(fileSystem, ".")
}
