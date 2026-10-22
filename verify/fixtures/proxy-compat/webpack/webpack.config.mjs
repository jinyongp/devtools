import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { publicOrigin } from '../shared/server-options.mjs';
const root = path.dirname(fileURLToPath(import.meta.url));
export default {
  mode: 'development',
  entry: path.join(root, 'src/main.js'),
  output: { filename: 'bundle.js', path: path.join(root, 'dist') },
  devServer: {
    host: '127.0.0.1', port: Number(process.env.PROXY_COMPAT_PORT),
    allowedHosts: [publicOrigin.hostname], hot: true,
    static: { directory: path.join(root, 'public') },
    client: { webSocketURL: { protocol: publicOrigin.protocol === 'https:' ? 'wss' : 'ws', hostname: publicOrigin.hostname, port: Number(publicOrigin.port || (publicOrigin.protocol === 'https:' ? 443 : 80)) } },
  },
};
