import { expect, it } from 'vitest';
import { groupEpisodesBySeason, groupTitles } from '../src/native/title-groups';
import type { DTO } from '../src/native/api';

function movie(id: string, title: string, libraryId = 'films'): DTO<'ItemDTO'> {
  return {
    id,
    title,
    libraryId,
    kind: 'movie',
    parentId: '',
    year: 2025,
    season: 0,
    episode: 0,
    overview: '',
    poster: '',
    providerId: '',
    metadataLocked: false,
    favorite: false,
    watched: false,
    position: 42,
  };
}
it('groups sequels, release variants and shared name prefixes without changing items', () => {
  const items = [
    movie('a', 'Dune (2021)'),
    movie('b', 'Dune: Part Two'),
    movie('c', 'Harry Potter and the Chamber of Secrets'),
    movie('d', 'Harry Potter and the Philosopher’s Stone'),
    movie('e', 'Arrival'),
  ];
  const groups = groupTitles(items);
  expect(groups.map((g) => g.items.length)).toEqual([1, 2, 2]);
  expect(groups[1].title).toBe('Dune');
  expect(groups[1].items.map((i) => i.id)).toEqual(['a', 'b']);
  expect(items[0].position).toBe(42);
  expect(groups[1].items[0]).toBe(items[0]);
});
it('keeps different prefixes, libraries and media kinds apart', () => {
  const groups = groupTitles([
    movie('a', 'Star Wars'),
    movie('b', 'Star Trek'),
    movie('c', 'Star Wars II', 'private'),
    { ...movie('d', 'Star Wars III'), kind: 'series' },
  ]);
  expect(groups).toHaveLength(4);
});
it('normalizes Vietnamese accents and sorts numbered parts naturally', () => {
  const groups = groupTitles([
    movie('a', 'Đất Rừng Phần 10'),
    movie('b', 'Dat Rung Phan 2'),
    movie('c', 'Đất Rừng Phần 1'),
  ]);
  expect(groups).toHaveLength(1);
  expect(groups[0].items.map((i) => i.id)).toEqual(['c', 'b', 'a']);
});
it('does not lose films when another infinite-scroll page extends a group', () => {
  const first = [movie('a', 'Iron Man'), movie('b', 'Iron Man 2')];
  const initial = groupTitles(first);
  const next = groupTitles([...first, movie('c', 'Iron Man 3')]);
  expect(next[0].key).toBe(initial[0].key);
  expect(next[0].items).toHaveLength(3);
  expect(groupTitles([])).toEqual([]);
});
it('groups episodes by season and orders each group by episode number', () => {
  const episodes = [
    { ...movie('a', 'Second'), kind: 'episode', season: 2, episode: 2 },
    { ...movie('b', 'First'), kind: 'episode', season: 1, episode: 1 },
    { ...movie('c', 'Pilot'), kind: 'episode', season: 2, episode: 1 },
  ];
  const groups = groupEpisodesBySeason(episodes);
  expect(groups.map((group) => group.season)).toEqual([1, 2]);
  expect(groups[1].items.map((item) => item.id)).toEqual(['c', 'a']);
});
