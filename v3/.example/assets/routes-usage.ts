/*
 * Example: integrating the melody route manifest on the frontend.
 *
 * 1. Generate the manifest from your app's CLI (the routes opt in server-side via
 *    `http.ExposedRouteAttributes(zone)` on their RouteOptions attributes):
 *
 *        go run ./cmd/app melody:routes:manifest --out ./web/routes.json
 *        # or scope to one zone:
 *        go run ./cmd/app melody:routes:manifest --zone frontend --out ./web/routes.json
 *
 * 2. Load the JSON and build URLs by route name with the framework helper.
 *
 * Adjust the import path to wherever you vendor melody-routes.ts
 * (framework copy: v3/http/assets/melody-routes.ts).
 */

import { RouteGenerator, RouteManifest } from "./melody-routes";

import manifestJson from "./routes.json";

const routes = new RouteGenerator(manifestJson as RouteManifest);

/* /users/42 */
const userPath = routes.path("user_show", { id: 42 });

/* /users/42?tab=orders — leftover params become query string */
const userOrdersPath = routes.path("user_show", { id: 42, tab: "orders" });

/* guard before generating, e.g. for optionally-exposed routes */
if (routes.has("account_show")) {
    const accountPath = routes.path("account_show");
    console.log(accountPath);
}

console.log(userPath, userOrdersPath);
