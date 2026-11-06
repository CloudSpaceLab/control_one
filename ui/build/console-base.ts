import type { Plugin, Connect } from 'vite';

// Run before Vite's base middleware so a router-generated root survives reload.
export const canonicalConsoleBase: Connect.NextHandleFunction = (req, res, next) => {
  const url = req.url ?? '';
  const queryAt = url.indexOf('?');
  const pathname = queryAt < 0 ? url : url.slice(0, queryAt);
  if ((req.method === 'GET' || req.method === 'HEAD') && pathname === '/console') {
    res.statusCode = 308;
    res.setHeader('Location', `/console/${queryAt < 0 ? '' : url.slice(queryAt)}`);
    res.end();
    return;
  }
  next();
};

export function consoleBaseRedirect(): Plugin {
  return {
    name: 'console-base-redirect',
    configureServer(server) { server.middlewares.use(canonicalConsoleBase); },
    configurePreviewServer(server) { server.middlewares.use(canonicalConsoleBase); },
  };
}
