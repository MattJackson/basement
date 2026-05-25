// Side-effect-only module: install a localStorage shim on globalThis +
// window BEFORE any other test setup code runs. Must be imported as the
// first statement in src/tests/setup.ts so it executes before the i18n
// init (which captures the localStorage reference at module load time).
//
// Node 26 + Vitest 4 + jsdom 29 doesn't expose Node's experimental
// localStorage (gated on --localstorage-file), and jsdom's
// window.localStorage is undefined in non-DOM test files. This shim
// gives every test a working Storage implementation.

if (typeof (globalThis as { localStorage?: Storage }).localStorage === "undefined") {
  const store: Record<string, string> = {};
  const shim: Storage = {
    get length() {
      return Object.keys(store).length;
    },
    clear() {
      for (const k of Object.keys(store)) delete store[k];
    },
    getItem(key) {
      return Object.prototype.hasOwnProperty.call(store, key) ? store[key] ?? null : null;
    },
    key(index) {
      return Object.keys(store)[index] ?? null;
    },
    removeItem(key) {
      delete store[key];
    },
    setItem(key, value) {
      store[key] = String(value);
    },
  };
  (globalThis as { localStorage?: Storage }).localStorage = shim;
  if (typeof window !== "undefined") {
    Object.defineProperty(window, "localStorage", { value: shim, configurable: true });
  }
}
