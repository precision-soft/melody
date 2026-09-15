//go:build !melody_env_embedded

package application

import (
    "io/fs"
    "os"

    "github.com/precision-soft/melody/v3/config"
    configcontract "github.com/precision-soft/melody/v3/config/contract"
)

func newEnvironmentSource(
    projectDirectory string,
    embeddedEnvFiles fs.FS,
) configcontract.EnvironmentSource {
    _ = embeddedEnvFiles

    fileSystem := os.DirFS(projectDirectory)

    return config.NewEnvironmentSource(fileSystem, ".")
}

func missingEnvironmentFileHint(projectDirectory string) string {
    if "" == projectDirectory || true == workingDirectoryHasEnvironmentFile(projectDirectory) {
        return ""
    }

    return "; no .env, .env.local or .env." + config.EnvDevelopment + " file was found in " + projectDirectory + " — create one there, or build with -tags melody_env_embedded to embed it"
}
