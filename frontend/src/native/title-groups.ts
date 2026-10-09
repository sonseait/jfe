import type { DTO } from './api';

export interface TitleGroup {
  key: string;
  title: string;
  items: DTO<'ItemDTO'>[];
}

export interface SeasonGroup {
  season: number;
  items: DTO<'ItemDTO'>[];
}

const collator = new Intl.Collator(undefined, { numeric: true, sensitivity: 'base' });
function titleStem(title: string) {
  const stem = title
    .split(/[:：]/)[0]
    .replace(/\([^)]*\)|\[[^\]]*\]/g, ' ')
    .replace(
      /\b(?:part|chapter|volume|vol|phần|tập)\s+(?:\d+|[ivx]+|one|two|three|four|five)\b.*$/iu,
      '',
    )
    .replace(/\s+(?:\d+|[ivx]+)\s*$/i, '')
    .trim();
  const words = stem
    .normalize('NFD')
    .replace(/\p{M}/gu, '')
    .replace(/đ/gi, 'd')
    .toLocaleLowerCase()
    .replace(/[^\p{L}\p{N}]+/gu, ' ')
    .trim()
    .split(/\s+/);
  if (['the', 'a', 'an'].includes(words[0])) words.shift();
  return { stem, words: words.filter(Boolean) };
}

// Group only for display; every item keeps its identity and playback state.
export function groupTitles(items: DTO<'ItemDTO'>[], alphabetical = true): TitleGroup[] {
  const entries = items.map((item) => ({ item, ...titleStem(item.title) }));
  const roots = new Set(
    entries
      .filter((e) => e.words.length === 1)
      .map((e) => `${e.item.libraryId}:${e.item.kind}:${e.words[0]}`),
  );
  const groups = new Map<string, TitleGroup>();
  for (const { item, stem, words } of entries) {
    const root = `${item.libraryId}:${item.kind}:${words[0]}`;
    const size = roots.has(root) ? 1 : 2;
    const key = `${item.libraryId}:${item.kind}:${words.slice(0, size).join(' ') || item.id}`;
    const group = groups.get(key);
    if (group) {
      group.items.push(item);
      if (stem.length < group.title.length) group.title = stem;
    } else groups.set(key, { key, title: stem || item.title, items: [item] });
  }
  for (const group of groups.values()) {
    if (alphabetical)
      group.items.sort(
        (a, b) => collator.compare(a.title, b.title) || a.year - b.year || a.id.localeCompare(b.id),
      );
    if (group.items.length > 1) {
      // Use the common display prefix instead of naming the group after one sequel.
      const labels = group.items.map((i) => titleStem(i.title).stem.split(/\s+/));
      const prefix: string[] = [];
      for (let n = 0; n < labels[0].length; n++) {
        if (!labels.every((words) => words[n] && collator.compare(words[n], labels[0][n]) === 0))
          break;
        prefix.push(labels[0][n]);
      }
      while (
        prefix.length > 1 &&
        ['and', 'the', 'of', 'a', 'an', 'và', 'của'].includes(prefix.at(-1)!.toLocaleLowerCase())
      )
        prefix.pop();
      if (prefix.length) group.title = prefix.join(' ');
    }
  }
  const result = [...groups.values()];
  return alphabetical
    ? result.sort((a, b) => collator.compare(a.title, b.title) || a.key.localeCompare(b.key))
    : result;
}

// A series can have multiple seasons, but episodes remain independently playable cards.
export function groupEpisodesBySeason(items: DTO<'ItemDTO'>[]): SeasonGroup[] {
  const groups = new Map<number, SeasonGroup>();
  for (const item of items) {
    const group = groups.get(item.season);
    if (group) group.items.push(item);
    else groups.set(item.season, { season: item.season, items: [item] });
  }
  for (const group of groups.values()) {
    group.items.sort(
      (a, b) =>
        a.episode - b.episode || collator.compare(a.title, b.title) || a.id.localeCompare(b.id),
    );
  }
  return [...groups.values()].sort((a, b) => a.season - b.season);
}
