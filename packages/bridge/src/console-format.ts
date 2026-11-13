// The text of console messages for Page.OnConsoleMessage, as the web
// inspector shows them: a first string's substitutions, strings as they
// are, and other values as previews that read no getter, cut at a length so
// that a large value logged in a loop costs the page little and Go less.

/** How deep previews of objects go: deeper ones are {…} and […]. */
const MAX_DEPTH = 2;
/** How many items, entries or keys of one object a preview shows. */
const MAX_ITEMS = 100;
/** Marks where text was cut. */
const ELLIPSIS = "…";

const VOID_ELEMENTS = new Set(["area", "base", "br", "col", "embed", "hr", "img", "input", "link", "meta", "source", "track", "wbr"]);

/** Text of at most max UTF-16 code units, and the marker of a cut. */
class Out {
  s = "";
  constructor(readonly max: number) {}
  get full(): boolean {
    return this.s.length >= this.max;
  }
  /** Room left. */
  get room(): number {
    return this.max - this.s.length;
  }
  add(t: string): void {
    if (!this.full) this.s += t.length > this.room ? t.slice(0, this.room + 1) : t;
  }
}

/**
 * Formats the arguments of a console call as the inspector shows them, in
 * at most max UTF-16 code units, cut with "…": a first string with more
 * arguments substitutes %s, %d, %i, %f, %o, %O and %c (whose styles show
 * nothing) and turns %% into %, and the rest follow, separated by spaces.
 */
export function formatArgs(args: readonly unknown[], max: number): string {
  const out = new Out(max);
  let next = 0;
  const first = args[0];
  if (typeof first === "string" && args.length > 1) {
    next = 1;
    let from = 0;
    for (let i = first.indexOf("%"); i >= 0 && i < first.length - 1 && !out.full; i = first.indexOf("%", i)) {
      const c = first[i + 1]!;
      if (c === "%") {
        out.add(first.slice(from, i) + "%");
        from = i += 2;
        continue;
      }
      if (!"sdifoOc".includes(c) || next >= args.length) {
        i++;
        continue;
      }
      out.add(first.slice(from, i));
      const arg = args[next++];
      switch (c) {
        case "s":
        case "o":
        case "O":
          inspect(arg, out, 0, [], c === "s");
          break;
        case "d":
        case "i":
          out.add(toNumber(arg, (s) => parseInt(s, 10)));
          break;
        case "f":
          out.add(toNumber(arg, parseFloat));
          break;
      }
      from = i += 2;
    }
    out.add(first.slice(from));
  }
  for (let i = next; i < args.length && !out.full; i++) {
    if (i > 0) out.add(" ");
    inspect(args[i], out, 0, [], true);
  }
  return cut(out.s, max);
}

function number(n: number): string {
  return Object.is(n, -0) ? "-0" : String(n);
}

/** %d, %i and %f: the number parse reads of v, NaN for a symbol. */
function toNumber(v: unknown, parse: (s: string) => number): string {
  try {
    return typeof v === "symbol" ? "NaN" : number(parse(v as string));
  } catch {
    return "NaN"; // a toString that throws
  }
}

/**
 * Writes the preview of v: at the top level (top), strings as they are and
 * errors with their stacks. ancestors are the objects v is in, for cycles.
 */
function inspect(v: unknown, out: Out, depth: number, ancestors: object[], top: boolean): void {
  try {
    switch (typeof v) {
      case "string":
        out.add(top ? v : JSON.stringify(v.length > out.room ? v.slice(0, out.room) : v));
        return;
      case "number":
        out.add(number(v));
        return;
      case "bigint":
        out.add(`${v}n`);
        return;
      case "symbol":
        out.add(v.toString());
        return;
      case "function":
        out.add(functionText(v));
        return;
      case "object":
        if (v === null) break;
        if (ancestors.includes(v)) {
          out.add("[Circular]");
          return;
        }
        ancestors.push(v);
        try {
          object(v, out, depth, ancestors, top);
        } finally {
          ancestors.pop();
        }
        return;
    }
    out.add(String(v));
  } catch {
    out.add("[object]"); // a revoked proxy, a frame of another origin
  }
}

function object(v: object, out: Out, depth: number, ancestors: object[], top: boolean): void {
  if (isError(v)) {
    out.add(top ? errorText(v) : errorHeader(v));
    return;
  }
  if (typeof Node === "function" && v instanceof Node) {
    out.add(nodeText(v));
    return;
  }
  if (v === globalThis) {
    out.add("Window");
    return;
  }
  if (v instanceof Date) {
    out.add(Number.isNaN(v.getTime()) ? "Invalid Date" : v.toISOString());
    return;
  }
  if (v instanceof RegExp) {
    out.add(String(v));
    return;
  }
  const collapsed = depth > MAX_DEPTH;
  if (Array.isArray(v) || ArrayBuffer.isView(v)) {
    const typed = !Array.isArray(v);
    const items = v as ArrayLike<unknown>;
    if (typed) out.add(`${constructorName(v)}(${items.length}) `);
    if (collapsed) return out.add("[…]");
    out.add("[");
    for (let i = 0, shown = 0; i < items.length && !out.full; shown++) {
      if (shown === MAX_ITEMS) {
        out.add(`, … ${items.length - i} more items`);
        break;
      }
      if (shown > 0) out.add(", ");
      if (typed || i in items) {
        inspect(items[i], out, depth + 1, ancestors, false);
        i++;
        continue;
      }
      const next = nextIndex(items, i);
      out.add(`<${next - i} empty item${next - i === 1 ? "" : "s"}>`);
      i = next;
    }
    return out.add("]");
  }
  if (v instanceof Map || v instanceof Set) {
    const map = v instanceof Map;
    out.add(`${map ? "Map" : "Set"}(${v.size}) `);
    if (collapsed) return out.add("{…}");
    out.add("{");
    let shown = 0;
    for (const entry of map ? Map.prototype.entries.call(v) : Set.prototype.values.call(v)) {
      if (out.full) break;
      if (shown > 0) out.add(", ");
      if (shown++ === MAX_ITEMS) {
        out.add("…");
        break;
      }
      if (map) {
        const [key, value] = entry as [unknown, unknown];
        inspect(key, out, depth + 1, ancestors, false);
        out.add(" => ");
        inspect(value, out, depth + 1, ancestors, false);
      } else {
        inspect(entry, out, depth + 1, ancestors, false);
      }
    }
    return out.add("}");
  }
  const name = constructorName(v);
  if (name && name !== "Object") out.add(name + " ");
  if (collapsed) return out.add("{…}");
  out.add("{");
  const keys: (string | symbol)[] = Object.keys(v);
  for (const s of Object.getOwnPropertySymbols(v)) {
    if (Object.prototype.propertyIsEnumerable.call(v, s)) keys.push(s);
  }
  for (let i = 0; i < keys.length && !out.full; i++) {
    if (i > 0) out.add(", ");
    if (i === MAX_ITEMS) {
      out.add("…");
      break;
    }
    const key = keys[i]!;
    out.add(typeof key === "symbol" ? `[${key.toString()}]` : /^[A-Za-z_$][\w$]*$/.test(key) ? key : JSON.stringify(key));
    out.add(": ");
    // Getters are the page's code, which a preview does not run.
    const d = Object.getOwnPropertyDescriptor(v, key);
    if (d && (d.get || d.set)) {
      out.add(d.get && d.set ? "[Getter/Setter]" : d.get ? "[Getter]" : "[Setter]");
    } else {
      inspect(d?.value, out, depth + 1, ancestors, false);
    }
  }
  out.add("}");
}

/**
 * The first index from on that array a has, or its length: sparse arrays,
 * as new Array(1e9), are searched by their keys rather than index by index.
 */
function nextIndex(a: ArrayLike<unknown>, from: number): number {
  const end = Math.min(a.length, from + 1000);
  for (let i = from; i < end; i++) if (i in a) return i;
  if (end === a.length) return end;
  for (const k of Object.keys(a)) {
    const i = Number(k);
    if (i >= from && i < a.length) return i; // indices come first, in order
  }
  return a.length;
}

/** The name of v's constructor, without running a getter, "" for none. */
function constructorName(v: object): string {
  const proto = Object.getPrototypeOf(v);
  if (!proto) return "";
  const ctor = Object.getOwnPropertyDescriptor(proto, "constructor")?.value;
  return typeof ctor === "function" && typeof ctor.name === "string" ? ctor.name : "";
}

function functionText(fn: Function): string {
  const name = typeof fn.name === "string" ? fn.name : "";
  if (/^class[\s{]/.test(Function.prototype.toString.call(fn))) return `[class ${name || "(anonymous)"}]`;
  return name ? `[Function: ${name}]` : "[Function (anonymous)]";
}

function nodeText(n: Node): string {
  switch (n.nodeType) {
    case 1: {
      const el = n as Element;
      const tag = el.localName;
      let s = "<" + tag;
      const attrs = el.attributes;
      for (let i = 0; i < attrs.length && i < MAX_ITEMS; i++) {
        const a = attrs[i]!;
        s += ` ${a.name}="${a.value.replace(/&/g, "&amp;").replace(/"/g, "&quot;")}"`;
      }
      if (VOID_ELEMENTS.has(tag)) return s + ">";
      return s + ">" + (el.hasChildNodes() ? ELLIPSIS : "") + `</${tag}>`;
    }
    case 3:
      return "#text " + JSON.stringify((n as Text).data);
    case 8:
      return `<!--${(n as Comment).data}-->`;
    default:
      return n.nodeName;
  }
}

/** Whether v is an Error, of this page or another frame's. */
export function isError(v: unknown): v is Error {
  return v instanceof Error || /^\[object (Error|DOMException)\]$/.test(Object.prototype.toString.call(v));
}

/** "TypeError: message", as an error's toString, without running it. */
export function errorHeader(e: Error): string {
  const name = typeof e.name === "string" && e.name ? e.name : "Error";
  const message = typeof e.message === "string" ? e.message : "";
  return message ? `${name}: ${message}` : name;
}

/** The header of an error and its stack's frames, as the inspector shows it. */
function errorText(e: Error): string {
  const header = errorHeader(e);
  const frames = stackFrames(e.stack, header);
  return frames ? header + "\n" + frames : header;
}

/**
 * The frames of a stack, one per line, trimmed: V8's (WebView2) starts with
 * the error's header, which is left out, and its frames read
 * "at f (url:line:column)"; JavaScriptCore's (WebKit) are "f@url:line:column".
 */
export function stackFrames(stack: unknown, header: string): string {
  if (typeof stack !== "string") return "";
  let rest = stack;
  if (rest.startsWith(header + "\n") || rest === header) {
    rest = rest.slice(header.length);
  } else {
    const at = rest.search(/^\s+at /m);
    if (at > 0) rest = rest.slice(at);
  }
  return rest
    .split("\n")
    .map((l) => l.trim())
    .filter(Boolean)
    .join("\n");
}

/** Where a frame's code is. */
export interface Place {
  url: string;
  line: number;
  column: number;
}

/**
 * The place of the first frame of stack, after the first skip frames, that
 * has one: frames of the engine's own code ("[native code]", V8's
 * "<anonymous>") and of evals have none.
 */
export function callSite(stack: unknown, skip: number): Place | null {
  const frames = stackFrames(stack, "Error").split("\n").slice(skip);
  for (const frame of frames) {
    const m = /(?:^|[\s(@])([^\s()@]+):(\d+):(\d+)\)?$/.exec(frame);
    if (m && m[1] !== "<anonymous>") return { url: m[1]!, line: Number(m[2]), column: Number(m[3]) };
  }
  return null;
}

/** s cut to n UTF-16 code units, with "…" where it was cut; "" for n <= 0. */
export function cut(s: string, n: number): string {
  if (s.length <= n) return s;
  if (n <= 0) return "";
  const c = s.charCodeAt(n - 1);
  return s.slice(0, c >= 0xd800 && c < 0xdc00 ? n - 1 : n) + ELLIPSIS;
}

/** The length of s in UTF-8, a lone surrogate counted as the U+FFFD Go reads. */
export function utf8Length(s: string): number {
  let n = 0;
  for (let i = 0; i < s.length; i++) {
    const c = s.charCodeAt(i);
    if (c < 0x80) n += 1;
    else if (c < 0x800) n += 2;
    else if (c >= 0xd800 && c < 0xdc00 && i + 1 < s.length && (s.charCodeAt(i + 1) & 0xfc00) === 0xdc00) {
      n += 4;
      i++;
    } else n += 3;
  }
  return n;
}
