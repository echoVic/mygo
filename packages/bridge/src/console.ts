// What Page.OnConsoleMessage hears of a page: its console's calls, its
// uncaught errors and its unhandled rejections, which a script of MyGo's
// tells Go in pages whose window has a listener (console-script.ts). The
// script wraps the console's methods, which still do what they do, and
// listens for the window's error and unhandledrejection events: WKWebView,
// WebKitGTK and WebView2 have no event of their own for all three.
import { callSite, cut, errorHeader, formatArgs, isError, type Place, stackFrames, utf8Length } from "./console-format";

/** The level of a console message, as Go's ConsoleLevel. */
export type ConsoleLevel = "log" | "info" | "warn" | "error" | "debug";

/** A message posted to Go: its level, text, place and stack. */
export interface ConsoleMessage {
  t: "console";
  l: ConsoleLevel;
  /** The text. */
  v: string;
  /** The URL, line and column of the code that logged or threw. */
  u?: string;
  n?: number;
  c?: number;
  /** The frames of the stack, one per line. */
  s?: string;
}

/** The page's window, as the hook uses it. */
export interface Scope {
  console: Record<string, unknown>;
  addEventListener(type: "error" | "unhandledrejection", listener: (e: any) => void): void;
}

/** The console's methods the hook tells of, and their levels. */
const METHODS: Record<string, ConsoleLevel> = {
  log: "log",
  info: "info",
  warn: "warn",
  error: "error",
  debug: "debug",
  trace: "log",
  assert: "error",
};

/**
 * Wraps the console's methods of scope and listens for its uncaught errors
 * and rejections, posting each as a ConsoleMessage of at most max bytes of
 * UTF-8. A console call made while one is told (a toString that logs)
 * reaches the console alone.
 */
export function hookConsole(scope: Scope, post: (message: string) => void, max: number): void {
  let busy = false;
  const tell = (make: () => ConsoleMessage | null) => {
    if (busy) return;
    busy = true;
    try {
      const m = make();
      const s = m && encode(m, max);
      if (s) post(s);
    } catch {
      // A value that can't be told, or the page being torn down.
    } finally {
      busy = false;
    }
  };
  const target = scope.console;
  for (const [name, level] of Object.entries(METHODS)) {
    const original = target[name];
    if (typeof original !== "function") continue;
    const wrapper = function (this: unknown, ...args: unknown[]) {
      // Made here, so that the frame after this one is the caller's.
      const stack = new Error().stack;
      tell(() => call(name, level, args, stack, max));
      return original.apply(this, args);
    };
    // Named as the method it wraps, as the console's own are.
    Object.defineProperty(wrapper, "name", { value: name });
    target[name] = wrapper;
  }
  scope.addEventListener("error", (e) => tell(() => uncaught(e, max)));
  scope.addEventListener("unhandledrejection", (e) => tell(() => rejection(e, max)));
}

/** The message of a console call; stack is the wrapper's. */
function call(name: string, level: ConsoleLevel, args: unknown[], stack: string | undefined, max: number): ConsoleMessage | null {
  let text: string;
  let frames = "";
  if (name === "assert") {
    if (args[0]) return null;
    text = args.length > 1 ? "Assertion failed: " + formatArgs(args.slice(1), max) : "Assertion failed";
  } else {
    text = formatArgs(args, max);
    if (name === "trace") {
      text ||= "console.trace";
      frames = stackFrames(stack, "Error").split("\n").slice(1).join("\n");
    }
  }
  return message(level, text, callSite(stack, 1), frames, max);
}

/** The message of an error event: an uncaught error, or reportError(). */
function uncaught(e: any, max: number): ConsoleMessage | null {
  if (!e || (typeof e.message !== "string" && e.error === undefined)) return null; // not an ErrorEvent
  const error = e.error;
  let text: string;
  if (error !== undefined && error !== null) {
    text = "Uncaught " + (isError(error) ? errorHeader(error) : formatArgs([error], max));
  } else {
    // Another origin's script tells no more ("Script error.").
    text = String(e.message);
    if (!text.startsWith("Uncaught")) text = "Uncaught " + text;
  }
  const frames = isError(error) ? stackFrames(error.stack, errorHeader(error)) : "";
  const place: Place | null = e.filename ? { url: String(e.filename), line: Number(e.lineno) || 0, column: Number(e.colno) || 0 } : callSite(frames, 0);
  return message("error", text, place, frames, max);
}

/** The message of a promise rejected with no handler. */
function rejection(e: any, max: number): ConsoleMessage | null {
  if (!e || !("reason" in e)) return null;
  const reason = e.reason;
  const error = isError(reason);
  const frames = error ? stackFrames(reason.stack, errorHeader(reason)) : "";
  const text = "Uncaught (in promise) " + (error ? errorHeader(reason) : formatArgs([reason], max));
  return message("error", text, callSite(frames, 0), frames, max);
}

function message(level: ConsoleLevel, text: string, place: Place | null, frames: string, max: number): ConsoleMessage {
  const m: ConsoleMessage = { t: "console", l: level, v: cut(text, max) };
  if (place) {
    if (place.url) m.u = cut(place.url, max);
    if (place.line) m.n = place.line;
    if (place.column) m.c = place.column;
  }
  if (frames) m.s = cut(frames, max);
  return m;
}

/**
 * Encodes m in at most max bytes of UTF-8, or returns null when even its
 * level does not fit: the text, the stack and the URL share the room left,
 * each cut with "…" where it doesn't fit in its share.
 */
export function encode(m: ConsoleMessage, max: number): string | null {
  let json = JSON.stringify(m);
  if (utf8Length(json) <= max) return json;
  const keys = (["u", "s", "v"] as const).filter((k) => m[k] !== undefined);
  const empty: ConsoleMessage = { ...m };
  for (const k of keys) empty[k] = "";
  let room = max - utf8Length(JSON.stringify(empty));
  if (room < 0) return null;
  // The smallest parts first, so that what they leave of their shares goes
  // to the larger ones.
  const sizes = new Map(keys.map((k) => [k, jsonBytes(m[k]!)] as const));
  keys.sort((a, b) => sizes.get(a)! - sizes.get(b)!);
  keys.forEach((k, i) => {
    const share = Math.floor(room / (keys.length - i));
    if (sizes.get(k)! > share) m[k] = cutJSON(m[k]!, share);
    room -= jsonBytes(m[k]!);
  });
  json = JSON.stringify(m);
  return utf8Length(json) <= max ? json : null;
}

/** The bytes s takes in JSON (UTF-8), its quotes left out. */
function jsonBytes(s: string): number {
  let n = 0;
  for (let i = 0; i < s.length; i++) {
    const w = unitBytes(s, i);
    n += w;
    if (w === 4) i++;
  }
  return n;
}

/** The longest start of s that takes at most room bytes of JSON with "…". */
function cutJSON(s: string, room: number): string {
  room -= 3; // "…"
  let n = 0;
  let i = 0;
  while (i < s.length) {
    const w = unitBytes(s, i);
    if (n + w > room) break;
    n += w;
    i += w === 4 ? 2 : 1;
  }
  return i === 0 && room < 0 ? "" : s.slice(0, i) + "…";
}

/** The bytes JSON.stringify writes for the code unit of s at i: 4 for a surrogate pair. */
function unitBytes(s: string, i: number): number {
  const c = s.charCodeAt(i);
  if (c === 0x22 || c === 0x5c || c === 0x08 || c === 0x0c || c === 0x0a || c === 0x0d || c === 0x09) return 2;
  if (c < 0x20) return 6;
  if (c < 0x80) return 1;
  if (c < 0x800) return 2;
  if (c >= 0xd800 && c < 0xdc00 && (s.charCodeAt(i + 1) & 0xfc00) === 0xdc00) return 4;
  if (c >= 0xd800 && c < 0xe000) return 6; // a lone surrogate, escaped
  return 3;
}
