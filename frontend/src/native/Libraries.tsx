import { useState } from 'react';
import { Badge, Button, Group, TextInput } from '@mantine/core';
import {
  ArrowDownAZ,
  ArrowUpRight,
  Film,
  FolderPlus,
  Library,
  Search,
  Tv,
  Music2,
} from 'lucide-react';
import { Link } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { api, result, useAuth, useResource, imageURL, type DTO } from './api';
import { LibraryStatus, State } from './shared';

function LibraryTile({ library, index }: { library: DTO<'LibraryDTO'>; index: number }) {
  const { t } = useTranslation();
  const preview = useResource(['library-preview', library.id, library.lastScanAt], async (signal) =>
    result(
      await api.GET('/api/v1/items', {
        signal,
        params: {
          query: {
            libraryId: library.id,
            kind: {
              movies: 'movie',
              series: 'series',
              music: 'album',
              podcasts: 'podcast',
              audiobooks: 'audiobook',
            }[library.kind] as DTO<'CatalogQuery'>['kind'],
            limit: 3,
          },
        },
      }),
    ),
  );
  const posters = preview.data?.items.filter((i) => i.poster) ?? [];
  const Icon = library.kind === 'movies' ? Film : library.kind === 'series' ? Tv : Music2;
  return (
    <Link
      to={`/library/${library.id}`}
      aria-label={library.name}
      className={`archive-tile archive-tone-${index % 3}`}
    >
      <div className="archive-art" aria-hidden="true">
        <span className="archive-index">{String(index + 1).padStart(2, '0')}</span>
        {posters.length ? (
          <div className="archive-posters">
            {posters.map((item) => (
              <img
                key={item.id}
                src={imageURL(item)}
                alt=""
                loading="lazy"
                onError={(e) => {
                  e.currentTarget.style.visibility = 'hidden';
                }}
              />
            ))}
          </div>
        ) : (
          <div className="archive-symbol">
            <Icon strokeWidth={0.75} />
          </div>
        )}
        <span className="archive-kind">
          <Icon size={14} />
          {t(
            library.kind === 'movies' || library.kind === 'series'
              ? library.kind
              : `audioUI.${library.kind}`,
          )}
        </span>
        <span className="archive-open">
          <ArrowUpRight size={22} />
        </span>
      </div>
      <div className="archive-info">
        <h2>{library.name}</h2>
        <LibraryStatus library={library} />
      </div>
    </Link>
  );
}

export default function Libraries({ embedded = false }: { embedded?: boolean }) {
  const { t } = useTranslation();
  const admin = useAuth((s) => s.user?.role === 'admin');
  const [search, setSearch] = useState('');
  const data = useResource(
    'libraries',
    async (signal) => result(await api.GET('/api/v1/libraries', { signal })),
    true,
    3000,
  );
  const libraries = data.data?.items ?? [];
  const filtered = libraries.filter((l) =>
    l.name.toLocaleLowerCase().includes(search.toLocaleLowerCase()),
  );
  return (
    <section className={embedded ? 'archive-embedded' : 'page archive-page'}>
      {!embedded && (
        <header className="archive-heading">
          <div>
            <span className="eyebrow">{t('archive.eyebrow')}</span>
            <h1>{t('libraries')}</h1>
            <p>{t('archive.description')}</p>
          </div>
          {admin && (
            <Button
              component={Link}
              to="/admin/libraries"
              variant="default"
              leftSection={<FolderPlus size={17} />}
            >
              {t('archive.manage')}
            </Button>
          )}
        </header>
      )}
      {!embedded && (
        <div className="archive-toolbar">
          <Group gap="sm">
            <Badge variant="light" color="orange" leftSection={<Library size={12} />}>
              {t('archive.libraryCount', { count: libraries.length })}
            </Badge>
            <span className="muted">
              {t('native.fileCount', { count: libraries.reduce((n, l) => n + l.fileCount, 0) })}
            </span>
          </Group>
          <TextInput
            aria-label={t('archive.findLibrary')}
            placeholder={t('archive.findLibrary')}
            leftSection={<Search size={17} />}
            value={search}
            onChange={(e) => setSearch(e.currentTarget.value)}
          />
          <span className="archive-sort">
            <ArrowDownAZ size={17} />
            {t('archive.nameOrder')}
          </span>
        </div>
      )}
      <State loading={data.isLoading} error={data.error}>
        <div className="archive-grid">
          {filtered.map((library, index) => (
            <LibraryTile key={library.id} library={library} index={index} />
          ))}
        </div>
        {!filtered.length && (
          <div className="archive-empty">
            <Library size={40} strokeWidth={1} />
            <h2>{t(libraries.length ? 'empty' : 'archive.emptyTitle')}</h2>
            <p>{t(libraries.length ? 'archive.changeSearch' : 'native.empty')}</p>
            {admin && !libraries.length && (
              <Button component={Link} to="/admin/libraries">
                {t('manage.addLibrary')}
              </Button>
            )}
          </div>
        )}
      </State>
    </section>
  );
}
