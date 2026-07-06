/*
 * Example: integrating the melody route manifest on the frontend.
 *
 * 1. Generate the manifest from your app's CLI (routes opt in server-side via
 *    `http.ExposedRouteAttributes(zone)` on their RouteOptions attributes):
 *
 *        go run . melody:routes:manifest --out ./assets/routes.json
 *        # or scope to one zone:
 *        go run . melody:routes:manifest --zone frontend --out ./assets/routes.json
 *
 * 2. Load the JSON and build URLs by route name with the framework helper
 *    (melody-routes.ts sits next to this file; app.ts bundles it for the browser).
 */

import {RouteGenerator, RouteManifest} from "./melody-routes";

import manifestJson from "./routes.json";

const routes = new RouteGenerator(manifestJson as RouteManifest);

/* /users/api/read/42/ */
const userPath = routes.path("example.users.api.read", {id: 42});

/* /users/api/read/42/?tab=orders — leftover params become query string */
const userOrdersPath = routes.path("example.users.api.read", {id: 42, tab: "orders"});

/* guard before generating, e.g. for optionally-exposed routes */
if (routes.has("example.products.api.read")) {
    const productPath = routes.path("example.products.api.read", {id: 7});
    console.log(productPath);
}

console.log(userPath, userOrdersPath);
