package websocketdemo

import (
    nethttp "net/http"

    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* PageHandler serves a self-contained page that opens a websocket to /ws and renders the counter the
backend broadcasts every 1.5s — a practical, in-the-browser view of the websocket fan-out. */
func PageHandler() melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        return melodyhttp.HtmlResponse(nethttp.StatusOK, page), nil
    }
}

const page = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>melody · websocket demo</title>
<style>
    :root { color-scheme: light dark; }
    * { box-sizing: border-box; }
    body {
        margin: 0; min-height: 100vh; display: grid; place-items: center;
        font-family: ui-sans-serif, system-ui, -apple-system, Segoe UI, Roboto, sans-serif;
        background: #0f172a; color: #e2e8f0;
    }
    .card {
        width: min(92vw, 30rem); padding: 2rem; border-radius: 1rem;
        background: #1e293b; box-shadow: 0 10px 30px rgba(0,0,0,.35);
    }
    h1 { margin: 0 0 .25rem; font-size: 1.15rem; font-weight: 600; }
    .sub { margin: 0 0 1.5rem; color: #94a3b8; font-size: .85rem; }
    .status { display: inline-flex; align-items: center; gap: .5rem; font-size: .8rem; margin-bottom: 1.25rem; }
    .dot { width: .6rem; height: .6rem; border-radius: 50%; background: #f59e0b; }
    .dot.on { background: #22c55e; } .dot.off { background: #ef4444; }
    .counter { font-size: 4.5rem; font-weight: 800; line-height: 1; letter-spacing: -.02em; color: #38bdf8; }
    .counter small { display: block; font-size: .8rem; font-weight: 500; color: #64748b; letter-spacing: 0; margin-top: .5rem; }
    ul { list-style: none; margin: 1.5rem 0 0; padding: 0; max-height: 9rem; overflow-y: auto; font-size: .8rem; color: #cbd5e1; }
    li { padding: .35rem 0; border-top: 1px solid #334155; font-variant-numeric: tabular-nums; }
</style>
</head>
<body>
<div class="card">
    <h1>websocket fan-out demo</h1>
    <p class="sub">the backend broadcasts a counter every 1.5s over <code>/ws</code>; every open tab receives it live.</p>
    <div class="status"><span id="dot" class="dot"></span><span id="state">connecting…</span></div>
    <div class="counter" id="counter">—<small id="stamp">waiting for the first tick</small></div>
    <ul id="log"></ul>
</div>
<script>
    var dot = document.getElementById('dot');
    var state = document.getElementById('state');
    var counter = document.getElementById('counter');
    var stamp = document.getElementById('stamp');
    var log = document.getElementById('log');

    function connect() {
        var proto = location.protocol === 'https:' ? 'wss' : 'ws';
        var socket = new WebSocket(proto + '://' + location.host + '/ws');

        socket.onopen = function () {
            dot.className = 'dot on';
            state.textContent = 'connected';
        };

        socket.onmessage = function (event) {
            var data;
            try { data = JSON.parse(event.data); } catch (e) { return; }

            counter.firstChild.nodeValue = data.counter;
            stamp.textContent = 'last tick at ' + data.at;

            var item = document.createElement('li');
            item.textContent = '#' + data.counter + '  ·  ' + data.at;
            log.insertBefore(item, log.firstChild);
            while (log.children.length > 8) { log.removeChild(log.lastChild); }
        };

        socket.onclose = function () {
            dot.className = 'dot off';
            state.textContent = 'disconnected — retrying…';
            setTimeout(connect, 1500);
        };
    }

    connect();
</script>
</body>
</html>`
