//go:build melody_env_embedded

package application

import (
    "io/fs"

    "github.com/precision-soft/melody/v3/config"
    configcontract "github.com/precision-soft/melody/v3/config/contract"
    "github.com/precision-soft/melody/v3/exception"
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

/* missingEnvironmentFileHint has no on-disk .env to point at in the embedded build: the environment is read from the embedded fs (a nil fs already fails loudly in newEnvironmentSource), so a resolution failure here is never a missing-file-beside-the-binary problem. */
func missingEnvironmentFileHint(projectDirectory string) string {
    _ = projectDirectory

    return ""
}
