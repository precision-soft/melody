//go:build melody_env_embedded

package application

import (
    "io/fs"

    "github.com/precision-soft/melody/v3/config"
    configcontract "github.com/precision-soft/melody/v3/config/contract"
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/internal"
)

func newEnvironmentSource(
    projectDirectory string,
    embeddedEnvFiles fs.FS,
) configcontract.EnvironmentSource {
    _ = projectDirectory

    if true == internal.IsNilInterface(embeddedEnvFiles) {
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

func missingEnvironmentFileHint(projectDirectory string) string {
    _ = projectDirectory

    return ""
}
