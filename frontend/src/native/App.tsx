import Libraries from './Libraries';
import AudioPlayer, { isAudioItem, isAudioGroup, useAudioPlayer } from './AudioPlayer';
import { AudioDetail, Imports, AudioArtists } from './Audio';
import DetailInfo from './DetailInfo';
import Discover from './Discover';
import { groupEpisodesBySeason, groupTitles } from './title-groups';
import { useEffect, useRef, useState } from 'react';
import {
  Alert,
  Badge,
  SegmentedControl,
  Button,
  Group,
  Loader,
  PasswordInput,
  Select,
  Stack,
  TextInput,
} from '@mantine/core';
import { useDebouncedValue } from '@mantine/hooks';
import { useInfiniteQuery } from '@tanstack/react-query';
import {
  ChevronDown,
  ChevronRight,
  ArrowLeft,
  Layers,
  LayoutGrid,
  Compass,
  Download,
  Film,
  Heart,
  Library,
  LogOut,
  Play,
  Search,
  Settings,
  Shield,
} from 'lucide-react';
import {
  Link,
  Navigate,
  NavLink,
  Outlet,
  Route,
  Routes,
  useParams,
  useSearchParams,
} from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { api, result, useAuth, useResource, type DTO, imageURL } from './api';
import { Card, State, mutate } from './shared';
import Admin, { MetadataEditor } from './Admin';
import Player, { useNativePlayer } from './Player';

function Brand() {
  return (
    <Link to="/" className="brand">
      <Play fill="currentColor" size={24} />
      <span>jfe.</span>
    </Link>
  );
}
export default function NativeApp() {
  const { t } = useTranslation();
  const system = useResource('system', async (signal) =>
    result(await api.GET('/api/v1/system', { signal })),
  );
  const auth = useAuth();
  const me = useResource(
    'me',
    async (signal) => result(await api.GET('/api/v1/users/me', { signal })),
    Boolean(auth.token),
  );
  return (
    <State
      loading={system.isLoading || (Boolean(auth.token) && me.isLoading)}
      error={system.error || (auth.token ? me.error : undefined)}
    >
      {!system.data ? (
        <Loader />
      ) : !auth.token || !me.data ? (
        <Login system={system.data} />
      ) : (
        <Routes>
          <Route element={<Shell user={me.data} serverName={system.data.name} />}>
            <Route index element={<Discover />} />
            <Route path="imports" element={<Imports />} />
            <Route path="libraries" element={<Libraries />} />
            <Route path="library/:id" element={<Catalog mode="library" />} />
            <Route path="search" element={<Catalog mode="search" />} />
            <Route path="favorites" element={<Catalog mode="favorites" />} />
            <Route path="item/:id" element={<Detail />} />
            <Route path="settings/:tab?" element={<Account />} />
            <Route
              path="admin/:tab?"
              element={
                me.data.role === 'admin' ? <Admin /> : <Alert color="red">{t('forbidden')}</Alert>
              }
            />
            <Route path="*" element={<Navigate to="/" replace />} />
          </Route>
        </Routes>
      )}
    </State>
  );
}
function Login({ system }: { system: DTO<'SystemDTO'> }) {
  const { t } = useTranslation();
  const [name, setName] = useState('');
  const [password, setPassword] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(false);
  return (
    <div className="connect-page">
      <div className="connect-story">
        <Brand />
        <div className="orb" />
        <div className="orb orb-two" />
        <div className="cinema-art">
          <div className="cinema-frame">
            <Play size={64} />
          </div>
        </div>
        <div className="connect-copy">
          <p className="server-name">{system.name}</p>
          <h1>{t('cinema')}</h1>
          <p>{t('native.welcome')}</p>
        </div>
      </div>
      <div className="connect-form">
        <form
          className="form-inner"
          onSubmit={async (e) => {
            e.preventDefault();
            setBusy(true);
            setError(false);
            try {
              const body = { username: name, password };
              const value = system.setupRequired
                ? result(await api.POST('/api/v1/auth/setup', { body }))
                : result(await api.POST('/api/v1/auth/login', { body }));
              useAuth.getState().login(value);
            } catch {
              setError(true);
            } finally {
              setBusy(false);
            }
          }}
        >
          <Stack>
            <h1>{t(system.setupRequired ? 'native.setup' : 'signIn')}</h1>
            <TextInput
              label={t('username')}
              required
              value={name}
              onChange={(e) => setName(e.currentTarget.value)}
            />
            <PasswordInput
              label={t('password')}
              autoComplete={system.setupRequired ? 'new-password' : 'current-password'}
              minLength={system.setupRequired ? 8 : 1}
              required
              value={password}
              onChange={(e) => setPassword(e.currentTarget.value)}
            />
            {error && <Alert color="red">{t('invalidLogin')}</Alert>}
            <Button type="submit" loading={busy}>
              {t(system.setupRequired ? 'create' : 'signIn')}
            </Button>
          </Stack>
        </form>
      </div>
    </div>
  );
}
function Shell({ user, serverName }: { user: DTO<'UserDTO'>; serverName: string }) {
  const { t } = useTranslation();
  const nav = [
    [Compass, 'home', '/'],
    [Library, 'libraries', '/libraries'],
    [Download, 'audioUI.imports', '/imports'],
    [Search, 'search', '/search'],
    [Heart, 'favorites', '/favorites'],
    [Settings, 'settings', '/settings/account'],
  ] as const;
  return (
    <div className="app-shell">
      <aside className="sidebar">
        <Brand />
        <div className="server-name" title={serverName}>
          {serverName}
        </div>
        <div className="sidebar-label">{t('personalLibrary')}</div>
        <nav className="main-nav">
          {nav.map(([Icon, key, path]) => (
            <NavLink key={key} to={path} end={path === '/'}>
              <Icon size={20} />
              {t(key)}
            </NavLink>
          ))}
          {user.role === 'admin' && (
            <NavLink to="/admin/general">
              <Shield size={20} />
              {t('admin')}
            </NavLink>
          )}
        </nav>
      </aside>
      <div className="app-content">
        <header className="topbar">
          <Link to="/search" className="top-search">
            <Search size={20} />
            {t('native.searchPlaceholder')}
          </Link>
          <Group gap="xs">
            <span>{user.username}</span>
            <Button
              variant="subtle"
              aria-label={t('logout')}
              onClick={async () => {
                if (await mutate(async () => result(await api.POST('/api/v1/auth/logout')))) {
                  useNativePlayer.getState().stop();
                  useAudioPlayer.getState().stop();
                  useAuth.getState().logout();
                }
              }}
            >
              <LogOut size={18} />
            </Button>
          </Group>
        </header>
        <main id="main">
          <Outlet />
        </main>
        <footer className="app-footer">
          <Brand />
          <span>{t('cinema')}</span>
        </footer>
      </div>
      <nav className="mobile-nav">
        {nav.map(([Icon, key, path]) => (
          <NavLink key={key} to={path} end={path === '/'}>
            <Icon size={20} />
            <span>{t(key)}</span>
          </NavLink>
        ))}
      </nav>
      <Player />
      <AudioPlayer />
    </div>
  );
}
export function Catalog({
  mode = 'library',
  parentId,
}: {
  mode?: 'library' | 'search' | 'favorites';
  parentId?: string;
}) {
  const { t } = useTranslation();
  const { id } = useParams();
  const [params, setParams] = useSearchParams();
  const input = params.get('q') ?? '';
  const [search] = useDebouncedValue(input, 300);
  const user = useAuth((s) => s.user?.id);
  const libraries = useResource(
    'libraries',
    async (signal) => result(await api.GET('/api/v1/libraries', { signal })),
    mode === 'library' && !parentId,
    3000,
  );
  const library = libraries.data?.items.find((l) => l.id === id);
  const grouped = params.get('group') === 'title' && !parentId;
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set());
  const requestedKind = params.get('kind');
  const kind = parentId
    ? 'episode'
    : mode === 'library'
      ? library &&
        {
          movies: 'movie',
          series: 'series',
          music: 'album',
          podcasts: 'podcast',
          audiobooks: 'audiobook',
        }[library.kind]
      : mode === 'search'
        ? [
            'movie',
            'series',
            'album',
            'track',
            'podcast',
            'podcast_episode',
            'audiobook',
            'book_part',
          ].includes(requestedKind ?? '')
          ? requestedKind
          : undefined
        : requestedKind || undefined;
  const query = {
    artist: params.get('artist') || undefined,
    topLevel: mode === 'search',
    libraryId: mode === 'library' && !parentId ? id : undefined,
    parentId,
    search: !parentId ? search : undefined,
    favorites: mode === 'favorites',
    kind: kind as DTO<'CatalogQuery'>['kind'],
    limit: 36,
  };
  const data = useInfiniteQuery({
    queryKey: ['native', user, 'catalog', query],
    initialPageParam: '',
    queryFn: async ({ pageParam, signal }) =>
      result(
        await api.GET('/api/v1/items', {
          params: { query: { ...query, cursor: pageParam || undefined } },
          signal,
        }),
      ),
    getNextPageParam: (last) => (last.hasMore ? last.nextCursor : undefined),
  });
  const sentinel = useRef<HTMLDivElement>(null);
  const { fetchNextPage, hasNextPage, isFetching, isError } = data;
  useEffect(() => {
    if (!sentinel.current || !hasNextPage || isFetching || isError) return;
    const observer = new IntersectionObserver(
      ([e]) => {
        if (e.isIntersecting) void fetchNextPage();
      },
      { rootMargin: '400px' },
    );
    observer.observe(sentinel.current);
    return () => observer.disconnect();
  }, [fetchNextPage, hasNextPage, isFetching, isError]);
  const items = Array.from(
    new Map(data.data?.pages.flatMap((p) => p.items).map((i) => [i.id, i])).values(),
  );
  const seasonGroups = parentId ? groupEpisodesBySeason(items) : [];
  return (
    <div className={parentId ? '' : 'page'}>
      {!parentId && (
        <>
          <header className="archive-heading catalog-heading">
            <div>
              {mode === 'library' && (
                <Link to="/libraries" className="catalog-back">
                  <ArrowLeft size={15} />
                  {t('libraries')}
                </Link>
              )}
              <span className="eyebrow">{t(mode === 'library' ? 'archive.browse' : mode)}</span>
              <h1>{mode === 'library' ? (library?.name ?? t('libraries')) : t(mode)}</h1>
              <p>
                {t(
                  library && ['music', 'podcasts', 'audiobooks'].includes(library.kind)
                    ? 'audioUI.browseHelp'
                    : 'archive.catalogDescription',
                )}
              </p>
            </div>
            {library && (
              <div className="catalog-summary">
                <Film size={23} />
                <strong>{library.fileCount}</strong>
                <span>{t('archive.mediaFiles')}</span>
              </div>
            )}
          </header>
          {library && ['music', 'podcasts', 'audiobooks'].includes(library.kind) && (
            <AudioArtists
              libraryId={library.id}
              value={params.get('artist')}
              onChange={(artist) => {
                const next = new URLSearchParams(params);
                if (artist) next.set('artist', artist);
                else next.delete('artist');
                setParams(next);
              }}
            />
          )}
          <div className="browser-toolbar">
            <TextInput
              aria-label={t('search')}
              placeholder={t('archive.findFilm')}
              leftSection={<Search size={17} />}
              value={input}
              onChange={(e) => {
                const next = new URLSearchParams(params);
                if (e.currentTarget.value) next.set('q', e.currentTarget.value);
                else next.delete('q');
                setParams(next, { replace: true });
              }}
            />
            {mode !== 'library' && (
              <Select
                aria-label={t('filter')}
                value={kind ?? ''}
                allowDeselect={false}
                data={[
                  { value: '', label: t('all') },
                  { value: 'movie', label: t('movies') },
                  { value: 'series', label: t('series') },
                  ...['album', 'track', 'podcast', 'audiobook'].map((value) => ({
                    value,
                    label: t(`audioUI.${value}`),
                  })),
                  ...(mode === 'search' ? [] : [{ value: 'episode', label: t('episodes') }]),
                ]}
                onChange={(v) => {
                  const next = new URLSearchParams(params);
                  if (v) next.set('kind', v);
                  else next.delete('kind');
                  setParams(next);
                }}
              />
            )}
            <SegmentedControl
              aria-label={t('archive.view')}
              value={grouped ? 'title' : 'none'}
              data={[
                {
                  value: 'none',
                  label: (
                    <span className="view-option">
                      <LayoutGrid size={15} />
                      {t('archive.allFilms')}
                    </span>
                  ),
                },
                {
                  value: 'title',
                  label: (
                    <span className="view-option">
                      <Layers size={15} />
                      {t('archive.groupTitles')}
                    </span>
                  ),
                },
              ]}
              onChange={(value) => {
                const next = new URLSearchParams(params);
                if (value === 'title') next.set('group', value);
                else next.delete('group');
                setParams(next);
              }}
            />
          </div>
          <div className="browser-meta">
            <span>{t('archive.loaded', { count: items.length })}</span>
            {grouped && (
              <Group gap="xs">
                <span>{t('archive.groupHint')}</span>
                <Button size="compact-xs" variant="subtle" onClick={() => setCollapsed(new Set())}>
                  {t('archive.expandAll')}
                </Button>
                <Button
                  size="compact-xs"
                  variant="subtle"
                  onClick={() => setCollapsed(new Set(groupTitles(items).map((g) => g.key)))}
                >
                  {t('archive.collapseAll')}
                </Button>
              </Group>
            )}
          </div>
        </>
      )}
      <State loading={data.isLoading} error={!items.length ? data.error : undefined}>
        {parentId ? (
          <div className="season-groups">
            {seasonGroups.map((group) => (
              <section key={group.season} className="season-group">
                <button
                  className="season-group-heading"
                  aria-expanded={!collapsed.has(`season-${group.season}`)}
                  aria-controls={`season-${group.season}`}
                  onClick={() =>
                    setCollapsed((old) => {
                      const key = `season-${group.season}`;
                      const next = new Set(old);
                      if (next.has(key)) next.delete(key);
                      else next.add(key);
                      return next;
                    })
                  }
                >
                  {collapsed.has(`season-${group.season}`) ? (
                    <ChevronRight size={18} />
                  ) : (
                    <ChevronDown size={18} />
                  )}
                  <h3>{t('detail.season', { season: group.season })}</h3>
                  <Badge variant="light" color="gray">
                    {group.items.length}
                  </Badge>
                  <span className="season-group-rule" />
                </button>
                <div id={`season-${group.season}`} hidden={collapsed.has(`season-${group.season}`)}>
                  <div className="poster-grid">
                    {group.items.map((i) => (
                      <Card key={i.id} item={i} />
                    ))}
                  </div>
                </div>
              </section>
            ))}
          </div>
        ) : grouped ? (
          <div className="title-groups">
            {groupTitles(items).map((group, index) => (
              <section key={group.key} className="title-group">
                <button
                  className="title-group-heading"
                  aria-expanded={!collapsed.has(group.key)}
                  aria-controls={`title-group-${index}`}
                  onClick={() =>
                    setCollapsed((old) => {
                      const next = new Set(old);
                      if (next.has(group.key)) next.delete(group.key);
                      else next.add(group.key);
                      return next;
                    })
                  }
                >
                  {collapsed.has(group.key) ? (
                    <ChevronRight size={18} />
                  ) : (
                    <ChevronDown size={18} />
                  )}
                  <h2>{group.title}</h2>
                  <Badge variant="light" color="gray">
                    {group.items.length}
                  </Badge>
                  <span className="title-group-rule" />
                </button>
                <div id={`title-group-${index}`} hidden={collapsed.has(group.key)}>
                  <div className="poster-grid">
                    {group.items.map((i) => (
                      <Card key={i.id} item={i} />
                    ))}
                  </div>
                </div>
              </section>
            ))}
          </div>
        ) : (
          <div className="poster-grid">
            {items.map((i) => (
              <Card key={i.id} item={i} />
            ))}
          </div>
        )}
        {!items.length && <p className="muted">{t('empty')}</p>}
        <div ref={sentinel} className="catalog-load-more">
          {data.isFetchNextPageError && <Alert color="red">{t('error')}</Alert>}
          {hasNextPage && (
            <Button loading={data.isFetchingNextPage} onClick={() => void fetchNextPage()}>
              {t(data.isFetchNextPageError ? 'retry' : 'loadMore')}
            </Button>
          )}
        </div>
      </State>
    </div>
  );
}
function Detail() {
  const { t } = useTranslation();
  const { id = '' } = useParams();
  const detail = useResource(['item', id], async (signal) =>
    result(await api.GET('/api/v1/items/{id}', { params: { path: { id } }, signal })),
  );
  const [selected, setSelected] = useState<string>();
  const [edit, setEdit] = useState(false);
  const item = detail.data?.item;
  const activeFile =
    detail.data?.files.find((f) => f.id === selected && f.available) ??
    detail.data?.files.find((f) => f.available);
  const libraries = useResource('libraries', async (signal) =>
    result(await api.GET('/api/v1/libraries', { signal })),
  );
  const library = libraries.data?.items.find((l) => l.id === item?.libraryId);
  const admin = useAuth((s) => s.user?.role === 'admin');
  if (detail.data && item && (isAudioItem(item.kind) || isAudioGroup(item.kind)))
    return <AudioDetail detail={detail.data} />;
  return (
    <div className="page">
      <State loading={detail.isLoading} error={detail.error}>
        {item && (
          <>
            <div className="detail-breadcrumb">
              <Link to={`/library/${item.libraryId}`}>{library?.name ?? t('libraries')}</Link>
              <ChevronRight size={14} />
              {item.kind === 'episode' && item.parentId && (
                <>
                  <Link to={`/item/${item.parentId}`}>{t('detail.backToSeries')}</Link>
                  <ChevronRight size={14} />
                </>
              )}
              <span>
                {t(
                  item.kind === 'episode'
                    ? 'episodes'
                    : item.kind === 'movie'
                      ? 'movies'
                      : 'series',
                )}
              </span>
            </div>
            <div className="native-detail film-detail-hero">
              <div className="poster-art">
                {item.poster ? <img src={imageURL(item)} alt="" /> : <Film size={70} />}
              </div>
              <Stack>
                <div className="eyebrow">{t('detail.inYourLibrary')}</div>
                <Group gap="xs">
                  {item.year > 0 && <Badge variant="light">{item.year}</Badge>}
                  {item.kind === 'episode' && (
                    <Badge variant="light">
                      {t('detail.episode', { season: item.season, episode: item.episode })}
                    </Badge>
                  )}
                  {activeFile && activeFile.duration > 0 && (
                    <Badge variant="light">
                      {t('detail.minutes', { count: Math.ceil(activeFile.duration / 60) })}
                    </Badge>
                  )}
                  {item.watched && <Badge color="green">{t('detail.watched')}</Badge>}
                </Group>
                <h1>{item.title}</h1>

                <Group>
                  {item.kind !== 'series' && (
                    <Button
                      leftSection={<Play size={18} />}
                      disabled={!detail.data?.files.some((f) => f.available)}
                      onClick={() => {
                        const f = activeFile;
                        if (f) useNativePlayer.getState().play(detail.data!, f.id);
                      }}
                    >
                      {t(item.position ? 'resume' : 'play')}
                    </Button>
                  )}
                  <Button
                    variant="default"
                    onClick={() =>
                      void mutate(async () =>
                        result(
                          await api.PUT('/api/v1/items/{id}/state', {
                            params: { path: { id } },
                            body: { favorite: !item.favorite, watched: item.watched },
                          }),
                        ),
                      )
                    }
                  >
                    {t(item.favorite ? 'removeFavorite' : 'addFavorite')}
                  </Button>
                  <Button
                    variant="subtle"
                    onClick={() =>
                      void mutate(async () =>
                        result(
                          await api.PUT('/api/v1/items/{id}/state', {
                            params: { path: { id } },
                            body: { favorite: item.favorite, watched: !item.watched },
                          }),
                        ),
                      )
                    }
                  >
                    {t(item.watched ? 'markUnwatched' : 'markWatched')}
                  </Button>
                  {admin && (
                    <Button variant="default" onClick={() => setEdit(true)}>
                      {t('editMetadata')}
                    </Button>
                  )}
                </Group>
                {(detail.data?.files.length ?? 0) > 1 && (
                  <Select
                    label={t('source')}
                    value={activeFile?.id ?? null}
                    data={
                      detail.data?.files.map((f) => ({
                        value: f.id,
                        label: f.name,
                        disabled: !f.available,
                      })) ?? []
                    }
                    onChange={(v) => setSelected(v ?? undefined)}
                  />
                )}
              </Stack>
            </div>
            {detail.data && (
              <DetailInfo detail={detail.data} activeFile={activeFile} onSelect={setSelected} />
            )}
            {item.kind === 'series' && (
              <section className="film-episodes" aria-labelledby="film-episodes-title">
                <h2 id="film-episodes-title">{t('episodes')}</h2>
                <Catalog parentId={id} />
              </section>
            )}
            <MetadataEditor
              item={item}
              sourceName={activeFile?.name}
              opened={edit}
              close={() => setEdit(false)}
            />
          </>
        )}
      </State>
    </div>
  );
}
function Account() {
  const { t, i18n } = useTranslation();
  const [current, setCurrent] = useState('');
  const [next, setNext] = useState('');
  return (
    <div className="page">
      <h1>{t('settings')}</h1>
      <div className="settings-panel">
        <Stack>
          <Select
            label={t('language')}
            value={i18n.language}
            data={[
              { value: 'en', label: 'English' },
              { value: 'vi', label: 'Tiếng Việt' },
            ]}
            onChange={(v) => {
              if (v) {
                localStorage.setItem('jfe.language', v);
                void i18n.changeLanguage(v);
              }
            }}
          />
          {useAuth.getState().user?.role === 'admin' && (
            <Button component={Link} to="/admin/general">
              {t('admin')}
            </Button>
          )}
          <form
            onSubmit={async (e) => {
              e.preventDefault();
              if (
                await mutate(async () =>
                  result(
                    await api.POST('/api/v1/users/me/password', {
                      body: { currentPassword: current, newPassword: next },
                    }),
                  ),
                )
              )
                useAuth.getState().logout();
            }}
          >
            <Stack>
              <PasswordInput
                required
                label={t('currentPassword')}
                value={current}
                onChange={(e) => setCurrent(e.currentTarget.value)}
              />
              <PasswordInput
                required
                minLength={8}
                label={t('newPassword')}
                value={next}
                onChange={(e) => setNext(e.currentTarget.value)}
              />
              <Button type="submit">{t('save')}</Button>
            </Stack>
          </form>
        </Stack>
      </div>
    </div>
  );
}
