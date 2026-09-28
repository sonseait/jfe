import { useState } from 'react';
import { useDebouncedValue } from '@mantine/hooks';
import {
  ActionIcon,
  Alert,
  Badge,
  Button,
  Checkbox,
  Group,
  Modal,
  MultiSelect,
  NumberInput,
  PasswordInput,
  Progress,
  Select,
  Stack,
  TagsInput,
  Tabs,
  Textarea,
  TextInput,
} from '@mantine/core';
import {
  ArrowUpRight,
  Clock,
  Cpu,
  Film,
  Music2,
  Folder,
  FolderCog,
  HardDrive,
  Library,
  Plus,
  RefreshCw,
  Search,
  Settings2,
  ShieldCheck,
  Trash2,
  Tv,
  Users,
} from 'lucide-react';
import { Link, NavLink, useParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { api, result, useAuth, useResource, type DTO } from './api';
import { Confirm, State, mutate } from './shared';
import GeneralSettings from './GeneralSettings';
import { audioErrorKey } from './audio-errors';
export default function Admin() {
  const { t } = useTranslation();
  const { tab = 'libraries' } = useParams();
  return (
    <div className="page">
      <h1>{t('admin')}</h1>
      <div className="settings-layout">
        <nav className="settings-nav">
          {[
            [Settings2, 'general', 'general.title'],
            [FolderCog, 'libraries', 'manage.librarySettings'],
            [Users, 'users', 'users'],
            [Cpu, 'encoding', 'manage.transcoding'],
            [Clock, 'jobs', 'manage.tasks'],
          ].map(([Icon, id, label]) => {
            const Symbol = Icon as typeof Users;
            return (
              <NavLink key={String(id)} to={`/admin/${id}`}>
                <Symbol size={18} />
                {t(String(label))}
              </NavLink>
            );
          })}
        </nav>
        <section
          className={`settings-panel admin-panel ${tab === 'libraries' ? 'library-management-panel' : ''}`}
        >
          {tab === 'general' ? (
            <GeneralSettings />
          ) : tab === 'users' ? (
            <UsersPanel />
          ) : tab === 'encoding' ? (
            <Encoding />
          ) : tab === 'jobs' ? (
            <Jobs />
          ) : (
            <LibrariesPanel />
          )}
        </section>
      </div>
    </div>
  );
}
function LibrariesPanel() {
  const { t } = useTranslation();
  const data = useResource(
    'libraries',
    async (signal) => result(await api.GET('/api/v1/libraries', { signal })),
    true,
    3000,
  );
  const [editing, setEditing] = useState<DTO<'LibraryDTO'> | null>();
  const [remove, setRemove] = useState<DTO<'LibraryDTO'>>();
  const [search, setSearch] = useState('');
  const [kind, setKind] = useState('all');
  const libraries = data.data?.items ?? [];
  const visible = libraries.filter(
    (l) =>
      (kind === 'all' || l.kind === kind) &&
      l.name.toLocaleLowerCase().includes(search.toLocaleLowerCase()),
  );
  const active = libraries.filter(
    (l) => l.scan?.state === 'pending' || l.scan?.state === 'running',
  ).length;
  return (
    <div className="library-management">
      <header className="library-management-heading">
        <div>
          <span className="eyebrow">{t('libraryAdmin.eyebrow')}</span>
          <h2>{t('manage.librarySettings')}</h2>
          <p>{t('libraryAdmin.description')}</p>
        </div>
        <Button leftSection={<Plus size={17} />} onClick={() => setEditing(null)}>
          {t('manage.addLibrary')}
        </Button>
      </header>
      {data.data && (
        <div className="library-management-stats">
          <div>
            <Library size={19} />
            <strong>{libraries.length}</strong>
            <span>{t('libraries')}</span>
          </div>
          <div>
            <HardDrive size={19} />
            <strong>{libraries.reduce((n, l) => n + l.fileCount, 0).toLocaleString()}</strong>
            <span>{t('archive.mediaFiles')}</span>
          </div>
          <div>
            <RefreshCw size={19} />
            <strong>{active}</strong>
            <span>{t('libraryAdmin.activeScans')}</span>
          </div>
        </div>
      )}
      <div className="library-management-toolbar">
        <TextInput
          aria-label={t('archive.findLibrary')}
          placeholder={t('archive.findLibrary')}
          leftSection={<Search size={16} />}
          value={search}
          onChange={(e) => setSearch(e.currentTarget.value)}
        />
        <Select
          aria-label={t('manage.contentType')}
          allowDeselect={false}
          value={kind}
          onChange={(v) => setKind(v ?? 'all')}
          data={[
            { value: 'all', label: t('libraryAdmin.allTypes') },
            { value: 'movies', label: t('movies') },
            { value: 'series', label: t('series') },
            ...['music', 'podcasts', 'audiobooks'].map((value) => ({
              value,
              label: t(`audioUI.${value}`),
            })),
          ]}
        />
      </div>
      <State loading={data.isLoading} error={data.error}>
        <div className="library-management-list">
          {visible.map((library) => (
            <LibraryEntry
              key={library.id}
              library={library}
              edit={() => setEditing(library)}
              remove={() => setRemove(library)}
            />
          ))}
        </div>
        {!visible.length && (
          <div className="library-management-empty">
            <FolderCog size={36} strokeWidth={1} />
            <h3>{t(libraries.length ? 'empty' : 'libraryAdmin.empty')}</h3>
            <p>{t(libraries.length ? 'archive.changeSearch' : 'libraryAdmin.emptyHelp')}</p>
            {!libraries.length && (
              <Button
                variant="default"
                leftSection={<Plus size={16} />}
                onClick={() => setEditing(null)}
              >
                {t('manage.addLibrary')}
              </Button>
            )}
          </div>
        )}
      </State>
      <div className="library-management-note">
        <ShieldCheck size={18} />
        <span>{t('manage.libraryRemoveHelp')}</span>
      </div>
      <Modal
        size="lg"
        classNames={{ content: 'library-editor-modal' }}
        opened={editing !== undefined}
        onClose={() => setEditing(undefined)}
        title={t(editing ? 'libraryAdmin.edit' : 'manage.addLibrary')}
      >
        {editing !== undefined && (
          <LibraryForm
            key={editing?.id ?? 'new'}
            value={editing}
            close={() => setEditing(undefined)}
          />
        )}
      </Modal>
      <Confirm
        opened={Boolean(remove)}
        name={remove?.name ?? ''}
        close={() => setRemove(undefined)}
        run={async () =>
          result(
            await api.DELETE('/api/v1/libraries/{id}', { params: { path: { id: remove!.id } } }),
          )
        }
      />
    </div>
  );
}
function LibraryEntry({
  library: l,
  edit,
  remove,
}: {
  library: DTO<'LibraryDTO'>;
  edit: () => void;
  remove: () => void;
}) {
  const { t, i18n } = useTranslation();
  const [busy, setBusy] = useState(false);
  const [forceMetadata, setForceMetadata] = useState(false);
  const active = l.scan?.state === 'running' || l.scan?.state === 'pending';
  const Icon = l.kind === 'movies' ? Film : l.kind === 'series' ? Tv : Music2;
  const scanned = new Date(l.lastScanAt).getTime() > 86400000;
  const state = l.scan?.state;
  return (
    <article className="admin-library managed-library">
      <header className="managed-library-heading">
        <span className={`managed-library-icon ${l.kind}`}>
          <Icon size={24} strokeWidth={1.2} />
        </span>
        <div>
          <span className="managed-library-kind">
            {t(l.kind === 'movies' || l.kind === 'series' ? l.kind : `audioUI.${l.kind}`)}
          </span>
          <h3>{l.name}</h3>
        </div>
        <Badge
          variant="light"
          color={
            active
              ? 'orange'
              : state === 'failed'
                ? 'red'
                : state === 'completed'
                  ? 'green'
                  : 'gray'
          }
        >
          {t(state ? `native.state.${state}` : 'libraryAdmin.notScanned')}
        </Badge>
      </header>
      <div className="managed-library-metrics">
        <div>
          <span>{t('libraryAdmin.files')}</span>
          <strong>{t('native.fileCount', { count: l.fileCount })}</strong>
        </div>
        <div>
          <span>{t('libraryAdmin.lastScan')}</span>
          <strong>
            {scanned
              ? new Intl.DateTimeFormat(i18n.language, {
                  dateStyle: 'medium',
                  timeStyle: 'short',
                }).format(new Date(l.lastScanAt))
              : t('libraryAdmin.never')}
          </strong>
        </div>
        <div>
          <span>{t('libraryAdmin.schedule')}</span>
          <strong>
            {l.scanIntervalHours
              ? t('libraryAdmin.everyHours', { count: l.scanIntervalHours })
              : t('libraryAdmin.manual')}
          </strong>
        </div>
      </div>
      <div className="managed-library-paths">
        <span className="managed-library-paths-label">
          <Folder size={14} />
          {t('manage.paths')}
        </span>
        {l.paths.map((path) => (
          <code key={path}>{path}</code>
        ))}
      </div>
      {(active || (l.scan && l.scan.totalFiles > 0)) && (
        <div className="managed-library-progress">
          <div>
            <span>
              {t(
                active && !l.scan?.totalFiles
                  ? state === 'pending'
                    ? 'native.state.pending'
                    : 'native.discovering'
                  : 'native.job.scan',
              )}
            </span>
            <span>
              {l.scan!.processedFiles}/{l.scan!.totalFiles} · {l.scan!.progress}%
            </span>
          </div>
          <Progress
            value={l.scan!.progress}
            animated={active}
            aria-label={t('native.job.scan')}
            color={state === 'failed' ? 'red' : 'orange'}
            size={3}
          />
        </div>
      )}
      {state === 'failed' && (
        <p className="managed-library-error">
          {t('libraryAdmin.scanFailed')}{' '}
          <Link to="/admin/jobs">
            {t('manage.tasks')}
            <ArrowUpRight size={12} />
          </Link>
        </p>
      )}
      <footer className="managed-library-actions">
        <Button size="xs" variant="default" leftSection={<Settings2 size={14} />} onClick={edit}>
          {t('settings')}
        </Button>
        <Button
          size="xs"
          variant="subtle"
          leftSection={<RefreshCw size={14} />}
          disabled={active}
          loading={busy}
          onClick={async () => {
            setBusy(true);
            try {
              await mutate(async () =>
                result(
                  await api.POST('/api/v1/libraries/{id}/scan', {
                    params: { path: { id: l.id }, query: { forceMetadata } },
                  }),
                ),
              );
            } finally {
              setBusy(false);
            }
          }}
        >
          {t('scanLibrary')}
        </Button>
        <Checkbox
          label={t('scan.forceMetadata')}
          description={t('scan.forceMetadataHelp')}
          checked={forceMetadata}
          disabled={active || busy}
          onChange={(event) => setForceMetadata(event.currentTarget.checked)}
          size="xs"
        />
        <ActionIcon
          variant="subtle"
          color="red"
          aria-label={t('remove')}
          title={t('remove')}
          onClick={remove}
        >
          <Trash2 size={16} />
        </ActionIcon>
      </footer>
    </article>
  );
}
function LibraryForm({ value, close }: { value: DTO<'LibraryDTO'> | null; close: () => void }) {
  const { t } = useTranslation();
  const [name, setName] = useState(value?.name ?? '');
  const [kind, setKind] = useState<DTO<'LibraryRequest'>['kind']>(
    (value?.kind as DTO<'LibraryRequest'>['kind']) ?? 'movies',
  );
  const [paths, setPaths] = useState(value?.paths ?? []);
  const [interval, setInterval] = useState(value?.scanIntervalHours ?? 0);
  const [busy, setBusy] = useState(false);
  const [confirm, setConfirm] = useState(false);
  async function save() {
    setBusy(true);
    const body = { name: name.trim(), kind, paths, scanIntervalHours: interval };
    const ok = await mutate(async () =>
      value
        ? result(
            await api.PUT('/api/v1/libraries/{id}', { params: { path: { id: value.id } }, body }),
          )
        : result(await api.POST('/api/v1/libraries', { body })),
    );
    setBusy(false);
    if (ok) close();
  }
  return (
    <>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          if (value?.paths.some((p) => !paths.includes(p))) setConfirm(true);
          else void save();
        }}
      >
        <fieldset className="admin-fieldset library-editor" disabled={busy}>
          <Stack>
            <div className="library-editor-section">
              <span>01</span>
              <div>
                <h3>{t('libraryAdmin.identity')}</h3>
                <p>{t('libraryAdmin.identityHelp')}</p>
              </div>
            </div>
            <TextInput
              label={t('name')}
              required
              value={name}
              onChange={(e) => setName(e.currentTarget.value)}
            />
            <Select
              label={t('manage.contentType')}
              allowDeselect={false}
              disabled={Boolean(value)}
              value={kind}
              data={[
                { value: 'movies', label: t('movies') },
                { value: 'series', label: t('series') },
                ...['music', 'podcasts', 'audiobooks'].map((value) => ({
                  value,
                  label: t(`audioUI.${value}`),
                })),
              ]}
              onChange={(v) => setKind(v as DTO<'LibraryRequest'>['kind'])}
            />
            <div className="library-editor-section">
              <span>02</span>
              <div>
                <h3>{t('manage.paths')}</h3>
                <p>
                  {t(
                    ['music', 'podcasts', 'audiobooks'].includes(kind)
                      ? 'audioUI.pathsHelp'
                      : 'libraryAdmin.pathsHelp',
                  )}
                </p>
              </div>
            </div>
            <TagsInput
              label={t('manage.paths')}
              description={t('manage.serverPathHelp')}
              splitChars={[]}
              value={paths}
              onChange={setPaths}
            />
            <div className="library-editor-section">
              <span>03</span>
              <div>
                <h3>{t('libraryAdmin.schedule')}</h3>
                <p>{t('libraryAdmin.scheduleHelp')}</p>
              </div>
            </div>
            <NumberInput
              label={t('native.scanInterval')}
              min={0}
              max={8760}
              value={interval}
              onChange={(v) => setInterval(Number(v))}
            />
            <Group justify="flex-end" className="library-editor-footer">
              <Button variant="default" disabled={busy} onClick={close}>
                {t('cancel')}
              </Button>
              <Button
                type="submit"
                loading={busy}
                disabled={
                  (!paths.length && !['music', 'podcasts', 'audiobooks'].includes(kind)) ||
                  !name.trim()
                }
              >
                {t('save')}
              </Button>
            </Group>
          </Stack>
        </fieldset>
      </form>
      <Modal opened={confirm} onClose={() => setConfirm(false)} title={t('confirm')}>
        <p>{t('manage.removePathsConfirm')}</p>
        <Button
          onClick={() => {
            setConfirm(false);
            void save();
          }}
        >
          {t('confirm')}
        </Button>
      </Modal>
    </>
  );
}
function UsersPanel() {
  const { t } = useTranslation();
  const data = useResource('users', async (signal) =>
    result(await api.GET('/api/v1/admin/users', { signal })),
  );
  const [search, setSearch] = useState('');
  const [role, setRole] = useState('all');
  const libraries = useResource('libraries', async (signal) =>
    result(await api.GET('/api/v1/libraries', { signal })),
  );
  const [editing, setEditing] = useState<DTO<'UserDTO'> | null>();
  const [remove, setRemove] = useState<DTO<'UserDTO'>>();
  const self = useAuth((s) => s.user?.id);
  return (
    <Stack className="users-admin">
      <div className="admin-section-heading">
        <span className="eyebrow">{t('usersAdmin.eyebrow')}</span>
        <Group justify="space-between">
          <div>
            <h2>{t('users')}</h2>
            <p>{t('usersAdmin.description')}</p>
          </div>
          <Button leftSection={<Plus size={17} />} onClick={() => setEditing(null)}>
            {t('usersAdmin.create')}
          </Button>
        </Group>
      </div>
      <div className="admin-stat-strip">
        <div>
          <strong>{data.data?.items.length ?? 0}</strong>
          <span>{t('usersAdmin.accounts')}</span>
        </div>
        <div>
          <strong>{data.data?.items.filter((u) => u.role === 'admin').length ?? 0}</strong>
          <span>{t('administrator')}</span>
        </div>
        <div>
          <strong>{data.data?.items.filter((u) => u.disabled).length ?? 0}</strong>
          <span>{t('disabled')}</span>
        </div>
      </div>
      <Group>
        <TextInput
          className="admin-search"
          leftSection={<Search size={16} />}
          aria-label={t('usersAdmin.search')}
          placeholder={t('usersAdmin.search')}
          value={search}
          onChange={(e) => setSearch(e.currentTarget.value)}
        />
        <Select
          aria-label={t('usersAdmin.filter')}
          allowDeselect={false}
          value={role}
          onChange={(v) => setRole(v ?? 'all')}
          data={[
            { value: 'all', label: t('usersAdmin.all') },
            { value: 'admin', label: t('administrator') },
            { value: 'user', label: t('usersAdmin.viewer') },
          ]}
        />
      </Group>
      <State loading={data.isLoading} error={data.error}>
        <div className="users-grid">
          {data.data?.items
            .filter(
              (u) =>
                u.username.toLowerCase().includes(search.toLowerCase()) &&
                (role === 'all' || u.role === role),
            )
            .map((u) => (
              <article className={`user-card ${u.disabled ? 'is-disabled' : ''}`} key={u.id}>
                <Group justify="space-between">
                  <span className="avatar">{u.username.slice(0, 2).toUpperCase()}</span>
                  <Badge color={u.disabled ? 'gray' : 'green'} variant="light">
                    {t(u.disabled ? 'disabled' : 'usersAdmin.active')}
                  </Badge>
                </Group>
                <h3>
                  {u.username} {u.id === self && <small>{t('usersAdmin.you')}</small>}
                </h3>
                <span className="user-role">
                  <ShieldCheck size={15} />
                  {t(u.role === 'admin' ? 'administrator' : 'usersAdmin.viewer')}
                </span>
                <div className="user-access">
                  <span className="eyebrow">{t('allowedLibraries')}</span>
                  {u.role === 'admin' ? (
                    <p>{t('usersAdmin.allLibraries')}</p>
                  ) : u.libraryIds.length ? (
                    <Group gap={6}>
                      {u.libraryIds.map((id) => (
                        <Badge key={id} variant="light" color="gray">
                          {libraries.data?.items.find((l) => l.id === id)?.name ?? t('libraries')}
                        </Badge>
                      ))}
                    </Group>
                  ) : (
                    <p>{t('usersAdmin.noLibraries')}</p>
                  )}
                </div>
                <Group className="user-card-actions" justify="space-between">
                  <Button variant="default" onClick={() => setEditing(u)}>
                    {t('usersAdmin.edit')}
                  </Button>
                  <ActionIcon
                    aria-label={t('usersAdmin.remove', { name: u.username })}
                    variant="subtle"
                    color="red"
                    disabled={u.id === self}
                    onClick={() => setRemove(u)}
                  >
                    <Trash2 size={17} />
                  </ActionIcon>
                </Group>
              </article>
            ))}
        </div>
        {data.data &&
          !data.data.items.some(
            (u) =>
              u.username.toLowerCase().includes(search.toLowerCase()) &&
              (role === 'all' || u.role === role),
          ) && <Alert color="gray">{t('usersAdmin.noResults')}</Alert>}
      </State>
      <Modal
        opened={editing !== undefined}
        onClose={() => setEditing(undefined)}
        title={t(editing ? 'usersAdmin.edit' : 'usersAdmin.create')}
        size="lg"
      >
        {editing !== undefined && (
          <UserForm
            value={editing}
            close={() => setEditing(undefined)}
            key={editing?.id ?? 'new'}
          />
        )}
      </Modal>
      <Confirm
        opened={Boolean(remove)}
        name={remove?.username ?? ''}
        close={() => setRemove(undefined)}
        run={async () =>
          result(
            await api.DELETE('/api/v1/admin/users/{id}', { params: { path: { id: remove!.id } } }),
          )
        }
      />
    </Stack>
  );
}
function UserForm({ value, close }: { value: DTO<'UserDTO'> | null; close: () => void }) {
  const { t } = useTranslation();
  const [name, setName] = useState(value?.username ?? '');
  const [password, setPassword] = useState('');
  const [admin, setAdmin] = useState(value?.role === 'admin');
  const [disabled, setDisabled] = useState(value?.disabled ?? false);
  const [ids, setIDs] = useState(value?.libraryIds ?? []);
  const [importIDs, setImportIDs] = useState(value?.importLibraryIds ?? []);
  const [busy, setBusy] = useState(false);
  const self = useAuth((s) => s.user?.id);
  const libraries = useResource(
    'libraries',
    async (signal) => result(await api.GET('/api/v1/libraries', { signal })),
    true,
    3000,
  );
  return (
    <form
      onSubmit={async (e) => {
        e.preventDefault();
        setBusy(true);
        const body = {
          username: name,
          ...(password ? { password } : {}),
          role: admin ? ('admin' as const) : ('user' as const),
          disabled,
          libraryIds: ids,
          importLibraryIds: importIDs.filter((id) => ids.includes(id)),
        };
        const ok = await mutate(async () =>
          value
            ? result(
                await api.PUT('/api/v1/admin/users/{id}', {
                  params: { path: { id: value.id } },
                  body,
                }),
              )
            : result(await api.POST('/api/v1/admin/users', { body })),
        );
        setBusy(false);
        if (ok) close();
      }}
    >
      <fieldset disabled={busy} className="admin-fieldset">
        <Stack>
          <TextInput
            label={t('username')}
            required
            value={name}
            onChange={(e) => setName(e.currentTarget.value)}
          />
          <PasswordInput
            label={t(value ? 'newPassword' : 'password')}
            required={!value}
            minLength={8}
            value={password}
            onChange={(e) => setPassword(e.currentTarget.value)}
          />
          <Checkbox
            label={t('administrator')}
            checked={admin}
            disabled={value?.id === self}
            onChange={(e) => setAdmin(e.currentTarget.checked)}
          />
          <Checkbox
            label={t('disabled')}
            checked={disabled}
            disabled={value?.id === self}
            onChange={(e) => setDisabled(e.currentTarget.checked)}
          />
          {!admin && (
            <MultiSelect
              label={t('allowedLibraries')}
              value={ids}
              onChange={setIDs}
              disabled={!libraries.data}
              data={libraries.data?.items.map((l) => ({ value: l.id, label: l.name })) ?? []}
            />
          )}
          {!admin && (
            <MultiSelect
              label={t('audioUI.importPermission')}
              description={t('audioUI.permissionHelp')}
              value={importIDs.filter((id) => ids.includes(id))}
              onChange={setImportIDs}
              data={
                libraries.data?.items
                  .filter(
                    (l) =>
                      ids.includes(l.id) && ['music', 'podcasts', 'audiobooks'].includes(l.kind),
                  )
                  .map((l) => ({ value: l.id, label: l.name })) ?? []
              }
            />
          )}
          <Button type="submit" loading={busy}>
            {t('save')}
          </Button>
        </Stack>
      </fieldset>
    </form>
  );
}
function Encoding() {
  const { t } = useTranslation();
  const data = useResource('encoding', async (signal) =>
    result(await api.GET('/api/v1/admin/encoding', { signal })),
  );
  return (
    <State loading={data.isLoading} error={data.error}>
      {data.data && <EncodingForm initial={data.data} />}
      <Alert color="gray" mt="lg">
        {t('native.nvencOnly')}
      </Alert>
    </State>
  );
}
function EncodingForm({ initial }: { initial: DTO<'EncodingDTO'> }) {
  const { t } = useTranslation();
  const [value, setValue] = useState(initial);
  const [busy, setBusy] = useState(false);
  return (
    <form
      onSubmit={async (e) => {
        e.preventDefault();
        setBusy(true);
        await mutate(async () => result(await api.PUT('/api/v1/admin/encoding', { body: value })));
        setBusy(false);
      }}
    >
      <Stack>
        <h2>{t('manage.transcoding')}</h2>
        <Select
          label={t('native.encodingMode')}
          allowDeselect={false}
          value={value.mode}
          data={[
            { value: 'disabled', label: t('native.encodingDisabled') },
            { value: 'nvidia', label: 'NVIDIA NVENC' },
          ]}
          onChange={(v) =>
            setValue((s) => ({ ...s, mode: v === 'nvidia' ? 'nvidia' : 'disabled' }))
          }
        />
        <NumberInput
          required
          disabled={value.mode !== 'nvidia'}
          label={t('native.nvencCQ')}
          min={0}
          max={51}
          value={value.cq}
          onChange={(v) => setValue((s) => ({ ...s, cq: Number(v) }))}
        />
        <NumberInput
          required
          disabled={value.mode !== 'nvidia'}
          label={t('native.gpuDevice')}
          min={0}
          max={31}
          value={value.device}
          onChange={(v) => setValue((s) => ({ ...s, device: Number(v) }))}
        />
        <NumberInput
          required
          label={t('native.maxConcurrent')}
          min={1}
          max={16}
          value={value.maxConcurrent}
          onChange={(v) => setValue((s) => ({ ...s, maxConcurrent: Number(v) }))}
        />
        <Button type="submit" loading={busy}>
          {t('save')}
        </Button>
      </Stack>
    </form>
  );
}
function Jobs() {
  const { t } = useTranslation();
  const [search, setSearch] = useState('');
  const [state, setState] = useState('all');
  const libraries = useResource(
    'libraries',
    async (signal) => result(await api.GET('/api/v1/libraries', { signal })),
    true,
    10000,
  );
  const jobs = useResource(
    'jobs',
    async (signal) => result(await api.GET('/api/v1/admin/jobs', { signal })),
    true,
    3000,
  );
  const workers = useResource(
    'workers',
    async (signal) => result(await api.GET('/api/v1/admin/workers', { signal })),
    true,
    10000,
  );
  return (
    <Stack>
      <section className="task-schedules">
        <h2>{t('libraryAdmin.schedule')}</h2>
        <State loading={libraries.isLoading} error={libraries.error}>
          {libraries.data?.items.map((l) => (
            <div className="task-schedule" key={l.id}>
              <Link to={`/library/${l.id}`}>{l.name}</Link>
              <span>
                {l.scanIntervalHours > 0
                  ? t('libraryAdmin.everyHours', { count: l.scanIntervalHours })
                  : t('libraryAdmin.manual')}
              </span>
              <small>
                {t('libraryAdmin.lastScan')}:{' '}
                {new Date(l.lastScanAt).getFullYear() > 1970
                  ? new Date(l.lastScanAt).toLocaleString()
                  : t('libraryAdmin.never')}
              </small>
            </div>
          ))}
        </State>
      </section>
      <h2>{t('native.workers')}</h2>
      <State loading={workers.isLoading} error={workers.error}>
        {workers.data?.items.length ? (
          workers.data.items.map((w) => (
            <Group key={w.id}>
              <Badge color="green">{w.role}</Badge>
              <span>{new Date(w.heartbeatAt).toLocaleString()}</span>
            </Group>
          ))
        ) : (
          <Alert color="orange">{t('native.noWorkers')}</Alert>
        )}
      </State>
      <div className="admin-section-heading">
        <span className="eyebrow">{t('tasksAdmin.eyebrow')}</span>
        <h2>{t('manage.tasks')}</h2>
        <p>{t('tasksAdmin.description')}</p>
      </div>
      <Group>
        <TextInput
          className="admin-search"
          leftSection={<Search size={16} />}
          aria-label={t('tasksAdmin.search')}
          placeholder={t('tasksAdmin.search')}
          value={search}
          onChange={(e) => setSearch(e.currentTarget.value)}
        />
        <Select
          aria-label={t('tasksAdmin.status')}
          allowDeselect={false}
          value={state}
          onChange={(v) => setState(v ?? 'all')}
          data={['all', 'pending', 'running', 'completed', 'failed', 'cancelled'].map((s) => ({
            value: s,
            label: t(s === 'all' ? 'tasksAdmin.all' : `native.state.${s}`),
          }))}
        />
      </Group>
      <State loading={jobs.isLoading} error={jobs.error}>
        {jobs.data?.items
          .filter(
            (j) =>
              (state === 'all' || j.state === state) &&
              `${j.resourceName} ${j.id}`.toLowerCase().includes(search.toLowerCase()),
          )
          .map((j) => (
            <article className="task-card" key={j.id}>
              <Group justify="space-between">
                <h3>{t(`native.job.${j.kind}`)}</h3>
                <Badge>{t(`native.state.${j.state}`)}</Badge>
              </Group>
              <div className="task-target">
                <span className="eyebrow">{t('tasksAdmin.target')}</span>
                {j.itemId ? (
                  <Link to={`/item/${j.itemId}`}>
                    {j.resourceName || j.resourceId}
                    <ArrowUpRight size={16} />
                  </Link>
                ) : j.libraryId ? (
                  <Link to={`/library/${j.libraryId}`}>
                    {j.resourceName || j.resourceId}
                    <ArrowUpRight size={16} />
                  </Link>
                ) : (
                  <strong>{j.resourceName || j.resourceId}</strong>
                )}
                <p>{t(`tasksAdmin.${j.kind}`)}</p>
              </div>
              <dl className="task-facts">
                <div>
                  <dt>{t('tasksAdmin.worker')}</dt>
                  <dd>{j.role}</dd>
                </div>
                <div>
                  <dt>{t('tasksAdmin.attempts')}</dt>
                  <dd>{j.attempts} / 3</dd>
                </div>
                <div>
                  <dt>{t('tasksAdmin.created')}</dt>
                  <dd>{new Date(j.createdAt).toLocaleString()}</dd>
                </div>
                <div>
                  <dt>{t('tasksAdmin.updated')}</dt>
                  <dd>{new Date(j.updatedAt).toLocaleString()}</dd>
                </div>
              </dl>
              <details className="task-diagnostics">
                <summary>{t('tasksAdmin.diagnostics')}</summary>
                <code>{j.id}</code>
                <p>{t('tasksAdmin.logHint')}</p>
              </details>
              {j.kind === 'scan' && (
                <>
                  <p>
                    {j.processedFiles}/{j.totalFiles} · {j.progress}%
                  </p>
                  <Progress value={j.progress} animated={j.state === 'running'} />
                </>
              )}
              {j.error && (
                <Alert color="red">
                  {j.error === 'Metadata match is ambiguous or missing; identify this item manually'
                    ? t('metadata.ambiguous')
                    : j.error === 'TMDB_TOKEN not configured'
                      ? t('metadata.providerUnavailable')
                      : ['youtube_preview', 'youtube_download', 'audio_tags'].includes(j.kind)
                        ? t(audioErrorKey(j.error))
                        : j.error}
                </Alert>
              )}
              {j.cancelRequested && j.state === 'running' && (
                <Alert color="orange">{t('tasksAdmin.cancelling')}</Alert>
              )}
              {['pending', 'running'].includes(j.state) && (
                <Button
                  variant="subtle"
                  disabled={j.cancelRequested}
                  onClick={() =>
                    void mutate(async () =>
                      result(
                        await api.POST('/api/v1/admin/jobs/{id}/cancel', {
                          params: { path: { id: j.id } },
                        }),
                      ),
                    )
                  }
                >
                  {t('cancel')}
                </Button>
              )}
            </article>
          ))}
      </State>
    </Stack>
  );
}
export function MetadataEditor({
  item,
  opened,
  close,
  sourceName,
}: {
  sourceName?: string;
  item: DTO<'ItemDTO'>;
  opened: boolean;
  close: () => void;
}) {
  const { t } = useTranslation();
  return (
    <Modal
      opened={opened}
      onClose={close}
      title={t('editMetadata')}
      size="xl"
      className="metadata-modal"
    >
      <MetadataForm
        key={`${item.id}:${opened}`}
        item={item}
        close={close}
        sourceName={sourceName}
      />
    </Modal>
  );
}
function MetadataForm({
  item,
  close,
  sourceName,
}: {
  item: DTO<'ItemDTO'>;
  close: () => void;
  sourceName?: string;
}) {
  const { t } = useTranslation();
  const [title, setTitle] = useState(item.title);
  const [year, setYear] = useState(item.year);
  const [overview, setOverview] = useState(item.overview);
  const [locked, setLocked] = useState(item.metadataLocked);
  const [search, setSearch] = useState(sourceName || item.title);
  const [query] = useDebouncedValue(search, 500);
  const [selected, setSelected] = useState<number>();
  const [busy, setBusy] = useState(false);
  const [tab, setTab] = useState<string | null>('manual');
  const matches = useResource(
    ['metadataMatches', query, item.kind, item.year],
    async (signal) => {
      const response = await api.GET('/api/v1/metadata/search', {
        signal,
        params: {
          query: {
            search: query,
            kind: item.kind === 'series' ? 'series' : 'movie',
            year: item.year,
          },
        },
      });
      if (response.response.status === 503) throw new Error('metadata.providerUnavailable');
      return result(response);
    },
    tab === 'identify' && item.kind !== 'episode' && Boolean(query.trim()),
  );
  const chosen =
    matches.data?.items.find((m) => m.id === selected) ??
    matches.data?.items.find((m) => m.recommended);
  return (
    <div className="metadata-editor">
      <div className="metadata-intro">
        <Film size={25} />
        <div>
          <strong>{item.title}</strong>
          <p>{sourceName || t('metadata.manualHelp')}</p>
        </div>
      </div>
      <Tabs value={tab} onChange={setTab}>
        <Tabs.List grow>
          <Tabs.Tab value="manual">{t('metadata.editDetails')}</Tabs.Tab>
          <Tabs.Tab value="identify" disabled={item.kind === 'episode'}>
            {t('metadata.identify')}
          </Tabs.Tab>
        </Tabs.List>
        <Tabs.Panel value="manual" pt="lg">
          <form
            onSubmit={async (e) => {
              e.preventDefault();
              setBusy(true);
              const ok = await mutate(async () =>
                result(
                  await api.PUT('/api/v1/items/{id}/metadata', {
                    params: { path: { id: item.id } },
                    body: { title, year, overview, locked, providerId: item.providerId },
                  }),
                ),
              );
              setBusy(false);
              if (ok) close();
            }}
          >
            <fieldset disabled={busy} className="admin-fieldset">
              <Stack>
                <div className="metadata-fields">
                  <TextInput
                    label={t('title')}
                    value={title}
                    required
                    onChange={(e) => setTitle(e.currentTarget.value)}
                  />
                  <NumberInput
                    label={t('year')}
                    value={year}
                    min={0}
                    max={9999}
                    onChange={(v) => setYear(Number(v))}
                  />
                </div>
                <Textarea
                  label={t('overview')}
                  value={overview}
                  minRows={6}
                  autosize
                  maxRows={12}
                  onChange={(e) => setOverview(e.currentTarget.value)}
                />
                <div className="metadata-lock">
                  <Checkbox
                    label={t('lockMetadata')}
                    checked={locked}
                    onChange={(e) => setLocked(e.currentTarget.checked)}
                  />
                  <p>{t('metadata.lockHelp')}</p>
                </div>
                {item.providerId && <Badge variant="light">TMDB · {item.providerId}</Badge>}
                {item.kind === 'episode' && <Alert color="gray">{t('metadata.episodeHelp')}</Alert>}
                <Group justify="flex-end">
                  <Button variant="default" onClick={close}>
                    {t('cancel')}
                  </Button>
                  <Button type="submit" loading={busy}>
                    {t('save')}
                  </Button>
                </Group>
              </Stack>
            </fieldset>
          </form>
        </Tabs.Panel>
        <Tabs.Panel value="identify" pt="lg">
          <Stack>
            <Alert color="gray">{t('metadata.matchHelp')}</Alert>
            <TextInput
              label={t('metadata.searchLabel')}
              leftSection={<Search size={17} />}
              value={search}
              onChange={(e) => {
                setSearch(e.currentTarget.value);
                setSelected(undefined);
              }}
            />
            {matches.data && (
              <p className="muted">
                {t('metadata.searchingFor', {
                  title: matches.data.search,
                  year: matches.data.year || '—',
                })}
              </p>
            )}
            {matches.error ? (
              <Alert color="orange">
                {t(
                  matches.error.message === 'metadata.providerUnavailable'
                    ? 'metadata.providerUnavailable'
                    : 'error',
                )}
              </Alert>
            ) : (
              <State loading={matches.isFetching} error={null}>
                {matches.data?.items.length === 0 && (
                  <Alert color="gray">{t('metadata.noMatches')}</Alert>
                )}
                {matches.data &&
                  matches.data.items.length > 0 &&
                  !matches.data.items.some((m) => m.recommended) && (
                    <Alert color="orange">{t('metadata.ambiguous')}</Alert>
                  )}
                <div className="metadata-matches">
                  {matches.data?.items.map((m) => (
                    <button
                      type="button"
                      aria-pressed={chosen?.id === m.id}
                      disabled={busy || matches.isFetching || query !== search}
                      className={`metadata-match ${chosen?.id === m.id ? 'is-selected' : ''}`}
                      key={m.id}
                      onClick={() => setSelected(m.id)}
                    >
                      <Group justify="space-between">
                        <strong>{m.title}</strong>
                        <Badge color={m.recommended ? 'green' : 'gray'} variant="light">
                          {m.recommended
                            ? t('metadata.recommended')
                            : t('metadata.score', { score: m.score })}
                        </Badge>
                      </Group>
                      <span>
                        {m.year.slice(0, 4) || t('detail.unknown')} · TMDB {m.id}
                      </span>
                      <p>{m.overview || t('detail.noOverview')}</p>
                    </button>
                  ))}
                </div>
              </State>
            )}
            <div className="metadata-apply">
              <p>{t('metadata.applyHelp')}</p>
              <Button
                loading={busy}
                disabled={!chosen || matches.isFetching || query !== search}
                onClick={async () => {
                  if (!chosen) return;
                  setBusy(true);
                  const ok = await mutate(async () =>
                    result(
                      await api.POST('/api/v1/items/{id}/identify', {
                        params: { path: { id: item.id } },
                        body: { providerId: chosen.id },
                      }),
                    ),
                  );
                  setBusy(false);
                  if (ok) close();
                }}
              >
                {t('metadata.apply')}
              </Button>
            </div>
          </Stack>
        </Tabs.Panel>
      </Tabs>
    </div>
  );
}
