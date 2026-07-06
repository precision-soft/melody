/*
 * app.ts — the example's frontend entry point, bundled to public/assets/app.js.
 *
 * URL generation goes through the framework RouteGenerator (melody-routes.ts): the
 * server injects the route manifest as window.melodyRoutes on each page, and every
 * link, form action and fetch target is built from a route name instead of a
 * hardcoded path — the same manifest the melody:routes:manifest command exports.
 */

import {RouteGenerator, RouteManifest, RouteParams} from "./melody-routes";

/* jQuery is loaded separately by the pages and is not bundled here. */
declare const $: any;

declare global {
    interface Window {
        melodyRoutes?: RouteManifest;
        melodyExample?: Record<string, unknown>;
        $?: unknown;
    }
}

const requireJquery = (): void => {
    if ("undefined" === typeof window.$) {
        throw new Error("jquery is required");
    }
};

const routeGenerator = new RouteGenerator(
    window.melodyRoutes ?? {routes: []},
);

const route = (routeName: string, parameters?: RouteParams): string => {
    return routeGenerator.path(String(routeName ?? ""), parameters ?? {});
};

const toNormalizedAjaxError = (ajaxErr: any): Error => {
    if (ajaxErr instanceof Error) {
        return ajaxErr;
    }

    const statusCode = Number(ajaxErr?.status ?? 0);
    const responseJson = ajaxErr?.responseJSON ?? null;

    const errorList = Array.isArray(responseJson?.errors) ? responseJson.errors : [];
    const firstError = 0 < errorList.length ? String(errorList[0] ?? "").trim() : "";

    const fallbackMessage = String(ajaxErr?.statusText ?? "request failed").trim();
    const message = "" === firstError ? fallbackMessage : firstError;

    const normalized = new Error(message) as Error & { statusCode?: number };
    normalized.statusCode = statusCode;

    return normalized;
};

const normalizeSuccessBodyOrThrow = (body: any): any => {
    if (true !== (body?.success ?? false)) {
        const message = body?.errors?.[0] ?? "request failed";
        throw new Error(String(message ?? "request failed"));
    }

    return body;
};

const requestJson = (method: string, url: string, data?: unknown): any => {
    requireJquery();

    const safeMethod = String(method ?? "").trim().toUpperCase();
    if ("" === safeMethod) {
        throw new Error("invalid http method");
    }

    const headers: Record<string, string> = {
        Accept: "application/json",
    };

    const hasBody = null != data && "GET" !== safeMethod && "HEAD" !== safeMethod;
    if (true === hasBody) {
        headers["Content-Type"] = "application/json";
    }

    const requestOptions: Record<string, unknown> = {
        url: url,
        method: safeMethod,
        headers: headers,
        dataType: "json",
    };

    if (true === hasBody) {
        requestOptions.data = JSON.stringify(data ?? {});
    }

    return $.ajax(requestOptions)
        .then(normalizeSuccessBodyOrThrow)
        .catch((ajaxErr: any) => {
            throw toNormalizedAjaxError(ajaxErr);
        });
};

const getJson = (url: string): any => requestJson("GET", url, null);
const postJson = (url: string, data?: unknown): any => requestJson("POST", url, data ?? {});
const putJson = (url: string, data?: unknown): any => requestJson("PUT", url, data ?? {});
const deleteJson = (url: string): any => requestJson("DELETE", url, null);

const setStatus = (text: string, variant?: string): void => {
    requireJquery();

    const node = $("#status");
    if (0 === node.length) {
        return;
    }

    const message = String(text ?? "").trim();
    if ("" === message) {
        node.addClass("d-none");
        node.text("");
        return;
    }

    const safeVariant = String(variant ?? "info").trim();
    node.removeClass("alert-info alert-success alert-danger alert-warning d-none");
    node.addClass(`alert-${safeVariant}`);
    node.text(message);
};

const resolveDataRoutes = (): void => {
    requireJquery();

    const nodes = $("[data-route]");
    if (0 === nodes.length) {
        return;
    }

    nodes.each((_index: number, element: any) => {
        const node = $(element);
        const routeName = String(node.attr("data-route") ?? "").trim();
        if ("" === routeName) {
            return;
        }

        const paramsJsonString = String(node.attr("data-route-params") ?? "").trim();
        let params: RouteParams = {};

        if ("" !== paramsJsonString) {
            try {
                params = JSON.parse(paramsJsonString);
            } catch (err) {
                params = {};
            }
        }

        try {
            const url = route(routeName, params);

            if (true === node.is("a")) {
                node.attr("href", url);
            }

            if (true === node.is("form")) {
                node.attr("action", url);
            }

            if (true === node.is("button")) {
                node.attr("data-route-url", url);
            }
        } catch (err) {
            /* an unknown or unsatisfiable route leaves the element untouched */
        }
    });
};

const initAuthHeader = async (options?: { loginSelector?: string; logoutSelector?: string }): Promise<void> => {
    requireJquery();

    const settings = options ?? {};
    const loginSelector = String(settings.loginSelector ?? "#loginButton").trim();
    const logoutSelector = String(settings.logoutSelector ?? "#logoutButton").trim();

    const loginNode = $(loginSelector);
    const logoutNode = $(logoutSelector);

    if (0 === loginNode.length || 0 === logoutNode.length) {
        return;
    }

    const showLoggedOut = (): void => {
        loginNode.removeClass("d-none");
        logoutNode.addClass("d-none");
    };

    const showLoggedIn = (): void => {
        loginNode.addClass("d-none");
        logoutNode.removeClass("d-none");
    };

    try {
        await getJson(route("example.routes"));
        showLoggedIn();
    } catch (err: any) {
        showLoggedOut();
    }

    logoutNode.off("click").on("click", (event: any) => {
        event.preventDefault();

        try {
            window.location.href = route("example.logout");
        } catch (err) {
            window.location.href = "/logout";
        }
    });
};

window.melodyExample = window.melodyExample ?? {};
window.melodyExample.http = {getJson, postJson, putJson, deleteJson, requestJson};
window.melodyExample.ui = {setStatus};
window.melodyExample.routing = {route, resolveDataRoutes};
window.melodyExample.auth = {initAuthHeader};

$(document).ready(() => {
    try {
        resolveDataRoutes();
    } catch (err) {
        /* do nothing */
    }
});
