import http from 'node:http';
import { Server } from 'socket.io';
import { allowRequest, publicOrigin } from '../shared/server-options.mjs';
const html = `<!doctype html><title>Socket.IO proxy fixture</title><h1 data-testid="marker">compat-before</h1><script src="/socket.io/socket.io.js"></script><script>
window.__proxyProbe = {};
const mode = new URLSearchParams(location.search).get('transport');
const socket = io({ transports: mode === 'websocket' ? ['websocket'] : ['polling', 'websocket'] });
socket.on('connect', () => {
  window.__proxyProbe.connected = true;
  window.__proxyProbe.connectCount = (window.__proxyProbe.connectCount || 0) + 1;
  socket.emit('probe', 'proxy-echo', reply => { window.__proxyProbe.reply = reply; });
});
socket.io.engine.on('upgrade', () => { window.__proxyProbe.upgraded = true; });
socket.on('disconnect', () => { window.__proxyProbe.closed = true; });
socket.on('connect_error', () => { window.__proxyProbe.rejected = true; });
window.__proxySocket = socket;
</script>`;
const server = http.createServer((req, res) => { res.setHeader('Content-Type', 'text/html'); res.end(html); });
const io = new Server(server, {
  cors: { origin: publicOrigin.origin },
  allowRequest: (req, callback) => callback(null, allowRequest(req)),
});
io.on('connection', socket => socket.on('probe', (value, ack) => ack(value)));
server.listen(Number(process.env.PROXY_COMPAT_PORT), '127.0.0.1', () => console.log('fixture ready ' + publicOrigin.origin));
