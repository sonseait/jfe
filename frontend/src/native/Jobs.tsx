import { useState } from 'react';
import {
  Alert,
  Badge,
  Button,
  Group,
  Modal,
  Progress,
  ScrollArea,
  Select,
  Stack,
  Text,
  TextInput,
  Tabs,
} from '@mantine/core';
import { ArrowUpRight, CalendarClock, FileText, Play, Search } from 'lucide-react';
import { Link } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { api, result, useResource, type DTO } from './api';
import { mutate, State } from './shared';
import { audioErrorKey } from './audio-errors';

export default function Jobs() {
  const { t, i18n } = useTranslation();
  const [search, setSearch] = useState('');
  const [state, setState] = useState('all');
  const [role, setRole] = useState('scanner');
  const [selected, setSelected] = useState<DTO<'JobDTO'>>();
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
  const items = jobs.data?.items ?? [];
  const visible = items.filter(
    (j) =>
      j.role === role &&
      (state === 'all' || j.state === state) &&
      `${j.resourceName} ${j.resourceId} ${j.id} ${j.kind}`
        .toLocaleLowerCase()
        .includes(search.toLocaleLowerCase()),
  );
  const date = (value: string | number) => new Date(value).toLocaleString(i18n.language);
  return (
    <Stack gap="xl" className="jobs-dashboard">
      <header className="admin-section-heading">
        <span className="eyebrow">{t('tasksAdmin.eyebrow')}</span>
        <h2>{t('manage.tasks')}</h2>
        <p>{t('jobHistory.description')}</p>
      </header>
      <div className="job-summary">
        {['running', 'pending', 'failed', 'completed'].map((s) => (
          <button
            key={s}
            type="button"
            className={state === s ? 'active' : ''}
            onClick={() => setState(state === s ? 'all' : s)}
          >
            <strong>{items.filter((j) => j.role === role && j.state === s).length}</strong>
            <span>{t(`native.state.${s}`)}</span>
          </button>
        ))}
      </div>
      <section className="job-schedules">
        <Group justify="space-between">
          <h3>
            <CalendarClock size={18} />
            {t('libraryAdmin.schedule')}
          </h3>
          <Link to="/admin/libraries">{t('jobHistory.editSchedules')}</Link>
        </Group>
        <State loading={libraries.isLoading} error={libraries.error}>
          <div className="job-schedule-grid">
            {libraries.data?.items.map((library) => {
              const scanned = new Date(library.lastScanAt).getTime() > 0;
              const next =
                new Date(library.lastScanAt).getTime() + library.scanIntervalHours * 3600000;
              const busy = ['pending', 'running'].includes(library.scan?.state ?? '');
              return (
                <article className="job-schedule-card" key={library.id}>
                  <Group justify="space-between">
                    <Link to={`/library/${library.id}`}>{library.name}</Link>
                    <Badge variant="light" color={library.scanIntervalHours ? 'blue' : 'gray'}>
                      {library.scanIntervalHours
                        ? t('libraryAdmin.everyHours', { count: library.scanIntervalHours })
                        : t('libraryAdmin.manual')}
                    </Badge>
                  </Group>
                  <Text size="sm" c="dimmed">
                    {t('libraryAdmin.lastScan')}:{' '}
                    {scanned ? date(library.lastScanAt) : t('libraryAdmin.never')}
                  </Text>
                  {library.scanIntervalHours > 0 && (
                    <Text size="sm">
                      {t('jobHistory.nextRun')}:{' '}
                      {busy
                        ? t('jobHistory.alreadyQueued')
                        : next <= Date.now()
                          ? t('jobHistory.due')
                          : date(next)}
                    </Text>
                  )}
                  {library.autoSubtitles && (
                    <Text size="sm" c="dimmed">
                      {t('jobHistory.subtitleSchedule')}
                    </Text>
                  )}
                  <Button
                    variant="subtle"
                    size="compact-sm"
                    leftSection={<Play size={14} />}
                    disabled={busy}
                    onClick={() =>
                      void mutate(async () =>
                        result(
                          await api.POST('/api/v1/libraries/{id}/scan', {
                            params: { path: { id: library.id } },
                          }),
                        ),
                      )
                    }
                  >
                    {t('jobHistory.runNow')}
                  </Button>
                </article>
              );
            })}
            {!libraries.data?.items.length && <Text c="dimmed">{t('jobHistory.noSchedules')}</Text>}
          </div>
        </State>
      </section>
      <section className="job-executions">
        <h3>{t('jobHistory.executions')}</h3>
        <Tabs
          value={role}
          onChange={(next) => setRole(next ?? 'scanner')}
          className="task-process-tabs"
        >
          <Tabs.List grow>
            {['scanner', 'transcoder', 'downloader'].map((process) => (
              <Tabs.Tab key={process} value={process}>
                {t(`tasksAdmin.process.${process}`)}
                <Badge variant="light" ml={6} size="sm">
                  {items.filter((j) => j.role === process).length}
                </Badge>
              </Tabs.Tab>
            ))}
          </Tabs.List>
        </Tabs>
        <Group className="task-list-controls" my="md">
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
          <div className="task-list">
            {visible.map((j) => (
              <TaskCard key={j.id} job={j} open={() => setSelected(j)} />
            ))}
            {!visible.length && <p className="muted">{t('tasksAdmin.empty')}</p>}
          </div>
        </State>
      </section>
      <Modal
        opened={!!selected}
        onClose={() => setSelected(undefined)}
        title={t('jobHistory.title')}
        size="xl"
        scrollAreaComponent={ScrollArea.Autosize}
      >
        {selected && (
          <JobHistory key={selected.id} job={items.find((j) => j.id === selected.id) ?? selected} />
        )}
      </Modal>
    </Stack>
  );
}

function TaskCard({ job, open }: { job: DTO<'JobDTO'>; open: () => void }) {
  const { t, i18n } = useTranslation();
  const target = job.resourceName || job.resourceId;
  const targetNode = job.itemId ? (
    <Link to={`/item/${job.itemId}`}>
      {target}
      <ArrowUpRight size={15} />
    </Link>
  ) : job.libraryId ? (
    <Link to={`/library/${job.libraryId}`}>
      {target}
      <ArrowUpRight size={15} />
    </Link>
  ) : (
    <strong>{target}</strong>
  );
  let error = job.error;
  if (error === 'Job failed; inspect worker logs' || error === 'job_execution_failed')
    error = t('jobHistory.failed');
  else if (error === 'job_history_unavailable') error = t('jobHistory.unavailable');
  else if (error === 'Metadata match is ambiguous or missing; identify this item manually')
    error = t('metadata.ambiguous');
  else if (error === 'TMDB_TOKEN not configured') error = t('metadata.providerUnavailable');
  else if (['youtube_preview', 'youtube_download', 'audio_tags'].includes(job.kind) && error)
    error = t(audioErrorKey(error));
  else if (['subtitle_provider_check', 'subtitle_auto'].includes(job.kind) && error)
    error = t(`autoSub.error.${error}`, { defaultValue: error });
  else if (job.kind === 'subtitle_sync' && error)
    error = t(`subtitleEditor.reason.${error}`, { defaultValue: error });
  if (job.error && job.errorDetail) error = job.errorDetail;
  return (
    <article className="task-card">
      <div className="task-main">
        <div>
          <span className="task-kind">
            {t(`native.job.${job.kind}`, { defaultValue: job.kind })}
          </span>
          {targetNode}
          <small>
            {new Date(job.updatedAt).toLocaleString(i18n.language)} ·{' '}
            {t('jobHistory.attemptNumber', { count: job.attempts })}
          </small>
        </div>
        <Badge
          color={
            job.state === 'failed'
              ? 'red'
              : job.state === 'running'
                ? 'blue'
                : job.state === 'completed'
                  ? 'green'
                  : 'gray'
          }
        >
          {t(`native.state.${job.state}`)}
        </Badge>
      </div>
      {job.kind === 'scan' && (
        <div className="task-progress">
          <span>
            {job.processedFiles}/{job.totalFiles}
          </span>
          <Progress value={job.progress} animated={job.state === 'running'} />
        </div>
      )}
      {error && (
        <Alert color="red">
          {job.errorStage && (
            <Text size="xs" mb={4}>
              {t('jobHistory.stage')}: {job.errorStage}
            </Text>
          )}
          {error}
        </Alert>
      )}
      {job.cancelRequested && job.state === 'running' && (
        <Alert color="orange">{t('tasksAdmin.cancelling')}</Alert>
      )}
      <Group mt="sm" justify="space-between">
        <Button
          size="compact-sm"
          variant="light"
          leftSection={<FileText size={15} />}
          onClick={open}
        >
          {t('jobHistory.view')}
        </Button>
        {['pending', 'running'].includes(job.state) && (
          <Button
            size="compact-sm"
            variant="subtle"
            disabled={job.cancelRequested}
            onClick={() =>
              void mutate(async () =>
                result(
                  await api.POST('/api/v1/admin/jobs/{id}/cancel', {
                    params: { path: { id: job.id } },
                  }),
                ),
              )
            }
          >
            {t('cancel')}
          </Button>
        )}
      </Group>
    </article>
  );
}

function JobHistory({ job }: { job: DTO<'JobDTO'> }) {
  const { t, i18n } = useTranslation();
  const [runId, setRunId] = useState<string>();
  const [pages, setPages] = useState<string[]>([]);
  const [level, setLevel] = useState('all');
  const cursor = pages.at(-1);
  const history = useResource(
    ['job-history', job.id, runId, cursor],
    async (signal) =>
      result(
        await api.GET('/api/v1/admin/jobs/{id}/history', {
          params: { path: { id: job.id }, query: { runId, cursor } },
          signal,
        }),
      ),
    true,
    3000,
  );
  const data = history.data;
  const run = data?.runs.find((r) => r.id === data.runId);
  const date = (value: string) => new Date(value).toLocaleString(i18n.language);
  return (
    <Stack className="job-history">
      <Text fw={600}>{job.resourceName || job.resourceId}</Text>
      <Text size="xs" c="dimmed" style={{ overflowWrap: 'anywhere' }}>
        {job.id}
      </Text>
      <Text size="sm" c="dimmed">
        {t('jobHistory.liveHelp')}
      </Text>
      <State loading={history.isLoading} error={history.error}>
        {!!data?.runs.length && (
          <Select
            label={t('jobHistory.execution')}
            value={data.runId}
            allowDeselect={false}
            onChange={(value) => {
              setRunId(value ?? undefined);
              setPages([]);
            }}
            data={data.runs.map((r) => ({
              value: r.id,
              label: `${t('jobHistory.attemptNumber', { count: r.attempt })} · ${date(r.startedAt)} · ${t(`jobHistory.runState.${r.state}`, { defaultValue: r.state })}`,
            }))}
          />
        )}
        {run && (
          <>
            <Group justify="space-between">
              <Badge
                color={run.state === 'failed' ? 'red' : run.state === 'running' ? 'blue' : 'gray'}
              >
                {t(`jobHistory.runState.${run.state}`, { defaultValue: run.state })}
              </Badge>
              <Text size="xs" c="dimmed">
                {t('jobHistory.started')}: {date(run.startedAt)}
                {run.finishedAt ? ` · ${t('jobHistory.finished')}: ${date(run.finishedAt)}` : ''}
              </Text>
            </Group>
            {run.droppedLogs > 0 && (
              <Alert color="orange">{t('jobHistory.truncated', { count: run.droppedLogs })}</Alert>
            )}
          </>
        )}
        {!data?.runs.length && (
          <Alert color="gray">
            {t(
              job.attempts
                ? 'jobHistory.legacy'
                : job.state === 'cancelled'
                  ? 'jobHistory.notExecuted'
                  : 'jobHistory.waiting',
            )}
          </Alert>
        )}
        {!!data?.runs.length && (
          <>
            <Group justify="space-between">
              <Text size="xs" c="dimmed">
                {t('jobHistory.newestFirst')}
              </Text>
              <Select
                size="xs"
                aria-label={t('jobHistory.level')}
                value={level}
                allowDeselect={false}
                onChange={(value) => setLevel(value ?? 'all')}
                data={['all', 'error', 'warn', 'info', 'debug'].map((value) => ({
                  value,
                  label: t(`jobHistory.level.${value}`),
                }))}
              />
            </Group>
            <div className="job-log-list" role="log" aria-label={t('jobHistory.title')}>
              {data.items
                .filter((entry) => level === 'all' || entry.level === level)
                .map((entry) => (
                  <article className={`job-log-entry log-${entry.level}`} key={entry.id}>
                    <Group gap="xs">
                      <Badge
                        size="xs"
                        variant="light"
                        color={
                          entry.level === 'error'
                            ? 'red'
                            : entry.level === 'warn'
                              ? 'orange'
                              : 'gray'
                        }
                      >
                        {t(`jobHistory.level.${entry.level}`, { defaultValue: entry.level })}
                      </Badge>
                      <time dateTime={entry.at}>{date(entry.at)}</time>
                      {entry.stage && <code>{entry.stage}</code>}
                    </Group>
                    <Text size="sm" fw={500}>
                      {t(`jobHistory.event.${entry.message}`, { defaultValue: entry.message })}
                    </Text>
                    {entry.filename && (
                      <Text size="xs" c="dimmed">
                        {entry.filename}
                      </Text>
                    )}
                    {entry.error && (
                      <Text size="sm" c="red">
                        {entry.error}
                      </Text>
                    )}
                    {!!Object.keys(entry.details).length && (
                      <details>
                        <summary>{t('jobHistory.details')}</summary>
                        <dl>
                          {Object.entries(entry.details).map(([key, value]) => (
                            <div key={key}>
                              <dt>{key}</dt>
                              <dd>{value}</dd>
                            </div>
                          ))}
                        </dl>
                      </details>
                    )}
                  </article>
                ))}
              {!data.items.filter((entry) => level === 'all' || entry.level === level).length && (
                <Text size="sm" c="dimmed" p="md">
                  {t('jobHistory.noLogs')}
                </Text>
              )}
            </div>
            <Group justify="space-between">
              <Button
                variant="default"
                size="xs"
                disabled={!pages.length}
                onClick={() => setPages([])}
              >
                {t('jobHistory.latest')}
              </Button>
              {pages.length > 0 && (
                <Button
                  variant="subtle"
                  size="xs"
                  onClick={() => setPages((old) => old.slice(0, -1))}
                >
                  {t('jobHistory.newer')}
                </Button>
              )}
              <Button
                variant="default"
                size="xs"
                disabled={!data.nextCursor}
                onClick={() => {
                  setRunId(data.runId);
                  setPages((old) => [...old, data.nextCursor]);
                }}
              >
                {t('jobHistory.older')}
              </Button>
            </Group>
          </>
        )}
      </State>
    </Stack>
  );
}
