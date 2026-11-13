// Entry point of the script MyGo injects at document start, after the
// bridge, into the pages of a window whose page has Page.OnConsoleMessage
// listeners, and runs in the page that shows as the first listener comes
// (console.ts). The Go side wraps the bundle in a function that defines
// __MYGO_CONSOLE__.
import { hookConsole } from "./console";
import { transport } from "./transport";

declare const __MYGO_CONSOLE__: {
  /** Prefixes every message, as the bridge's. */
  secret: string;
  /** The size of the largest message, in bytes of UTF-8, the secret left out. */
  max: number;
};

(() => {
  const w = window as any;
  // Once per document: the page showing as the first listener comes may
  // have run the script at its start already.
  const installed = Symbol.for("mygo.console");
  if (w[installed]) return;
  const raw = transport(w);
  if (!raw) return;
  Object.defineProperty(w, installed, { value: true });
  const { secret, max } = __MYGO_CONSOLE__;
  hookConsole(w, (m) => raw.post(secret + m), max);
})();
