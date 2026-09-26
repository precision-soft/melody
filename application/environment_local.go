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

/* missingEnvironmentFileHint returns a remedy when the project directory holds no environment file the source would load, and the empty string otherwise. Boot appends it to a configuration-resolution failure, so "undefined environment key" names the real cause; a missing file is never a failure in itself. */
func missingEnvironmentFileHint(projectDirectory string) string {
    if "" == projectDirectory || true == workingDirectoryHasEnvironmentFile(projectDirectory) {
        return ""
    }

    return "; no .env, .env.local or .env." + config.EnvDevelopment + " file was found in " + projectDirectory + " — create one there, or build with -tags melody_env_embedded to embed it"
}
