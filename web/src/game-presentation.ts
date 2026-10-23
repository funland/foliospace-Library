import type { GameAsset } from "./api";

export type GamePresentationGroup = {
  key: string;
  title: string;
  items: GameAsset[];
  multidisc: boolean;
};

// Groups only the assets already loaded by the caller. A group is a display
// container; every action continues to use the original asset and its ID.
export function groupGamesForPresentation(games: GameAsset[]): GamePresentationGroup[] {
  const candidates = new Map<string, GameAsset[]>();
  for (const game of games) {
    const info = game.discGroupInfo;
    if (!info || !info.groupKey || !info.title || !Number.isInteger(info.discNumber) || info.discNumber < 1 || game.catalogRole === "dependency") continue;
    const items = candidates.get(info.groupKey) ?? [];
    items.push(game);
    candidates.set(info.groupKey, items);
  }
  const grouped = new Set<string>();
  const result: GamePresentationGroup[] = [];
  for (const game of games) {
    const info = game.discGroupInfo;
    const items = info ? candidates.get(info.groupKey) : undefined;
    // Multiple copies claiming the same disc number are ambiguous editions.
    // Keep their individual cards until the source metadata can distinguish them.
    const unambiguous = items && items.includes(game) && items.length > 1 && new Set(items.map(item => item.discGroupInfo?.discNumber)).size === items.length;
    if (!info || !unambiguous) {
      result.push({ key: `game-${game.id}`, title: game.title, items: [game], multidisc: false });
      continue;
    }
    if (grouped.has(info.groupKey)) continue;
    grouped.add(info.groupKey);
    result.push({
      key: info.groupKey,
      title: info.title,
      items: [...items].sort((a, b) => a.discGroupInfo!.discNumber - b.discGroupInfo!.discNumber),
      multidisc: true,
    });
  }
  return result;
}
