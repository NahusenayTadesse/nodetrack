export type Route = { name: 'overview' } | { name: 'app'; id: number } | { name: 'notfound'; path: string };

function parse(hash: string): Route {
  const path = hash.replace(/^#/, '') || '/';
  if (path === '/' || path === '') return { name: 'overview' };
  const m = /^\/app\/(\d+)\/?$/.exec(path);
  if (m) return { name: 'app', id: Number(m[1]) };
  return { name: 'notfound', path };
}

class Router {
  route = $state<Route>(parse(location.hash));

  constructor() {
    window.addEventListener('hashchange', () => {
      this.route = parse(location.hash);
      window.scrollTo({ top: 0 });
    });
  }

  go(path: string) {
    location.hash = path;
  }
}

export const router = new Router();

export const appHref = (id: number) => `#/app/${id}`;
export const homeHref = '#/';
