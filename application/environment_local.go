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

/* missingEnvironmentFileHint returns an actionable remedy when the project directory holds no .env in the on-disk (non-embedded) build, and the empty string otherwise. Boot appends it to a configuration-resolution failure — a compiled binary run from a directory with no .env, or go run falling back to the working directory when no go.mod is found — so the otherwise unsuggestive "undefined environment key" names the real cause. Missing .env is only a hint on a failure, never a failure in itself: an app whose parameters all have defaults boots without one. */
func missingEnvironmentFileHint(projectDirectory string) string {
    if "" == projectDirectory || true == workingDirectoryHasEnvironmentFile(projectDirectory) {
        return ""
    }

    return "; no .env or .env.local file was found in " + projectDirectory + " — create one there, or build with -tags melody_env_embedded to embed it"
}
