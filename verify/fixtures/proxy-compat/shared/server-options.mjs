export const publicOrigin = new URL(process.env.PROXY_COMPAT_ORIGIN);
export const authority = publicOrigin.host;
export const serverOptions = {
  host: '127.0.0.1',
  port: Number(process.env.PROXY_COMPAT_PORT),
  strictPort: true,
  allowedHosts: [publicOrigin.hostname],
  hmr: {
    protocol: publicOrigin.protocol === 'https:' ? 'wss' : 'ws',
    host: publicOrigin.hostname,
    clientPort: Number(publicOrigin.port || (publicOrigin.protocol === 'https:' ? 443 : 80)),
  },
};

// Browsers always send Origin on WebSocket upgrades. Same-origin polling GETs
// may omit it; a supplied Origin must match the one configured public origin.
export function allowRequest(request) {
  return request.headers.host === authority
    && (request.headers.origin === undefined || request.headers.origin === publicOrigin.origin);
}
