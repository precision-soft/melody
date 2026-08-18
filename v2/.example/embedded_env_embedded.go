//go:build melody_env_embedded

package main

import (
    "embed"
    "io/fs"
)

/* the embed names the committed file alone, never a glob: ".env*" also matched the gitignored .env.local — the machine-local override file that holds real credentials precisely because it never enters git — and baked it into every embedded-env binary, where the loader's precedence then let it override the committed .env at runtime. A file the repository does not carry must not ship in the artifact. */
//go:embed .env
var embeddedEnv embed.FS

var embeddedEnvFiles fs.FS = embeddedEnv
