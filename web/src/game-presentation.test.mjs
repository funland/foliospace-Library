import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";
import ts from "typescript";

const source = await readFile(path.join(path.dirname(fileURLToPath(import.meta.url)), "game-presentation.ts"), "utf8");
const transpiled = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ES2020, target: ts.ScriptTarget.ES2020 } }).outputText;
const { groupGamesForPresentation } = await import(`data:text/javascript;base64,${Buffer.from(transpiled).toString("base64")}`);

function disc(id, discNumber, groupKey = "edition-japan") {
  return { id, title: `Creature Shock (Disc ${discNumber})`, favorite: id === 1, discGroupInfo: { groupKey, title: "Creature Shock (Japan)", discNumber } };
}

test("presentation uses loaded discs in order and retains original state identities", () => {
  const first = disc(1, 1);
  const second = disc(2, 2);
  const input = [second, first];
  const before = structuredClone(input);
  const groups = groupGamesForPresentation(input);
  assert.equal(groups.length, 1);
  assert.equal(groups[0].multidisc, true);
  assert.deepEqual(groups[0].items.map(item => item.id), [1, 2]);
  assert.strictEqual(groups[0].items[0], first);
  assert.strictEqual(groups[0].items[1], second);
  assert.deepEqual(input, before);
});

test("incomplete pages and edition variants remain individually accessible", () => {
  const release = disc(1, 1);
  const pilot = { ...disc(2, 2, "edition-pilot"), title: "Creature Shock (Pilot) (Disc 2)" };
  const demo = { id: 3, title: "Creature Shock (Demo)" };
  const groups = groupGamesForPresentation([release, pilot, demo]);
  assert.deepEqual(groups.map(group => [group.multidisc, group.items[0].id]), [[false, 1], [false, 2], [false, 3]]);
  assert.equal(groups[0].title, release.title);
  const expanded = groupGamesForPresentation([release, pilot, demo, disc(4, 2)]);
  assert.deepEqual(expanded[0].items.map(item => item.id), [1, 4]);
  assert.equal(expanded.length, 3);
});

test("duplicate disc positions are never silently collapsed", () => {
  const input = [disc(1, 1), disc(2, 1), disc(3, 2)];
  assert.deepEqual(groupGamesForPresentation(input).map(group => group.items.map(item => item.id)), [[1], [2], [3]]);
});

test("dependency rows stay separate and malformed hints are ignored", () => {
  const input = [disc(1, 1), { ...disc(2, 2), catalogRole: "dependency" }, disc(3, 0), disc(4, 2)];
  assert.deepEqual(groupGamesForPresentation(input).map(group => group.items.map(item => item.id)), [[1, 4], [2], [3]]);
});
