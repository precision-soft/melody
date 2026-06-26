/*
 * melody-routes — generate URLs on the frontend from the route manifest emitted by
 * the `melody:routes:manifest` CLI command, referencing routes by name instead of
 * hardcoding paths. Framework-owned reference helper; see v3/.example/assets for a
 * usage example.
 *
 * Patterns use melody's `:param` placeholder syntax (e.g. `/users/:id`). Parameters
 * not consumed by a placeholder are appended as query-string parameters.
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

    /* path builds the path (and query string) for a named route, substituting `:param`
     * placeholders from params (falling back to the route's defaults) and appending any
     * leftover params and the explicit query as query-string parameters. */
    path(name: string, params: RouteParams = {}, query: RouteParams = {}): string {
        const entry = this.byName.get(name);
        if (undefined === entry) {
            throw new Error(`unknown route: ${name}`);
        }

        const consumed = new Set<string>();

        const path = entry.pattern.replace(/:([A-Za-z0-9_]+)/g, (_match, key: string) => {
            const value = params[key] ?? entry.defaults?.[key];
            if (undefined === value) {
                throw new Error(`missing route parameter "${key}" for route "${name}"`);
            }

            consumed.add(key);

            return encodeURIComponent(String(value));
        });

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
