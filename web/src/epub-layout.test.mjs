import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import ts from "typescript";

const source = await readFile(new URL("./epub-layout.ts", import.meta.url), "utf8");
const js = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ES2020, target: ts.ScriptTarget.ES2020 } }).outputText;
const { placeEpubPage } = await import(`data:text/javascript;base64,${Buffer.from(js).toString("base64")}`);

function columnDocument(fullWidth, translation = -3066) {
  const properties = new Map([["transform", [`translateX(${translation}px)`, "important"]]]);
  const style = {
    getPropertyValue: (name) => properties.get(name)?.[0] ?? "",
    getPropertyPriority: (name) => properties.get(name)?.[1] ?? "",
    setProperty: (name, value, priority = "") => properties.set(name, [value, priority]),
    removeProperty: (name) => properties.delete(name),
  };
  const offset = () => Number(style.getPropertyValue("transform").match(/translateX\((-?[\d.]+)px\)/)?.[1] ?? 0);
  const width = () => Math.max(1067, fullWidth + offset());
  const body = { style, get scrollWidth() { return width(); }, getBoundingClientRect: () => ({ left: offset() }) };
  const doc = { body, documentElement: { get scrollWidth() { return width(); } } };
  const target = { getClientRects: () => [{ left: 5691 + offset() }] };
  return { doc, target, style, setFullWidth: (next) => { fullWidth = next; } };
}

test("a footnote on the sixth spread stays reachable after a translated layout is measured again", () => {
  // Actual Jin Ou Que metrics: untranslated width 6143; translated width 3077.
  const { doc, target, style } = columnDocument(6143);
  assert.equal(doc.body.scrollWidth, 3077);
  const options = { viewportWidth: 1067, pageWidth: 1022, pagePosition: 1, target };
  assert.deepEqual(placeEpubPage(doc, options), { pageCount: 6, pagePosition: 5 });
  assert.equal(style.getPropertyValue("transform"), "translateX(-5110px)");
  assert.equal(style.getPropertyValue("transition"), "");
  // React metrics and the existing 80ms/ResizeObserver layout pass must not
  // clamp the destination backwards once its one-shot fragment was consumed.
  assert.deepEqual(placeEpubPage(doc, { ...options, target: undefined, pagePosition: 5 }), { pageCount: 6, pagePosition: 5 });
});

test("return navigation and real viewport changes use fresh complete column width", () => {
  const { doc, style, setFullWidth } = columnDocument(6143, -5110);
  style.setProperty("transition", "transform 140ms ease", "important");
  assert.deepEqual(placeEpubPage(doc, { viewportWidth: 1067, pageWidth: 1022, pagePosition: 1 }), { pageCount: 6, pagePosition: 1 });
  assert.equal(style.getPropertyValue("transition"), "transform 140ms ease");
  assert.equal(style.getPropertyPriority("transition"), "important");
  setFullWidth(2800);
  assert.deepEqual(placeEpubPage(doc, { viewportWidth: 1067, pageWidth: 1022, pagePosition: 5 }), { pageCount: 3, pagePosition: 2 });
});

test("one viewport and its side padding do not create an empty extra spread", () => {
  const { doc } = columnDocument(1067, 0);
  assert.deepEqual(placeEpubPage(doc, { viewportWidth: 1067, pageWidth: 1022, pagePosition: 0 }), { pageCount: 1, pagePosition: 0 });
});

test("failed measurement restores the prior transform and transition", () => {
  const { doc, style } = columnDocument(6143);
  const target = { getClientRects() { throw new Error("detached target"); } };
  assert.throws(() => placeEpubPage(doc, { viewportWidth: 1067, pageWidth: 1022, pagePosition: 1, target }), /detached target/);
  assert.equal(style.getPropertyValue("transform"), "translateX(-3066px)");
  assert.equal(style.getPropertyPriority("transform"), "important");
  assert.equal(style.getPropertyValue("transition"), "");
});
