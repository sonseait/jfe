import { useState } from 'react';
import toast from 'react-hot-toast';
import {
  Alert,
  Badge,
  Button,
  Checkbox,
  FileInput,
  Group,
  Modal,
  NumberInput,
  Progress,
  ScrollArea,
  Select,
  Stack,
  Table,
  TextInput,
} from '@mantine/core';
import {
  CircleCheck,
  CircleDashed,
  CircleX,
  Download,
  LoaderCircle,
  Music2,
  Pencil,
  Play,
  RefreshCw,
  Tag,
  X,
} from 'lucide-react';
import { Link } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { api, imageURL, result, useAuth, useResource, type DTO } from './api';
import { mutate, State } from './shared';
import { useAudioPlayer, isAudioGroup, isAudioLibrary } from './AudioPlayer';
import { audioErrorKey } from './audio-errors';

export function AudioDetail({ detail }: { detail: DTO<'DetailDTO'> }) {
  const { t } = useTranslation();
  const user = useAuth((s) => s.user);
  const [edit, setEdit] = useState(false);
  const [selected, setSelected] = useState<string[]>([]);
  const group = isAudioGroup(detail.item.kind);
  const entries = group ? (detail.children ?? []) : [detail.item];
  const canEdit = user?.role === 'admin' || user?.importLibraryIds?.includes(detail.item.libraryId);
  return (
    <div className="page audio-page">
      <Link to={`/library/${detail.item.libraryId}`}>{t('libraries')}</Link>
      <header className="audio-hero">
        {detail.item.poster ? (
          <img src={imageURL(detail.item)} alt="" />
        ) : (
          <Music2 size={100} strokeWidth={0.7} />
        )}
        <div>
          <span className="eyebrow">{t(`audioUI.${detail.item.kind}`)}</span>
          <h1>{detail.item.title}</h1>
          <p>{detail.audio?.tags.artists.join(', ')}</p>
          <Group>
            <Button
              leftSection={<Play size={18} />}
              disabled={!entries.length}
              onClick={() => useAudioPlayer.getState().play(entries)}
            >
              {t('audioUI.play')}
            </Button>
            {canEdit && (
              <Button
                variant="default"
                leftSection={<Pencil size={16} />}
                onClick={() => setEdit(true)}
              >
                {t('audioUI.editTags')}
              </Button>
            )}
            <Button
              variant="subtle"
              onClick={() =>
                void mutate(async () =>
                  result(
                    await api.PUT('/api/v1/items/{id}/state', {
                      params: { path: { id: detail.item.id } },
                      body: { favorite: !detail.item.favorite, watched: detail.item.watched },
                    }),
                  ),
                )
              }
            >
              {t(detail.item.favorite ? 'removeFavorite' : 'addFavorite')}
            </Button>
          </Group>
        </div>
      </header>
      <Table.ScrollContainer minWidth={450}>
        <Table>
          <Table.Tbody>
            {entries.map((item, index) => (
              <Table.Tr key={item.id}>
                <Table.Td>
                  {group && canEdit && (
                    <Checkbox
                      aria-label={item.title}
                      checked={selected.includes(item.id)}
                      onChange={(e) => {
                        const checked = e.currentTarget.checked;
                        setSelected((old) =>
                          checked ? [...old, item.id] : old.filter((id) => id !== item.id),
                        );
                      }}
                    />
                  )}
                </Table.Td>
                <Table.Td>{item.episode || index + 1}</Table.Td>
                <Table.Td>
                  <Link to={`/item/${item.id}`}>{item.title}</Link>
                </Table.Td>
                <Table.Td>
                  <Button
                    variant="subtle"
                    aria-label={`${t('audioUI.play')} ${item.title}`}
                    onClick={() => useAudioPlayer.getState().play(entries, index)}
                  >
                    <Play size={16} />
                  </Button>
                </Table.Td>
              </Table.Tr>
            ))}
          </Table.Tbody>
        </Table>
      </Table.ScrollContainer>
      {detail.audio && (
        <dl className="audio-facts">
          {Object.entries(detail.audio.tags)
            .filter(([, v]) => (Array.isArray(v) ? v.length : v))
            .map(([key, value]) => (
              <div key={key}>
                <dt>{t(`audioUI.${key}`)}</dt>
                <dd>{Array.isArray(value) ? value.join(', ') : value}</dd>
              </div>
            ))}
        </dl>
      )}
      <Modal
        opened={edit}
        onClose={() => setEdit(false)}
        title={t('audioUI.editTags')}
        size="lg"
        scrollAreaComponent={ScrollArea.Autosize}
      >
        {edit && (
          <TagEditor
            ids={selected.length ? selected : entries.map((i) => i.id)}
            onDone={() => setEdit(false)}
          />
        )}
      </Modal>
    </div>
  );
}
const tagKeys = [
  'title',
  'artists',
  'albumArtists',
  'album',
  'track',
  'disc',
  'date',
  'genres',
  'composer',
  'comment',
  'author',
  'narrator',
  'musicBrainzId',
] as const;
const listKeys = new Set<string>(['artists', 'albumArtists', 'genres']);
function TagEditor({ ids, onDone }: { ids: string[]; onDone: () => void }) {
  const { t } = useTranslation();
  const [patch, setPatch] = useState<Record<string, string>>({});
  const [confirm, setConfirm] = useState(false);
  const [busy, setBusy] = useState(false);
  const [artwork, setArtwork] = useState<string>();
  const [query, setQuery] = useState('');
  const [search, setSearch] = useState('');
  const [coverID, setCoverID] = useState<string>();
  const data = useResource(['audio-tags', ids], async () => {
    const details = await Promise.all(
      ids.map((id) => api.GET('/api/v1/items/{id}', { params: { path: { id } } }).then(result)),
    );
    const files = details.flatMap((d) => d.files.filter((f) => f.available));
    return Promise.all(
      files.map((f) =>
        api.GET('/api/v1/files/{id}/tags', { params: { path: { id: f.id } } }).then(result),
      ),
    );
  });
  const matches = useResource(
    ['musicbrainz', search],
    (signal) =>
      api.GET('/api/v1/audio/musicbrainz', { params: { query: { search } }, signal }).then(result),
    Boolean(search),
  );
  const editable =
    data.data?.every((f) => f.writable) &&
    (data.data?.length ?? 0) > 0 &&
    (data.data?.length ?? 0) <= 100;
  async function save() {
    setBusy(true);
    const changes: DTO<'AudioTagPatch'> = {};
    for (const [key, value] of Object.entries(patch))
      Object.assign(changes, {
        [key]: listKeys.has(key)
          ? value
              .split(';')
              .map((v) => v.trim())
              .filter(Boolean)
          : value,
      });
    if (artwork !== undefined) changes.artwork = artwork;
    const ok = await mutate(async () =>
      result(
        await api.POST('/api/v1/audio/tags', {
          body: {
            patch: changes,
            items: (data.data ?? []).map((f) => ({
              fileId: f.fileId,
              fingerprint: f.fingerprint,
            })),
          },
        }),
      ),
    );
    setBusy(false);
    if (ok) onDone();
  }
  return (
    <State loading={data.isLoading} error={data.error}>
      <Stack>
        <Alert>
          {t('audioUI.tagHelp')} {t('audioUI.fileCount', { count: data.data?.length ?? 0 })}
        </Alert>
        {(data.data?.length ?? 0) > 100 && <Alert color="yellow">{t('audioUI.batchLimit')}</Alert>}
        {!editable && <Alert color="yellow">{t('audioUI.readOnly')}</Alert>}
        {tagKeys.map((key) => {
          const values = (data.data ?? []).map((f) => {
            const v = f.tags[key];
            return Array.isArray(v) ? v.join('; ') : v;
          });
          const mixed = new Set(values).size > 1;
          const changed = key in patch;
          return (
            <Group key={key} align="end" wrap="nowrap">
              <Checkbox
                aria-label={`${t('audioUI.apply')} ${t(`audioUI.${key}`)}`}
                checked={changed}
                onChange={(e) => {
                  const checked = e.currentTarget.checked;
                  setPatch((old) => {
                    const next = { ...old };
                    if (checked) next[key] = mixed ? '' : (values[0] ?? '');
                    else delete next[key];
                    return next;
                  });
                }}
              />
              <TextInput
                style={{ flex: 1 }}
                label={t(`audioUI.${key}`)}
                placeholder={mixed ? t('audioUI.mixed') : ''}
                value={changed ? patch[key] : mixed ? '' : (values[0] ?? '')}
                onChange={(e) => {
                  const value = e.currentTarget.value;
                  setPatch((old) => ({ ...old, [key]: value }));
                }}
              />
            </Group>
          );
        })}
        <FileInput
          label={t('audioUI.artwork')}
          accept="image/jpeg,image/png"
          onChange={(file) => {
            if (!file) {
              setArtwork(undefined);
              return;
            }
            if (file.size > 600000) {
              toast.error(t('audioUI.artworkLimit'));
              setArtwork(undefined);
              return;
            }
            const reader = new FileReader();
            reader.onload = () => setArtwork(String(reader.result).split(',')[1]);
            reader.readAsDataURL(file);
          }}
          description={t('audioUI.artworkLimit')}
        />
        <Checkbox
          label={t('audioUI.removeArtwork')}
          checked={artwork === ''}
          onChange={(e) => setArtwork(e.currentTarget.checked ? '' : undefined)}
        />
        <Group align="end">
          <TextInput
            label="MusicBrainz"
            value={query}
            onChange={(e) => setQuery(e.currentTarget.value)}
          />
          <Button
            variant="default"
            disabled={!query.trim()}
            onClick={() => setSearch(query.trim())}
          >
            {t('search')}
          </Button>
        </Group>
        {matches.isError && <Alert color="yellow">{t('audioUI.providerUnavailable')}</Alert>}
        {matches.data?.items.map((m) => (
          <Button
            key={m.id}
            variant="subtle"
            onClick={() => {
              setCoverID(m.id);
              setPatch((old) => ({
                ...old,
                album: m.title,
                albumArtists: m.artist,
                date: m.date,
                musicBrainzId: m.id,
              }));
            }}
          >
            {m.title} / {m.artist} / {m.date} / {m.country}
          </Button>
        ))}
        {coverID && (
          <Button
            variant="default"
            onClick={() =>
              void mutate(async () => {
                const cover = result(
                  await api.GET('/api/v1/audio/musicbrainz/{id}/cover', {
                    params: { path: { id: coverID } },
                  }),
                );
                setArtwork(cover.artwork);
                return cover;
              })
            }
          >
            {t('audioUI.useCover')}
          </Button>
        )}
        {matches.data && !matches.data.items.length && <p>{t('empty')}</p>}
        {confirm && (
          <Alert color="orange">
            <p>{t('audioUI.confirmTags')}</p>
            <ul>
              {Object.entries(patch).map(([key, value]) => (
                <li key={key}>
                  {t(`audioUI.${key}`)}: {value || t('audioUI.clear')}
                </li>
              ))}
              {artwork !== undefined && (
                <li>
                  {t('audioUI.artwork')}: {artwork ? t('audioUI.replace') : t('audioUI.clear')}
                </li>
              )}
            </ul>
            <Button loading={busy} onClick={() => void save()}>
              {t('audioUI.writeFiles')}
            </Button>
          </Alert>
        )}
        <Button
          disabled={!editable || (!Object.keys(patch).length && artwork === undefined)}
          onClick={() => setConfirm(true)}
        >
          {t('audioUI.review')}
        </Button>
        <Link to="/imports" onClick={onDone}>
          {t('audioUI.viewJobs')}
        </Link>
      </Stack>
    </State>
  );
}

export function Imports() {
  const { t } = useTranslation();
  const user = useAuth((s) => s.user);
  const [url, setURL] = useState('');
  const [libraryId, setLibraryID] = useState<string | null>(null);
  const [follow, setFollow] = useState(false);
  const [remove, setRemove] = useState<string>();
  const system = useResource(
    'system',
    (signal) => api.GET('/api/v1/system', { signal }).then(result),
    true,
    10000,
  );
  const libraries = useResource('libraries', (signal) =>
    api.GET('/api/v1/libraries', { signal }).then(result),
  );
  const eligibleLibraries = (libraries.data?.items ?? []).filter(
    (l) =>
      isAudioLibrary(l.kind) && (user?.role === 'admin' || user?.importLibraryIds?.includes(l.id)),
  );
  const selectedLibrary = eligibleLibraries.find((l) => l.id === libraryId);
  const sources = useResource(
    'import-sources',
    (signal) => api.GET('/api/v1/import-sources', { signal }).then(result),
    true,
    3000,
  );
  const jobs = useResource(
    'audio-jobs',
    (signal) => api.GET('/api/v1/audio/jobs', { signal }).then(result),
    true,
    2000,
  );
  const [busy, setBusy] = useState(false);
  const jobStateColor = (state: string) =>
    ({ completed: 'green', failed: 'red', cancelled: 'gray', running: 'blue', pending: 'yellow' })[
      state
    ] ?? 'gray';
  const JobStateIcon = ({ state }: { state: string }) => {
    if (state === 'completed') return <CircleCheck />;
    if (state === 'failed' || state === 'cancelled') return <CircleX />;
    if (state === 'running') return <LoaderCircle />;
    return <CircleDashed />;
  };
  return (
    <div className="page audio-page">
      <header className="archive-heading">
        <div>
          <span className="eyebrow">YouTube</span>
          <h1>{t('audioUI.imports')}</h1>
          <p>{t('audioUI.importHelp')}</p>
        </div>
        <Download size={40} strokeWidth={1} />
      </header>
      <Stack>
        {!system.data?.capabilities.youtube && (
          <Alert color="yellow">{t('audioUI.downloaderOffline')}</Alert>
        )}
        <form
          onSubmit={async (e) => {
            e.preventDefault();
            if (!selectedLibrary || !system.data?.capabilities.youtube) return;
            setBusy(true);
            const ok = await mutate(async () =>
              result(
                await api.POST('/api/v1/imports', {
                  body: { url, libraryId: selectedLibrary.id, follow },
                }),
              ),
            );
            setBusy(false);
            if (ok) setURL('');
          }}
        >
          <Stack>
            <TextInput
              label={t('audioUI.youtubeURL')}
              type="url"
              required
              value={url}
              onChange={(e) => setURL(e.currentTarget.value)}
            />
            <State loading={libraries.isLoading} error={libraries.error}>
              <Select
                label={t('audioUI.destinationLibrary')}
                description={t('audioUI.destinationHelp')}
                placeholder={t('audioUI.chooseLibrary')}
                value={selectedLibrary?.id ?? null}
                onChange={setLibraryID}
                disabled={!eligibleLibraries.length}
                data={eligibleLibraries.map((l) => ({ value: l.id, label: l.name }))}
              />
              {!eligibleLibraries.length && (
                <Alert color="blue">
                  {t(user?.role === 'admin' ? 'audioUI.noLibraryAdmin' : 'audioUI.noLibraryUser')}
                  {user?.role === 'admin' && (
                    <Button component={Link} to="/admin/libraries" variant="subtle" mt="xs">
                      {t('archive.manage')}
                    </Button>
                  )}
                </Alert>
              )}
            </State>
            <Checkbox
              label={t('audioUI.follow')}
              checked={follow}
              onChange={(e) => setFollow(e.currentTarget.checked)}
            />
            <Button
              type="submit"
              loading={busy}
              disabled={!selectedLibrary || !!libraries.error || !system.data?.capabilities.youtube}
            >
              {t(follow ? 'audioUI.subscribe' : 'audioUI.preview')}
            </Button>
          </Stack>
        </form>
        <State loading={sources.isLoading} error={sources.error}>
          {sources.data?.items.map((source) => (
            <section key={source.id} className="audio-source">
              <Group justify="space-between">
                <h2>{source.title || t('audioUI.preparing')}</h2>
                <Badge>
                  {t(
                    source.paused
                      ? 'audioUI.paused'
                      : source.follow
                        ? 'audioUI.following'
                        : 'audioUI.once',
                  )}
                </Badge>
              </Group>
              <a href={source.url} target="_blank" rel="noreferrer">
                {source.url}
              </a>
              <Group mt="sm">
                <Button
                  disabled={source.paused || !source.entries.length}
                  onClick={() =>
                    void mutate(async () =>
                      result(
                        await api.POST('/api/v1/import-sources/{id}/download', {
                          params: { path: { id: source.id } },
                        }),
                      ),
                    )
                  }
                >
                  {t('audioUI.downloadRetry')}
                </Button>
                <Button
                  variant="default"
                  disabled={source.paused}
                  leftSection={<RefreshCw size={16} />}
                  onClick={() =>
                    void mutate(async () =>
                      result(
                        await api.POST('/api/v1/import-sources/{id}/sync', {
                          params: { path: { id: source.id } },
                        }),
                      ),
                    )
                  }
                >
                  {t('audioUI.sync')}
                </Button>
                <Button
                  variant="subtle"
                  onClick={() =>
                    void mutate(async () =>
                      result(
                        await api.PUT('/api/v1/import-sources/{id}', {
                          params: { path: { id: source.id } },
                          body: { follow: source.follow, paused: !source.paused },
                        }),
                      ),
                    )
                  }
                >
                  {t(source.paused ? 'resume' : 'pause')}
                </Button>
                <Button
                  variant="subtle"
                  onClick={() =>
                    void mutate(async () =>
                      result(
                        await api.PUT('/api/v1/import-sources/{id}', {
                          params: { path: { id: source.id } },
                          body: { follow: !source.follow, paused: source.paused },
                        }),
                      ),
                    )
                  }
                >
                  {t(source.follow ? 'audioUI.unfollow' : 'audioUI.follow')}
                </Button>
                <Button variant="subtle" color="red" onClick={() => setRemove(source.id)}>
                  {t('remove')}
                </Button>
              </Group>
              <ScrollArea.Autosize mah={260}>
                <Table>
                  <Table.Tbody>
                    {source.entries.map((entry) => (
                      <Table.Tr key={entry.videoId}>
                        <Table.Td>{entry.ordinal}</Table.Td>
                        <Table.Td>{entry.title}</Table.Td>
                        <Table.Td>
                          {t(entry.error ? audioErrorKey(entry.error) : `audioUI.${entry.state}`)}
                        </Table.Td>
                      </Table.Tr>
                    ))}
                  </Table.Tbody>
                </Table>
              </ScrollArea.Autosize>
            </section>
          ))}
        </State>
        <section className="audio-jobs" aria-labelledby="audio-jobs-title">
          <header className="audio-jobs-heading">
            <div>
              <span className="eyebrow">Activity</span>
              <h2 id="audio-jobs-title">{t('audioUI.viewJobs')}</h2>
            </div>
            <Badge variant="light" color="gray">
              {jobs.data?.items.length ?? 0}
            </Badge>
          </header>
          <State loading={jobs.isLoading} error={jobs.error}>
            <div className="audio-job-list">
              {jobs.data?.items.map((job) => {
                const JobKindIcon = job.kind === 'audio_tags' ? Tag : Download;
                const active = ['pending', 'running'].includes(job.state);
                return (
                  <article key={job.id} className="audio-job" data-state={job.state}>
                    <div className="audio-job-status" aria-hidden="true">
                      <JobStateIcon state={job.state} />
                    </div>
                    <div className="audio-job-body">
                      <header>
                        <span className="audio-job-kind">
                          <JobKindIcon size={15} />
                          {t(`audioUI.${job.kind}`)}
                        </span>
                        <Badge variant="light" color={jobStateColor(job.state)}>
                          {t(`audioUI.${job.state}`)}
                        </Badge>
                      </header>
                      {job.resourceName ? (
                        <Link className="audio-job-resource" to={job.itemId ? `/item/${job.itemId}` : '/imports'}>
                          {job.resourceName}
                        </Link>
                      ) : (
                        <strong className="audio-job-resource">{t(`audioUI.${job.kind}`)}</strong>
                      )}
                      {active && (
                        <div className="audio-job-progress">
                          <Progress value={job.progress} animated={job.state === 'running'} />
                          <span>{job.progress}%</span>
                        </div>
                      )}
                      {job.error && <Alert color="red">{t(audioErrorKey(job.error))}</Alert>}
                      <footer>
                        <span>{t('audioUI.jobReference', { id: job.id })}</span>
                        <span>{new Date(job.updatedAt).toLocaleString()}</span>
                      </footer>
                    </div>
                    {active && (
                      <Button
                        className="audio-job-cancel"
                        variant="subtle"
                        color="gray"
                        aria-label={t('cancel')}
                        onClick={() =>
                          void mutate(async () =>
                            result(
                              await api.POST('/api/v1/audio/jobs/{id}/cancel', {
                                params: { path: { id: job.id } },
                              }),
                            ),
                          )
                        }
                      >
                        <X size={16} />
                      </Button>
                    )}
                  </article>
                );
              })}
            </div>
          </State>
        </section>
        {user?.role === 'admin' && <DownloadSettings />}
      </Stack>
      <Modal
        opened={Boolean(remove)}
        onClose={() => setRemove(undefined)}
        title={t('remove')}
        scrollAreaComponent={ScrollArea.Autosize}
      >
        <p>{t('audioUI.removeSource')}</p>
        <Button
          color="red"
          onClick={async () => {
            if (
              remove &&
              (await mutate(async () =>
                result(
                  await api.DELETE('/api/v1/import-sources/{id}', {
                    params: { path: { id: remove } },
                  }),
                ),
              ))
            )
              setRemove(undefined);
          }}
        >
          {t('remove')}
        </Button>
      </Modal>
    </div>
  );
}
function DownloadSettings() {
  const data = useResource('download-settings', (signal) =>
    api.GET('/api/v1/admin/downloads', { signal }).then(result),
  );
  return (
    <State loading={data.isLoading} error={data.error}>
      {data.data && <DownloadSettingsForm key={JSON.stringify(data.data)} value={data.data} />}
    </State>
  );
}
function DownloadSettingsForm({ value }: { value: DTO<'DownloadSettingsDTO'> }) {
  const { t } = useTranslation();
  const [maxConcurrent, setMax] = useState(value.maxConcurrent);
  const [minFreeGiB, setFree] = useState(value.minFreeGiB);
  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        void mutate(async () =>
          result(await api.PUT('/api/v1/admin/downloads', { body: { maxConcurrent, minFreeGiB } })),
        );
      }}
    >
      <Stack>
        <h2>{t('audioUI.downloadSettings')}</h2>
        <NumberInput
          label={t('audioUI.concurrent')}
          value={maxConcurrent}
          min={1}
          max={8}
          onChange={(v) => setMax(Number(v))}
        />
        <NumberInput
          label={t('audioUI.freeSpace')}
          value={minFreeGiB}
          min={1}
          max={100000}
          onChange={(v) => setFree(Number(v))}
        />
        <Button type="submit">{t('save')}</Button>
      </Stack>
    </form>
  );
}

export function AudioArtists({
  libraryId,
  value,
  onChange,
}: {
  libraryId: string;
  value: string | null;
  onChange: (value: string | null) => void;
}) {
  const { t } = useTranslation();
  const data = useResource(['audio-artists', libraryId], (signal) =>
    api
      .GET('/api/v1/libraries/{id}/artists', { params: { path: { id: libraryId } }, signal })
      .then(result),
  );
  return (
    <Select
      mb="md"
      searchable
      clearable
      label={t('audioUI.artists')}
      value={value}
      onChange={onChange}
      data={data.data?.items ?? []}
    />
  );
}
