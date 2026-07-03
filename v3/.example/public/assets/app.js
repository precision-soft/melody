"use strict";
(() => {
  // melody-routes.ts
  var RouteGenerator = class {
    constructor(manifest) {
      this.byName = new Map(manifest.routes.map((route2) => [route2.name, route2]));
    }
    has(name) {
      return this.byName.has(name);
    }
    /* path builds the path (and query string) for a named route, substituting `:param`,
     * `:param?`, `*name` and `*name...` placeholders per segment from params (falling back
     * to the route's defaults) and appending any leftover params and the explicit query as
     * query-string parameters. */
    path(name, params = {}, query = {}) {
      const entry = this.byName.get(name);
      if (void 0 === entry) {
        throw new Error(`unknown route: ${name}`);
      }
      const consumed = /* @__PURE__ */ new Set();
      const lookup = (key) => {
        const value = params[key] ?? entry.defaults?.[key];
        return void 0 === value ? void 0 : String(value);
      };
      const segments = entry.pattern.split("/");
      const resultSegments = [];
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
          if (void 0 === value) {
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
          const value = "" === key ? void 0 : lookup(key);
          if ("" !== key) {
            consumed.add(key);
          }
          if (false === catchAll) {
            if ("" === key) {
              throw new Error(`wildcard segment must be named for route "${name}"`);
            }
            if (void 0 === value) {
              throw new Error(`missing wildcard parameter "${key}" for route "${name}"`);
            }
            if (true === value.includes("/")) {
              throw new Error(`wildcard parameter "${key}" cannot contain a slash for route "${name}"`);
            }
            resultSegments.push(encodeURIComponent(value));
            continue;
          }
          if (void 0 === value) {
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
        if (false === consumed.has(key) && void 0 !== value) {
          search.append(key, String(value));
        }
      }
      for (const [key, value] of Object.entries(query)) {
        if (void 0 !== value) {
          search.append(key, String(value));
        }
      }
      const queryString = search.toString();
      return "" === queryString ? path : `${path}?${queryString}`;
    }
  };

  // app.ts
  var requireJquery = () => {
    if ("undefined" === typeof window.$) {
      throw new Error("jquery is required");
    }
  };
  var routeGenerator = new RouteGenerator(
    window.melodyRoutes ?? { routes: [] }
  );
  var route = (routeName, parameters) => {
    return routeGenerator.path(String(routeName ?? ""), parameters ?? {});
  };
  var toNormalizedAjaxError = (ajaxErr) => {
    if (ajaxErr instanceof Error) {
      return ajaxErr;
    }
    const statusCode = Number(ajaxErr?.status ?? 0);
    const responseJson = ajaxErr?.responseJSON ?? null;
    const errorList = Array.isArray(responseJson?.errors) ? responseJson.errors : [];
    const firstError = 0 < errorList.length ? String(errorList[0] ?? "").trim() : "";
    const fallbackMessage = String(ajaxErr?.statusText ?? "request failed").trim();
    const message = "" === firstError ? fallbackMessage : firstError;
    const normalized = new Error(message);
    normalized.statusCode = statusCode;
    return normalized;
  };
  var normalizeSuccessBodyOrThrow = (body) => {
    if (true !== (body?.success ?? false)) {
      const message = body?.errors?.[0] ?? "request failed";
      throw new Error(String(message ?? "request failed"));
    }
    return body;
  };
  var requestJson = (method, url, data) => {
    requireJquery();
    const safeMethod = String(method ?? "").trim().toUpperCase();
    if ("" === safeMethod) {
      throw new Error("invalid http method");
    }
    const headers = {
      Accept: "application/json"
    };
    const hasBody = null != data && "GET" !== safeMethod && "HEAD" !== safeMethod;
    if (true === hasBody) {
      headers["Content-Type"] = "application/json";
    }
    const requestOptions = {
      url,
      method: safeMethod,
      headers,
      dataType: "json"
    };
    if (true === hasBody) {
      requestOptions.data = JSON.stringify(data ?? {});
    }
    return $.ajax(requestOptions).then(normalizeSuccessBodyOrThrow).catch((ajaxErr) => {
      throw toNormalizedAjaxError(ajaxErr);
    });
  };
  var getJson = (url) => requestJson("GET", url, null);
  var postJson = (url, data) => requestJson("POST", url, data ?? {});
  var putJson = (url, data) => requestJson("PUT", url, data ?? {});
  var deleteJson = (url) => requestJson("DELETE", url, null);
  var setStatus = (text, variant) => {
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
  var resolveDataRoutes = () => {
    requireJquery();
    const nodes = $("[data-route]");
    if (0 === nodes.length) {
      return;
    }
    nodes.each((_index, element) => {
      const node = $(element);
      const routeName = String(node.attr("data-route") ?? "").trim();
      if ("" === routeName) {
        return;
      }
      const paramsJsonString = String(node.attr("data-route-params") ?? "").trim();
      let params = {};
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
      }
    });
  };
  var initAuthHeader = async (options) => {
    requireJquery();
    const settings = options ?? {};
    const loginSelector = String(settings.loginSelector ?? "#loginButton").trim();
    const logoutSelector = String(settings.logoutSelector ?? "#logoutButton").trim();
    const loginNode = $(loginSelector);
    const logoutNode = $(logoutSelector);
    if (0 === loginNode.length || 0 === logoutNode.length) {
      return;
    }
    const showLoggedOut = () => {
      loginNode.removeClass("d-none");
      logoutNode.addClass("d-none");
    };
    const showLoggedIn = () => {
      loginNode.addClass("d-none");
      logoutNode.removeClass("d-none");
    };
    try {
      await getJson(route("example.routes"));
      showLoggedIn();
    } catch (err) {
      showLoggedOut();
    }
    logoutNode.off("click").on("click", (event) => {
      event.preventDefault();
      try {
        window.location.href = route("example.logout");
      } catch (err) {
        window.location.href = "/logout";
      }
    });
  };
  window.melodyExample = window.melodyExample ?? {};
  window.melodyExample.http = { getJson, postJson, putJson, deleteJson, requestJson };
  window.melodyExample.ui = { setStatus };
  window.melodyExample.routing = { route, resolveDataRoutes };
  window.melodyExample.auth = { initAuthHeader };
  $(document).ready(() => {
    try {
      resolveDataRoutes();
    } catch (err) {
    }
  });
})();
