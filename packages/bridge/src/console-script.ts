// Entry point of the script MyGo injects at document start, after the
// bridge, into the pages of a window whose page has Page.OnConsoleMessage
// listeners, and runs in the page that shows as the first listener comes
// (console.ts). The Go side wraps the bundle in a function that defines
// __MYGO_CONSOLE__.
import { type Hook, hookConsole } from "./console";
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
  // have run the script at its start already, or a listener before, whose
  // going muted the hook (internal/bridge's ConsoleMuteScript).
  const installed = Symbol.for("mygo.console");
  const hook: Hook | undefined = w[installed];
  if (hook) {
    hook.muted = false;
    return;
  }
  const raw = transport(w);
  if (!raw) return;
  const { secret, max } = __MYGO_CONSOLE__;
  Object.defineProperty(w, installed, { value: hookConsole(w, (m) => raw.post(secret + m), max) });
})();
