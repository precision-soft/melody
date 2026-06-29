/*
 * melody-routes — generate URLs on the frontend from the route manifest emitted by
 * the `melody:routes:manifest` CLI command, referencing routes by name instead of
 * hardcoding paths. Framework-owned reference helper; see v3/.example/assets for a
 * usage example.
 *
 * Patterns use melody's placeholder syntax, matched per path segment exactly as the
 * server-side router and Go URL generator do: required `:param`, optional `:param?`
 * (dropped when no value is given), single wildcard `*name`, and catch-all `*name...`
 * (or a trailing `*name`, which may span multiple slash-separated segments). A colon or
 * star only introduces a placeholder when it begins a segment. Parameters not consumed
 * by a placeholder are appended as query-string parameters.
 */

export interface RouteManifestEntry {
    name: string;
    pattern: string;
    methods?: string[];
    requirements?: Record<string, string>;
    defaults?: Record<string, string>;
    zone?: string;
}

export interface RouteManifest {
    routes: RouteManifestEntry[];
}

export type RouteParams = Record<string, string | number | boolean | undefined>;

export class RouteGenerator {
    private readonly byName: Map<string, RouteManifestEntry>;

    constructor(manifest: RouteManifest) {
        this.byName = new Map(manifest.routes.map((route) => [route.name, route]));
    }

    has(name: string): boolean {
        return this.byName.has(name);
    }

    /* path builds the path (and query string) for a named route, substituting `:param`,
     * `:param?`, `*name` and `*name...` placeholders per segment from params (falling back
     * to the route's defaults) and appending any leftover params and the explicit query as
     * query-string parameters. */
    path(name: string, params: RouteParams = {}, query: RouteParams = {}): string {
        const entry = this.byName.get(name);
        if (undefined === entry) {
            throw new Error(`unknown route: ${name}`);
        }

        const consumed = new Set<string>();

        const lookup = (key: string): string | undefined => {
            const value = params[key] ?? entry.defaults?.[key];
            return undefined === value ? undefined : String(value);
        };

        const segments = entry.pattern.split("/");
        const resultSegments: string[] = [];

        for (let index = 1; index < segments.length; index++) {
            const segment = segments[index];

            if (true === segment.startsWith(":")) {
                let key = segment.slice(1);
                let optional = false;
                if (true === key.endsWith("?")) {
                    optional = true;
                    key = key.slice(0, -1);
                }

                const value = lookup(key);
                if (undefined === value) {
                    if (true === optional) {
                        continue;
                    }
                    throw new Error(`missing route parameter "${key}" for route "${name}"`);
                }

                consumed.add(key);
                resultSegments.push(encodeURIComponent(value));

                continue;
            }

            if (true === segment.startsWith("*")) {
                let key = segment.slice(1);
                let catchAll = index === segments.length - 1;
                if (true === key.endsWith("...")) {
                    catchAll = true;
                    key = key.slice(0, -3);
                }

                const value = "" === key ? undefined : lookup(key);
                if ("" !== key) {
                    consumed.add(key);
                }

                if (false === catchAll) {
                    if ("" === key) {
                        throw new Error(`wildcard segment must be named for route "${name}"`);
                    }
                    if (undefined === value) {
                        throw new Error(`missing wildcard parameter "${key}" for route "${name}"`);
                    }
                    if (true === value.includes("/")) {
                        throw new Error(`wildcard parameter "${key}" cannot contain a slash for route "${name}"`);
                    }

                    resultSegments.push(encodeURIComponent(value));

                    continue;
                }

                if (undefined === value) {
                    continue;
                }

                for (const part of value.split("/")) {
                    if ("" === part) {
                        continue;
                    }
                    resultSegments.push(encodeURIComponent(part));
                }

                continue;
            }

            resultSegments.push(segment);
        }

        const path = 0 === resultSegments.length ? "/" : `/${resultSegments.join("/")}`;

        const search = new URLSearchParams();
        for (const [key, value] of Object.entries(params)) {
            if (false === consumed.has(key) && undefined !== value) {
                search.append(key, String(value));
            }
        }
        for (const [key, value] of Object.entries(query)) {
            if (undefined !== value) {
                search.append(key, String(value));
            }
        }

        const queryString = search.toString();

        return "" === queryString ? path : `${path}?${queryString}`;
    }
}
