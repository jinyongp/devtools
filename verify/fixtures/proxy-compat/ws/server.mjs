import http from 'node:http';
import { WebSocketServer } from 'ws';
import { allowRequest, publicOrigin } from '../shared/server-options.mjs';
const html = `<!doctype html><title>ws proxy fixture</title><h1 data-testid="marker">compat-before</h1><script>
window.__proxyProbe = {};
const socket = new WebSocket(location.origin.replace(/^http/, 'ws') + '/socket');
socket.onopen = () => { window.__proxyProbe.connected = true; socket.send('proxy-echo'); };
socket.onmessage = event => { window.__proxyProbe.reply = event.data; };
socket.onclose = event => { window.__proxyProbe.closed = true; window.__proxyProbe.closeCode = event.code; };
window.__proxySocket = socket;
</script>`;
const server = http.createServer((req, res) => {
  if (req.url === '/headers') {
    res.setHeader('Content-Type', 'application/json');
    res.end(JSON.stringify({ host: req.headers.host, origin: req.headers.origin, forwardedHost: req.headers['x-forwarded-host'], forwardedProto: req.headers['x-forwarded-proto'], authorizationPresent: !!req.headers.authorization, forwardedPresent: !!req.headers.forwarded }));
  } else { res.setHeader('Content-Type', 'text/html'); res.end(html); }
});
const sockets = new WebSocketServer({ noServer: true });
server.on('upgrade', (req, socket, head) => {
  if (!req.headers.origin || !allowRequest(req)) { socket.end('HTTP/1.1 403 Forbidden\r\nContent-Length: 0\r\n\r\n'); return; }
  sockets.handleUpgrade(req, socket, head, ws => sockets.emit('connection', ws, req));
});
sockets.on('connection', socket => socket.on('message', data => socket.send(data.toString())));
server.listen(Number(process.env.PROXY_COMPAT_PORT), '127.0.0.1', () => console.log('fixture ready ' + publicOrigin.origin));
