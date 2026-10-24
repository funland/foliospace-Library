import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import ts from "typescript";
const source = await readFile(new URL("./reader-keyboard.ts", import.meta.url), "utf8");
const compiled = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ES2020 } }).outputText;
const { readerKeyboardAction, bindReaderKeyboard } = await import(`data:text/javascript;base64,${Buffer.from(compiled).toString("base64")}`);
test("body and cross-window targets use arrow shortcuts, editable and modified keys do not", () => {
  assert.equal(readerKeyboardAction({ key: "ArrowRight", target: { closest: () => null } }), "next");
  assert.equal(readerKeyboardAction({ key: "ArrowLeft" }), "previous");
  for (const flag of ["ctrlKey", "altKey", "metaKey", "shiftKey", "isComposing", "defaultPrevented"]) assert.equal(readerKeyboardAction({ key: "ArrowRight", [flag]: true }), null);
  assert.equal(readerKeyboardAction({ key: "ArrowLeft", target: { closest: () => ({}) } }), null);
  assert.equal(readerKeyboardAction({ key: "f", target: { isContentEditable: true } }), null);
});
test("document replacement removes the previous listener and uses current callbacks", () => {
  const first = new EventTarget(), second = new EventTarget();
  let position = 0; let callback = () => position++;
  const detach = bindReaderKeyboard(first, () => callback());
  first.dispatchEvent(new Event("keydown"));
  callback = () => position += 10;
  first.dispatchEvent(new Event("keydown"));
  detach();
  const detachSecond = bindReaderKeyboard(second, () => callback());
  first.dispatchEvent(new Event("keydown"));
  second.dispatchEvent(new Event("keydown"));
  assert.equal(position, 21);
  detachSecond(); second.dispatchEvent(new Event("keydown")); assert.equal(position, 21);
});
