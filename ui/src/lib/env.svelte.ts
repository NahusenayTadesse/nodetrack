// Small reactive environment signals shared across components.

const darkQuery = window.matchMedia('(prefers-color-scheme: dark)');

class Env {
  /** current unix time in seconds, ticking once per second */
  now = $state(Math.floor(Date.now() / 1000));
  /** true when the OS / browser prefers a dark color scheme */
  dark = $state(darkQuery.matches);

  constructor() {
    setInterval(() => (this.now = Math.floor(Date.now() / 1000)), 1000);
    darkQuery.addEventListener('change', (e) => (this.dark = e.matches));
  }
}

export const env = new Env();

/** Read a CSS custom property from :root (used to theme canvas charts). */
export function cssVar(name: string): string {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim();
}
