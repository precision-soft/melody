/*
 * sync-icons — copy the shared icons out of the repository's single source, <root>/.assets, into this
 * example's public/ directory.
 *
 * The icons live in exactly ONE place in the tree. They are git-ignored here, so a checkout carries no
 * copy of them and none can go stale: this script is what puts them where the pages link them, and it
 * runs as the `prebuild` step of `npm run build`, so the one command a fresh clone needs for the browser
 * interface produces the bundle and the icons together. The container entrypoint runs it too, for each
 * major, so a development stack is populated before anything is served.
 *
 * A symlink would be the obvious way to keep one copy and it does not work here, for three measured
 * reasons: `//go:embed all:public` skips symlinks SILENTLY, so a melody_static_embedded binary would
 * carry no icons and say nothing about it; `os.CopyFS`, which the end-to-end harness uses to assemble a
 * runnable workspace, copies the link rather than its target and leaves it dangling; and public/ ships
 * on its own in packaging modes C and D, where a link pointing outside it has nothing to point at.
 */

import {copyFileSync, existsSync, mkdirSync} from "node:fs";
import {dirname, join, resolve} from "node:path";
import {fileURLToPath} from "node:url";

/* the mapping from source name to served name, and the only place it is written. logo.png serves as both
   the og/preview image and the apple touch icon, and logo.svg is what the pages link as the svg favicon. */
const iconList = [
    {source: "favicon.ico", target: join("public", "favicon.ico")},
    {source: "logo.svg", target: join("public", "assets", "favicon.svg")},
    {source: "logo.png", target: join("public", "assets", "logo.png")},
    {source: "logo.png", target: join("public", "assets", "apple-touch-icon.png")},
];

const assetsDirectory = dirname(fileURLToPath(import.meta.url));
const exampleDirectory = resolve(assetsDirectory, "..");

/* the repository root is found by walking up to the directory that holds .assets rather than by counting
   ".." segments, because the three examples sit at different depths and a hardcoded count is a copy of
   this file that is wrong in the other two */
const findSourceDirectory = () => {
    let candidate = exampleDirectory;

    for (; ;) {
        const sourceDirectory = join(candidate, ".assets");
        if (true === existsSync(sourceDirectory)) {
            return sourceDirectory;
        }

        const parent = dirname(candidate);
        if (parent === candidate) {
            return null;
        }

        candidate = parent;
    }
};

const sourceDirectory = findSourceDirectory();
if (null === sourceDirectory) {
    console.error(`sync-icons: no .assets directory above ${exampleDirectory} — the icons have a single source and it is missing`);
    process.exit(1);
}

for (const icon of iconList) {
    const from = join(sourceDirectory, icon.source);
    const to = join(exampleDirectory, icon.target);

    if (false === existsSync(from)) {
        console.error(`sync-icons: ${from} does not exist`);
        process.exit(1);
    }

    mkdirSync(dirname(to), {recursive: true});
    copyFileSync(from, to);
}

console.log(`sync-icons: ${iconList.length} icons copied from ${sourceDirectory}`);
