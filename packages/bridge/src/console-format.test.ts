import { afterAll, beforeAll, describe, expect, test } from "bun:test";
import { callSite, cut, formatArgs, stackFrames, utf8Length } from "./console-format";

const MAX = 1000;
const f = (...args: unknown[]) => formatArgs(args, MAX);

describe("formatArgs", () => {
  test("joins its arguments with spaces, strings as they are", () => {
    expect(f("a", "b c")).toBe("a b c");
    expect(f()).toBe("");
    expect(f(1, -0, 2.5, NaN, 10n, true, null, undefined)).toBe("1 -0 2.5 NaN 10n true null undefined");
    expect(f(Symbol("s"))).toBe("Symbol(s)");
  });

  test("substitutes a first string's %s, %d, %i, %f, %o, %O and %c", () => {
    expect(f("%s is %d years, %i%%", "Bun", 2.9, "7x")).toBe("Bun is 2 years, 7%");
    expect(f("100%%")).toBe("100%%"); // a string alone is as it is
    expect(f("%f and %d", "1.5", Symbol("x"))).toBe("1.5 and NaN");
    expect(f("%o / %O", { a: 1 }, [1])).toBe("{a: 1} / [1]");
    expect(f("%cstyled%c text", "color: red", "")).toBe("styled text");
    expect(f("%s %s", "only")).toBe("only %s");
    expect(f("%s", "a", "rest", 1)).toBe("a rest 1");
    expect(f("100%", 5)).toBe("100% 5");
    expect(f("%x %s", "y")).toBe("%x y");
    expect(f(1, "%s", 2)).toBe("1 %s 2"); // only a first string substitutes
  });

  test("formats objects and arrays as previews", () => {
    expect(f({ a: 1, "b-c": "x", [Symbol("k")]: true })).toBe('{a: 1, "b-c": "x", [Symbol(k)]: true}');
    expect(f({})).toBe("{}");
    expect(f([1, "two", [3, [4, [5, [6]]]]])).toBe('[1, "two", [3, [4, […]]]]');
    expect(f({ a: { b: { c: { d: 1 } } } })).toBe("{a: {b: {c: {…}}}}");
    expect(f([, 1, , , 2])).toBe("[<1 empty item>, 1, <2 empty items>, 2]");
    const sparse: number[] = [];
    sparse[5000] = 1;
    expect(f(sparse)).toBe("[<5000 empty items>, 1]");
    expect(f(new Array(1e9))).toBe("[<1000000000 empty items>]"); // without visiting each
    expect(f(new Uint8Array([1, 2]))).toBe("Uint8Array(2) [1, 2]");
    class Point {
      x = 1;
      y = 2;
    }
    expect(f(new Point())).toBe("Point {x: 1, y: 2}");
    expect(f(Object.create(null))).toBe("{}");
  });

  test("shows a cycle as [Circular]", () => {
    const a: any = { name: "a" };
    a.self = a;
    a.list = [a];
    expect(f(a)).toBe('{name: "a", self: [Circular], list: [[Circular]]}');
    const shared = { x: 1 };
    expect(f([shared, shared])).toBe("[{x: 1}, {x: 1}]"); // seen twice, not a cycle
  });

  test("does not run getters", () => {
    let ran = false;
    const o = {
      get g() {
        ran = true;
        return 1;
      },
      set s(_: number) {},
      get gs() {
        return 1;
      },
      set gs(_: number) {},
    };
    expect(f(o)).toBe("{g: [Getter], s: [Setter], gs: [Getter/Setter]}");
    expect(ran).toBe(false);
  });

  test("formats maps, sets, dates, regular expressions and functions", () => {
    expect(f(new Map<unknown, unknown>([["a", 1], [{ k: 1 }, [2]]]))).toBe('Map(2) {"a" => 1, {k: 1} => [2]}');
    expect(f(new Set([1, "x"]))).toBe('Set(2) {1, "x"}');
    expect(f(new Date(Date.UTC(2026, 10, 13, 2, 3, 4)))).toBe("2026-11-13T02:03:04.000Z");
    expect(f(new Date(NaN))).toBe("Invalid Date");
    expect(f(/a+b/gi)).toBe("/a+b/gi");
    function named() {}
    expect(f(named, () => {}, class Foo {})).toBe("[Function: named] [Function (anonymous)] [class Foo]");
    expect(f({ fn: named })).toBe("{fn: [Function: named]}");
  });

  test("formats an error with its stack, and nested errors by their name and message", () => {
    const e = new TypeError("bad thing");
    e.stack = "TypeError: bad thing\n    at f (https://app.test/a.js:1:2)\n    at https://app.test/a.js:3:4";
    expect(f(e)).toBe("TypeError: bad thing\nat f (https://app.test/a.js:1:2)\nat https://app.test/a.js:3:4");
    expect(f("failed:", e)).toBe("failed: TypeError: bad thing\nat f (https://app.test/a.js:1:2)\nat https://app.test/a.js:3:4");
    const jsc = new Error("boom");
    jsc.stack = "f@https://app.test/a.js:1:2\nglobal code@https://app.test/a.js:3:4";
    expect(f(jsc)).toBe("Error: boom\nf@https://app.test/a.js:1:2\nglobal code@https://app.test/a.js:3:4");
    const bare = new Error("");
    bare.stack = "";
    expect(f(bare)).toBe("Error");
    expect(f({ err: e })).toBe("{err: TypeError: bad thing}");
  });

  test("cuts long text, and stops looking at large values", () => {
    expect(formatArgs(["x".repeat(50)], 10)).toBe("xxxxxxxxxx…");
    const big = Array.from({ length: 1e6 }, (_, i) => i);
    const s = formatArgs([big], 100);
    expect(s.startsWith("[0, 1, 2,")).toBe(true);
    expect(s.length).toBeLessThanOrEqual(101);
    expect(f(Array.from({ length: 120 }, () => 0))).toEndWith(", 0, … 20 more items]");
    const many = Object.fromEntries(Array.from({ length: 120 }, (_, i) => [`k${i}`, i]));
    expect(f(many)).toEndWith(", k99: 99, …}");
    expect(f({ s: "y".repeat(5000) }).length).toBeLessThanOrEqual(MAX + 1);
  });

  test("survives values that throw", () => {
    const { proxy, revoke } = Proxy.revocable({}, {});
    revoke();
    expect(f("a", proxy, "b")).toBe("a [object] b");
    const hostile = {
      toString() {
        throw new Error("no");
      },
    };
    expect(f("%s", Object.setPrototypeOf(hostile, null))).toBe("{toString: [Function: toString]}");
  });
});

describe("DOM nodes", () => {
  class FakeNode {
    constructor(
      readonly nodeType: number,
      readonly nodeName: string,
      readonly props: Record<string, unknown> = {},
    ) {
      Object.assign(this, props);
    }
  }
  const g = globalThis as any;
  beforeAll(() => {
    g.Node = FakeNode;
  });
  afterAll(() => {
    delete g.Node;
  });

  test("shows elements as their tag, text as its text", () => {
    const attrs = [
      { name: "id", value: "app" },
      { name: "class", value: 'a "b"' },
    ];
    const div = new FakeNode(1, "DIV", { localName: "div", attributes: attrs, hasChildNodes: () => true });
    expect(f(div)).toBe('<div id="app" class="a &quot;b&quot;">…</div>');
    const img = new FakeNode(1, "IMG", { localName: "img", attributes: [], hasChildNodes: () => false });
    expect(f(img)).toBe("<img>");
    const p = new FakeNode(1, "P", { localName: "p", attributes: [], hasChildNodes: () => false });
    expect(f(p)).toBe("<p></p>");
    expect(f(new FakeNode(3, "#text", { data: "hi" }))).toBe('#text "hi"');
    expect(f(new FakeNode(9, "#document"))).toBe("#document");
    expect(f({ el: div })).toBe('{el: <div id="app" class="a &quot;b&quot;">…</div>}');
  });
});

describe("stacks", () => {
  test("stackFrames drops V8's header, and trims each frame", () => {
    expect(stackFrames("Error: a\nb\n    at f (x.js:1:2)\n    at x.js:3:4", "Error: a\nb")).toBe("at f (x.js:1:2)\nat x.js:3:4");
    expect(stackFrames("Error: changed\n    at f (x.js:1:2)", "Error: a")).toBe("at f (x.js:1:2)");
    expect(stackFrames("f@x.js:1:2\n@x.js:3:4\n", "Error: a")).toBe("f@x.js:1:2\n@x.js:3:4");
    expect(stackFrames("Error: a", "Error: a")).toBe(""); // no frames
    expect(stackFrames(undefined, "Error")).toBe("");
  });

  test("callSite finds the first frame with a place after those skipped", () => {
    const v8 = "Error\n    at console.log (<anonymous>:1:10)\n    at Array.forEach (<anonymous>)\n    at go (https://app.test/main.js:12:9)";
    expect(callSite(v8, 1)).toEqual({ url: "https://app.test/main.js", line: 12, column: 9 });
    // Code of an eval has no URL: the call site is the eval's.
    const evaled = "Error\n    at log (x)\n    at eval (eval at run (https://app.test/a.js:4:5), <anonymous>:1:9)\n    at run (https://app.test/a.js:4:5)";
    expect(callSite(evaled, 1)).toEqual({ url: "https://app.test/a.js", line: 4, column: 5 });
    const jsc = "log@\napply@[native code]\nglobal code@https://app.test/:7:20";
    expect(callSite(jsc, 1)).toEqual({ url: "https://app.test/", line: 7, column: 20 });
    expect(callSite("@http://localhost:5173/src/main.ts?t=1:3:14", 0)).toEqual({ url: "http://localhost:5173/src/main.ts?t=1", line: 3, column: 14 });
    expect(callSite("Error\n    at new Foo (file:///C:/app/a.js:2:3)", 0)).toEqual({ url: "file:///C:/app/a.js", line: 2, column: 3 });
    expect(callSite("Error\n    at async load (mygo://localhost/x.js:5:6)", 0)).toEqual({ url: "mygo://localhost/x.js", line: 5, column: 6 });
    expect(callSite("log@\n", 1)).toBeNull();
    expect(callSite(undefined, 0)).toBeNull();
  });
});

describe("cut and utf8Length", () => {
  test("cut keeps n code units and marks the cut, never splitting a pair", () => {
    expect(cut("abcdef", 3)).toBe("abc…");
    expect(cut("abc", 3)).toBe("abc");
    expect(cut("abc", 0)).toBe("");
    expect(cut("a😀b", 2)).toBe("a…");
  });

  test("utf8Length counts the bytes of UTF-8", () => {
    expect(utf8Length("abc")).toBe(3);
    expect(utf8Length("é")).toBe(2);
    expect(utf8Length("…")).toBe(3);
    expect(utf8Length("😀")).toBe(4);
    expect(utf8Length("\ud800")).toBe(3); // as U+FFFD
  });
});
