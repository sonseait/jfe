import { useRef, useState, type ReactNode } from 'react';
import { Button, Group, ScrollArea, Skeleton } from '@mantine/core';
import { ArrowLeft, ArrowRight, ArrowUpRight, Clock3, Film, Library, Music2, Play, Tv } from 'lucide-react';
import { Link } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import toast from 'react-hot-toast';
import { useQuery } from '@tanstack/react-query';
import { api, result, useAuth, useResource, imageURL, type DTO } from './api';
import { Card, State } from './shared';
import { useNativePlayer } from './Player';
import { isAudioItem, useAudioPlayer } from './AudioPlayer';

type Item = DTO<'ItemDTO'>;
function Cover({ item, className = '' }: { item: Item; className?: string }) {
  const [failed, setFailed] = useState(false);
  return (
    <div className={`discover-cover ${className}`}>
      {item.poster && !failed ? (
        <img src={imageURL(item)} alt="" loading="lazy" onError={() => setFailed(true)} />
      ) : (
        <div className="discover-cover-fallback">
          {isAudioItem(item.kind) ? <Music2 strokeWidth={0.75} /> : <Film strokeWidth={0.75} />}
          <span>{item.title}</span>
        </div>
      )}
    </div>
  );
}
function Rail({
  title,
  subtitle,
  children,
  wide = false,
  link,
}: {
  title: string;
  subtitle: string;
  children: ReactNode;
  wide?: boolean;
  link?: string;
}) {
  const { t } = useTranslation();
  const viewport = useRef<HTMLDivElement>(null);
  const scroll = (direction: number) => {
    const el = viewport.current;
    if (el)
      el.scrollBy({
        left: direction * el.clientWidth * 0.8,
        behavior: matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth',
      });
  };
  return (
    <section className="discover-section">
      <header className="discover-section-heading">
        <div>
          <span className="eyebrow">{subtitle}</span>
          <h2>{title}</h2>
        </div>
        <Group gap="xs">
          {link && (
            <Link className="discover-text-link" to={link}>
              {t('discover.browseAll')}
              <ArrowUpRight size={15} />
            </Link>
          )}
          <div className="discover-rail-controls">
            <button
              className="icon-button"
              aria-label={t('discover.previous', { section: title })}
              onClick={() => scroll(-1)}
            >
              <ArrowLeft size={17} />
            </button>
            <button
              className="icon-button"
              aria-label={t('discover.next', { section: title })}
              onClick={() => scroll(1)}
            >
              <ArrowRight size={17} />
            </button>
          </div>
        </Group>
      </header>
      <ScrollArea viewportRef={viewport} type="auto" offsetScrollbars="x" scrollbars="x">
        <div className={`discover-rail ${wide ? 'discover-rail-wide' : ''}`}>{children}</div>
      </ScrollArea>
    </section>
  );
}
export default function Discover() {
  const { t } = useTranslation();
  const user = useAuth((s) => s.user);
  const movies = useResource(['discover', 'movies'], async (signal) =>
    result(
      await api.GET('/api/v1/items', { signal, params: { query: { kind: 'movie', limit: 12 } } }),
    ),
  );
  const series = useResource(['discover', 'series'], async (signal) =>
    result(
      await api.GET('/api/v1/items', { signal, params: { query: { kind: 'series', limit: 12 } } }),
    ),
  );
  const resume = useQuery({
    queryKey: ['native', user?.id, 'resume'],
    queryFn: async ({ signal }) =>
      result(
        await api.GET('/api/v1/items', { signal, params: { query: { resume: true, limit: 8 } } }),
      ),
    staleTime: 0,
    refetchInterval: 10000,
  });
  const libraries = useResource('libraries', async (signal) =>
    result(await api.GET('/api/v1/libraries', { signal })),
  );
  const features = [...(movies.data?.items ?? []), ...(series.data?.items ?? [])].slice(0, 5);
  const [index, setIndex] = useState(0);
  const hero = features[index] ?? features[0];
  const [busy, setBusy] = useState(false);
  const start = async (item: Item) => {
    if (isAudioItem(item.kind)) {
      useAudioPlayer.getState().play([item]);
      return;
    }
    setBusy(true);
    try {
      const detail = result(
        await api.GET('/api/v1/items/{id}', { params: { path: { id: item.id } } }),
      );
      const file = detail.files.find((f) => f.available);
      if (file) useNativePlayer.getState().play(detail, file.id);
      else toast.error(t('discover.unavailable'));
    } catch {
      toast.error(t('error'));
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="discover-page">
      <header className="discover-heading">
        <div>
          <span className="eyebrow">{t('discover.yourCinema')}</span>
          <h1>{t('home')}</h1>
        </div>
        <Link className="discover-text-link" to="/libraries">
          {t('libraries')}
          <ArrowUpRight size={16} />
        </Link>
      </header>
      <State loading={movies.isLoading || series.isLoading} error={movies.error || series.error}>
        {hero ? (
          <section className="discover-spotlight" aria-label={t('discover.spotlight')}>
            <div className="discover-spotlight-copy">
              <span className="discover-feature-label">
                <span />
                {t('discover.spotlight')}
              </span>
              <div className="discover-feature-meta">
                <span>{t(hero.kind === 'series' ? 'series' : 'movies')}</span>
                {hero.year > 0 && <span>{hero.year}</span>}
                {hero.watched && <span>{t('watched')}</span>}
              </div>
              <h2>{hero.title}</h2>
              <p>{hero.overview || t('discover.noOverview')}</p>
              <Group gap="sm">
                {hero.kind === 'movie' && (
                  <Button
                    size="md"
                    loading={busy}
                    leftSection={<Play size={17} fill="currentColor" />}
                    onClick={() => void start(hero)}
                  >
                    {t(hero.position > 0 && !hero.watched ? 'resume' : 'play')}
                  </Button>
                )}
                <Button
                  component={Link}
                  to={`/item/${hero.id}`}
                  variant="default"
                  size="md"
                  rightSection={<ArrowUpRight size={17} />}
                >
                  {t('details')}
                </Button>
              </Group>
              <div className="discover-feature-bottom">
                <span>
                  {String(Math.min(index + 1, features.length)).padStart(2, '0')}{' '}
                  <i>/ {String(features.length).padStart(2, '0')}</i>
                </span>
                <div className="discover-feature-dots">
                  {features.map((item, n) => (
                    <button
                      key={item.id}
                      aria-label={t('discover.feature', { title: item.title })}
                      aria-pressed={hero.id === item.id}
                      onClick={() => setIndex(n)}
                    />
                  ))}
                </div>
                <span className="discover-feature-note">{t('discover.fromLibrary')}</span>
              </div>
            </div>
            <div className="discover-spotlight-art" aria-hidden="true">
              <div className="discover-orbit" />
              <Cover key={hero.id} item={hero} className="discover-feature-cover" />
              <span className="discover-art-caption">{t('discover.aStoryAwaits')}</span>
            </div>
          </section>
        ) : (
          <section className="discover-welcome">
            <div className="discover-welcome-icon">
              <Film size={58} strokeWidth={0.8} />
            </div>
            <span className="eyebrow">{t('discover.yourCinema')}</span>
            <h2>{t('discover.emptyTitle')}</h2>
            <p>{t(user?.role === 'admin' ? 'native.empty' : 'discover.viewerEmpty')}</p>
            <Button
              component={Link}
              to={user?.role === 'admin' ? '/admin/libraries' : '/libraries'}
              rightSection={<ArrowRight size={17} />}
            >
              {t(user?.role === 'admin' ? 'manage.addLibrary' : 'libraries')}
            </Button>
          </section>
        )}
      </State>
      <State loading={false} error={libraries.error}>
        {Boolean(libraries.data?.items.length) && (
          <nav className="discover-library-strip" aria-label={t('libraries')}>
            <span className="discover-strip-label">
              <Library size={16} />
              {t('discover.explore')}
            </span>
            {libraries.data?.items.map((l) => (
              <Link key={l.id} to={`/library/${l.id}`}>
                {l.kind === 'movies' ? (
                  <Film size={16} />
                ) : l.kind === 'series' ? (
                  <Tv size={16} />
                ) : (
                  <Music2 size={16} />
                )}
                <span>{l.name}</span>
                <ArrowUpRight size={14} />
              </Link>
            ))}
          </nav>
        )}
      </State>
      {resume.isLoading && <Skeleton height={160} radius="md" mt="xl" />}
      <State loading={false} error={resume.error}>
        {Boolean(resume.data?.items.length) && (
          <Rail title={t('continueWatching')} subtitle={t('discover.unfinished')} wide>
            {resume.data?.items.map((item) => (
              <article className="discover-resume" key={item.id}>
                <Cover item={item} />
                <div>
                  <span className="discover-resume-time">
                    <Clock3 size={12} />
                    {t('discover.resumeAt', {
                      position: `${Math.floor(item.position / 60)}:${String(Math.floor(item.position % 60)).padStart(2, '0')}`,
                    })}
                  </span>
                  <Link to={`/item/${item.id}`}>
                    <h3>{item.title}</h3>
                  </Link>
                  <span className="muted">
                    {isAudioItem(item.kind)
                      ? t(`audioUI.${item.kind}`)
                      : item.kind === 'episode'
                      ? `S${item.season} · E${item.episode}`
                      : item.year || t('movies')}
                  </span>
                </div>
                <button
                  className="discover-resume-play"
                  aria-label={t('discover.resumeTitle', { title: item.title })}
                  disabled={busy}
                  onClick={() => void start(item)}
                >
                  <Play size={17} fill="currentColor" />
                </button>
              </article>
            ))}
          </Rail>
        )}
      </State>
      {Boolean(movies.data?.items.length) && (
        <Rail
          title={t('discover.movieShelf')}
          subtitle={t('discover.fromLibrary')}
          link="/search?kind=movie"
        >
          {movies.data?.items.map((item) => (
            <Card key={item.id} item={item} />
          ))}
        </Rail>
      )}
      {Boolean(series.data?.items.length) && (
        <Rail
          title={t('discover.seriesShelf')}
          subtitle={t('discover.oneMoreEpisode')}
          link="/search?kind=series"
        >
          {series.data?.items.map((item) => (
            <Card key={item.id} item={item} />
          ))}
        </Rail>
      )}
      {hero && (
        <Link to="/favorites" className="discover-favorites-callout">
          <div>
            <span className="eyebrow">{t('discover.savedForYou')}</span>
            <h2>{t('discover.favoritesTitle')}</h2>
            <p>{t('discover.favoritesCopy')}</p>
          </div>
          <span className="discover-callout-arrow">
            <ArrowUpRight size={25} />
          </span>
        </Link>
      )}
    </div>
  );
}
