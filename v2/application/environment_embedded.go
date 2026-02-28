//go:build melody_env_embedded

package application

import (
    "io/fs"

    "github.com/precision-soft/melody/v2/config"
    configcontract "github.com/precision-soft/melody/v2/config/contract"
    "github.com/precision-soft/melody/v2/exception"
)

func newEnvironmentSource(
    projectDirectory string,
    embeddedEnvFiles fs.FS,
) configcontract.EnvironmentSource {
    _ = projectDirectory

    if nil == embeddedEnvFiles {
        exception.Panic(
            exception.NewError(
                "embedded environment files are not provided",
                map[string]any{"buildTag": "melody_env_embedded", "projectDirectory": projectDirectory},
                nil,
            ),
        )
    }

    return config.NewEnvironmentSource(embeddedEnvFiles, ".")
}
