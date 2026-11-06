//go:build melody_env_embedded

package main

import (
    "embed"
    "io/fs"
)

/* the embed names the committed file alone, never a glob: ".env*" would also match the gitignored .env.local, which holds real credentials, bake it into the binary and let it override the committed .env at runtime. A file the repository does not carry must not ship in the artifact. */
//go:embed .env
var embeddedEnv embed.FS

var embeddedEnvFiles fs.FS = embeddedEnv
