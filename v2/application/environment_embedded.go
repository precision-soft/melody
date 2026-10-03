//go:build melody_env_embedded

package application

import (
    "io/fs"

    "github.com/precision-soft/melody/v2/config"
    configcontract "github.com/precision-soft/melody/v2/config/contract"
    "github.com/precision-soft/melody/v2/exception"
    "github.com/precision-soft/melody/v2/internal"
)

func newEnvironmentSource(
    projectDirectory string,
    embeddedEnvFiles fs.FS,
) configcontract.EnvironmentSource {
    _ = projectDirectory

    /* read through the interface: a typed-nil fs.FS passes the plain comparison and would die later inside fs.Stat instead of in this refusal that names the argument */
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

/* missingEnvironmentFileHint has no on-disk .env to point at in the embedded build: the environment is read from the embedded fs. */
func missingEnvironmentFileHint(projectDirectory string) string {
    _ = projectDirectory

    return ""
}
