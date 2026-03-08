const requireJquery = () => {
    if ('undefined' === typeof window.$) {
        throw new Error('jquery is required');
    }
};

const toNormalizedAjaxError = (ajaxErr) => {
    if (ajaxErr instanceof Error) {
        return ajaxErr;
    }

    const statusCode = Number(ajaxErr?.status ?? 0);
    const responseJson = ajaxErr?.responseJSON ?? null;

    const errorList = Array.isArray(responseJson?.errors) ? responseJson.errors : [];
    const firstError = 0 < errorList.length ? String(errorList[0] ?? '').trim() : '';

    const fallbackMessage = String(ajaxErr?.statusText ?? 'request failed').trim();
    const message = '' === firstError ? fallbackMessage : firstError;

    const normalized = new Error(message);
    normalized.statusCode = statusCode;

    return normalized;
};

const normalizeSuccessBodyOrThrow = (body) => {
    if (true !== (body?.success ?? false)) {
        const message = body?.errors?.[0] ?? 'request failed';
        throw new Error(String(message ?? 'request failed'));
    }

    return body;
};

const getJson = (url) => {
    requireJquery();

    return $.ajax({
        url: url,
        method: 'GET',
        headers: {
            'Accept': 'application/json'
        },
        dataType: 'json'
    })
        .then(normalizeSuccessBodyOrThrow)
        .catch((ajaxErr) => {
            throw toNormalizedAjaxError(ajaxErr);
        });
};

const postJson = (url, data) => {
    requireJquery();

    return $.ajax({
        url: url,
        method: 'POST',
        headers: {
            'Accept': 'application/json',
            'Content-Type': 'application/json'
        },
        data: JSON.stringify(data ?? {}),
        dataType: 'json'
    })
        .then(normalizeSuccessBodyOrThrow)
        .catch((ajaxErr) => {
            throw toNormalizedAjaxError(ajaxErr);
        });
};

const putJson = (url, data) => {
    requireJquery();

    return $.ajax({
        url: url,
        method: 'PUT',
        headers: {
            'Accept': 'application/json',
            'Content-Type': 'application/json'
        },
        data: JSON.stringify(data ?? {}),
        dataType: 'json'
    })
        .then(normalizeSuccessBodyOrThrow)
        .catch((ajaxErr) => {
            throw toNormalizedAjaxError(ajaxErr);
        });
};

const requestJson = (method, url, data) => {
    requireJquery();

    const safeMethod = String(method ?? '').trim().toUpperCase();
    if ('' === safeMethod) {
        throw new Error('invalid http method');
    }

    const headers = {
        'Accept': 'application/json'
    };

    const hasBody = null != data && 'GET' !== safeMethod && 'HEAD' !== safeMethod;
    if (true === hasBody) {
        headers['Content-Type'] = 'application/json';
    }

    const requestOptions = {
        url: url,
        method: safeMethod,
        headers: headers,
        dataType: 'json'
    };

    if (true === hasBody) {
        requestOptions.data = JSON.stringify(data ?? {});
    }

    return $.ajax(requestOptions)
        .then(normalizeSuccessBodyOrThrow)
        .catch((ajaxErr) => {
            throw toNormalizedAjaxError(ajaxErr);
        });
};

const deleteJson = (url) => {
    return requestJson('DELETE', url, null);
};

const setStatus = (text, variant) => {
    requireJquery();

    const node = $('#status');
    if (0 === node.length) {
        return;
    }

    const message = String(text ?? '').trim();
    if ('' === message) {
        node.addClass('d-none');
        node.text('');
        return;
    }

    const safeVariant = String(variant ?? 'info').trim();
    node.removeClass('alert-info alert-success alert-danger alert-warning d-none');
    node.addClass(`alert-${safeVariant}`);
    node.text(message);
};

const routeDefinitionByName = (routeName) => {
    const safeName = String(routeName ?? '').trim();
    if ('' === safeName) {
        return null;
    }

    const definitions = Array.isArray(window.melodyRoutes) ? window.melodyRoutes : [];

    for (const definition of definitions) {
        if (safeName === String(definition?.name ?? '').trim()) {
            return {
                name: safeName,
                pattern: String(definition?.pattern ?? '').trim()
            };
        }
    }

    return null;
};

const generatePathFromPattern = (pattern, parameters) => {
    const safePattern = String(pattern ?? '').trim();
    if ('' === safePattern) {
        return '';
    }

    const params = parameters ?? {};

    let path = safePattern;

    for (const key of Object.keys(params)) {
        const safeKey = String(key ?? '').trim();
        if ('' === safeKey) {
            continue;
        }

        const token = `:${safeKey}`;
        const value = encodeURIComponent(String(params[key] ?? '').trim());

        path = path.split(token).join(value);
    }

    return path;
};

const route = (routeName, parameters) => {
    const definition = routeDefinitionByName(routeName);
    if (null === definition) {
        throw new Error(`unknown route: ${String(routeName ?? '')}`);
    }

    const path = generatePathFromPattern(definition.pattern, parameters);
    if ('' === path) {
        throw new Error(`invalid route pattern: ${definition.pattern}`);
    }

    return path;
};

const resolveDataRoutes = () => {
    requireJquery();

    const nodes = $('[data-route]');
    if (0 === nodes.length) {
        return;
    }

    nodes.each((index, element) => {
        const node = $(element);
        const routeName = String(node.attr('data-route') ?? '').trim();
        if ('' === routeName) {
            return;
        }

        const paramsJsonString = String(node.attr('data-route-params') ?? '').trim();
        let params = {};

        if ('' !== paramsJsonString) {
            try {
                params = JSON.parse(paramsJsonString);
            } catch (err) {
                params = {};
            }
        }

        try {
            const url = route(routeName, params);

            if (true === node.is('a')) {
                node.attr('href', url);
            }

            if (true === node.is('form')) {
                node.attr('action', url);
            }

            if (true === node.is('button')) {
                node.attr('data-route-url', url);
            }
        } catch (err) {
            /** do nothing */
        }
    });
};

const initAuthHeader = async (options) => {
    requireJquery();

    const settings = options ?? {};
    const loginSelector = String(settings.loginSelector ?? '#loginButton').trim();
    const logoutSelector = String(settings.logoutSelector ?? '#logoutButton').trim();

    const loginNode = $(loginSelector);
    const logoutNode = $(logoutSelector);

    if (0 === loginNode.length || 0 === logoutNode.length) {
        return;
    }

    const showLoggedOut = () => {
        loginNode.removeClass('d-none');
        logoutNode.addClass('d-none');
    };

    const showLoggedIn = () => {
        loginNode.addClass('d-none');
        logoutNode.removeClass('d-none');
    };

    try {
        await getJson(route('example.routes'));
        showLoggedIn();
    } catch (err) {
        const statusCode = Number(err?.statusCode ?? 0);
        if (401 === statusCode || 403 === statusCode) {
            showLoggedOut();
            return;
        }

        showLoggedOut();
    }

    logoutNode.off('click').on('click', (event) => {
        event.preventDefault();

        try {
            const logoutUrl = route('example.logout');
            window.location.href = logoutUrl;
        } catch (err) {
            window.location.href = '/logout';
        }
    });
};

window.melodyExample = window.melodyExample ?? {};
window.melodyExample.http = {
    getJson: getJson,
    postJson: postJson,
    putJson: putJson,
    deleteJson: deleteJson,
    requestJson: requestJson
};
window.melodyExample.ui = {
    setStatus: setStatus
};
window.melodyExample.routing = {
    route: route,
    resolveDataRoutes: resolveDataRoutes
};
window.melodyExample.auth = {
    initAuthHeader: initAuthHeader
};

$(document).ready(() => {
    try {
        window.melodyExample.routing.resolveDataRoutes();
    } catch (err) {
        /** do nothing */
    }
});
