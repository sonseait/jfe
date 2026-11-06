import { useCallback, useEffect, useRef, useState } from 'react';
import {
  Alert,
  Badge,
  Button,
  Group,
  Modal,
  NumberInput,
  ScrollArea,
  Select,
  Slider,
  Stack,
  Text,
} from '@mantine/core';
import { modals } from '@mantine/modals';
import { Clock3, RotateCcw, Trash2 } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import toast from 'react-hot-toast';
import { queryClient } from '../lib/query-client';
import { api, result, useResource, type DTO } from './api';
import { useNativePlayer } from './Player';
import { SubtitlePreview } from './SubtitlePreview';
import { minimumSubtitleOffset, subtitleClock } from './subtitle-sync';

export function SubtitleManagerButton({
  files,
  initialFileId,
}: {
  files: DTO<'FileDTO'>[];
  initialFileId?: string;
}) {
  const { t } = useTranslation();
  const [opened, setOpened] = useState(false);
  const [fileID, setFileID] = useState(initialFileId ?? files.find((f) => f.available)?.id ?? '');
  const file = files.find((f) => f.id === fileID);
  const sources = useResource(
    ['subtitleSources', fileID],
    async (signal) =>
      result(
        await api.GET('/api/v1/files/{id}/subtitle-sync', {
          params: { path: { id: fileID } },
          signal,
        }),
      ),
    Boolean(fileID),
  );
  if (!file || sources.data?.reason === 'permission') return null;
  return (
    <>
      <Button
        variant="default"
        leftSection={<Clock3 size={16} />}
        loading={sources.isLoading}
        disabled={!sources.data?.canEdit}
        title={sources.data?.reason ? t(`subtitleEditor.reason.${sources.data.reason}`) : undefined}
        onClick={() => {
          useNativePlayer.getState().stop();
          setOpened(true);
        }}
      >
        {t('subtitleEditor.title')}
      </Button>
      {opened && (
        <SubtitleEditor
          file={file}
          files={files}
          onFileChange={setFileID}
          close={() => setOpened(false)}
        />
      )}
    </>
  );
}

export function SubtitleEditor({
  file,
  files,
  onFileChange,
  close,
}: {
  file: DTO<'FileDTO'>;
  files: DTO<'FileDTO'>[];
  onFileChange: (id: string) => void;
  close: () => void;
}) {
  const { t } = useTranslation();
  const sources = useResource(['subtitleSources', file.id], async (signal) =>
    result(
      await api.GET('/api/v1/files/{id}/subtitle-sync', {
        params: { path: { id: file.id } },
        signal,
      }),
    ),
  );
  const [selected, setSelected] = useState<string | null>(null);
  const [offsetMS, setOffsetMS] = useState(0);
  const [jobID, setJobID] = useState('');
  const [action, setAction] = useState<'prepare' | 'save' | 'delete'>('prepare');
  const [requestBusy, setRequestBusy] = useState(false);
  const [document, setDocument] = useState<DTO<'SubtitleSyncJobDTO'>>();
  const preparationID = useRef('');
  const completed = useRef('');
  const confirmationID = useRef<string | undefined>(undefined);
  useEffect(
    () => () => {
      if (confirmationID.current) modals.close(confirmationID.current);
    },
    [],
  );
  const pendingSelection = useRef<{ kind: 'file' | 'track'; value: string | null } | undefined>(
    undefined,
  );
  const stopPreview = useRef<(() => Promise<void>) | undefined>(undefined);
  const onStopReady = useCallback((stop: (() => Promise<void>) | undefined) => {
    stopPreview.current = stop;
  }, []);
  const [time, setTime] = useState(0);
  const [seek, setSeek] = useState({ time: 0, generation: 0 });
  const [scrollY, setScrollY] = useState(0);
  const viewport = useRef<HTMLDivElement>(null);
  const job = useResource(
    ['subtitleSyncJob', file.id, jobID],
    async (signal) =>
      result(
        await api.GET('/api/v1/files/{id}/subtitle-sync/jobs/{jobId}', {
          params: { path: { id: file.id, jobId: jobID } },
          signal,
        }),
      ),
    Boolean(jobID),
    1000,
  );
  const busy =
    requestBusy ||
    Boolean(jobID && (!job.data || ['pending', 'running'].includes(job.data.job.state)));
  const mutationBusy = busy && action !== 'prepare';
  const source = sources.data?.items.find((s) => String(s.index) === selected);
  const cues = document?.cues ?? [];
  const invalidOffset = Boolean(cues.length && offsetMS < minimumSubtitleOffset(cues));
  const changed = offsetMS !== 0;
  const prepare = async (index: string) => {
    setRequestBusy(true);
    setDocument(undefined);
    setJobID('');
    setOffsetMS(0);
    setAction('prepare');
    try {
      const value = result(
        await api.POST('/api/v1/files/{id}/subtitle-sync/prepare', {
          params: { path: { id: file.id } },
          body: { trackIndex: Number(index), revision: sources.data?.revision ?? '' },
        }),
      );
      preparationID.current = value.id;
      setJobID(value.id);
    } catch {
      toast.error(t('error'));
    } finally {
      setRequestBusy(false);
    }
  };
  useEffect(() => {
    if (
      !job.data ||
      completed.current === job.data.job.id ||
      ['pending', 'running'].includes(job.data.job.state)
    )
      return;
    completed.current = job.data.job.id;
    if (job.data.job.state !== 'completed') {
      toast.error(t(`subtitleEditor.reason.${job.data.errorCode || 'subtitle_job_failed'}`));
      return;
    }
    if (action === 'prepare') {
      setDocument(job.data);
      return;
    }
    toast.success(t(action === 'delete' ? 'subtitleEditor.deleted' : 'saved'));
    setDocument(undefined);
    setOffsetMS(0);
    setSelected(null);
    void queryClient.invalidateQueries({ queryKey: ['native'] });
  }, [job.data, action, t]);
  const selectTrack = (value: string | null) => {
    setSelected(value);
    setDocument(undefined);
    setOffsetMS(0);
    setScrollY(0);
    viewport.current?.scrollTo({ top: 0 });
    if (value !== null) void prepare(value);
  };
  const switchSelection = (kind: 'file' | 'track', value: string | null) => {
    if (changed) {
      pendingSelection.current = { kind, value };
      confirmAction('switch');
      return;
    }
    if (kind === 'track') selectTrack(value);
    else if (value) {
      setSelected(null);
      setDocument(undefined);
      setJobID('');
      onFileChange(value);
    }
  };
  const mutate = async (kind: 'save' | 'delete') => {
    if (!document || !source) return;
    setRequestBusy(true);
    setAction(kind);
    try {
      await stopPreview.current?.();
      const body = {
        preparationId: preparationID.current,
        fingerprint: document.fingerprint,
        offsetMs: kind === 'save' ? offsetMS : 0,
      };
      const send = () =>
        kind === 'save'
          ? api.POST('/api/v1/files/{id}/subtitle-sync/save', {
              params: { path: { id: file.id } },
              body,
            })
          : api.POST('/api/v1/files/{id}/subtitle-sync/delete', {
              params: { path: { id: file.id } },
              body,
            });
      let response = await send();
      // A stopped preview's worker may need a moment to release FFmpeg resources.
      for (
        let attempt = 0;
        attempt < 4 && response.error?.detail === 'subtitle_playback_busy';
        attempt++
      ) {
        await new Promise<void>((resolve) => setTimeout(resolve, 500));
        response = await send();
      }
      if (response.error) throw new Error(response.error.detail);
      setJobID(result(response).id);
    } catch (e) {
      toast.error(
        t(
          `subtitleEditor.reason.${e instanceof Error && ['subtitle_playback_busy', 'subtitle_busy', 'subtitle_changed'].includes(e.message) ? e.message : 'subtitle_job_failed'}`,
        ),
      );
    } finally {
      setRequestBusy(false);
    }
  };
  const confirmAction = (kind: 'save' | 'delete' | 'close' | 'switch') => {
    confirmationID.current = modals.openConfirmModal({
      title: t('confirm'),
      centered: true,
      scrollAreaComponent: ScrollArea.Autosize,
      children: (
        <Text size="sm">
          {t(
            kind === 'save'
              ? 'subtitleEditor.confirmSave'
              : kind === 'delete'
                ? 'subtitleEditor.confirmDelete'
                : 'subtitleEditor.confirmDiscard',
            { file: file.name, track: source?.name, offset: offsetMS / 1000 },
          )}
        </Text>
      ),
      labels: {
        cancel: t('cancel'),
        confirm: t(
          kind === 'save'
            ? 'subtitleEditor.save'
            : kind === 'delete'
              ? 'subtitleEditor.delete'
              : 'subtitleEditor.discard',
        ),
      },
      confirmProps: { color: kind === 'delete' ? 'red' : undefined },
      onClose: () => {
        confirmationID.current = undefined;
      },
      onConfirm: () => {
        if (kind === 'save' || kind === 'delete') void mutate(kind);
        else if (kind === 'close') close();
        else {
          const next = pendingSelection.current;
          setOffsetMS(0);
          if (next?.kind === 'track') selectTrack(next.value);
          else if (next?.value) {
            setSelected(null);
            setDocument(undefined);
            setJobID('');
            onFileChange(next.value);
          }
        }
      },
    });
  };
  const first = Math.max(0, Math.floor(scrollY / 80) - 2);
  const activeIndex = cues.findIndex(
    (c) => c.start <= time - offsetMS / 1000 && time - offsetMS / 1000 < c.end,
  );
  return (
    <Modal
      opened
      onClose={() => {
        if (!mutationBusy) {
          if (changed) confirmAction('close');
          else close();
        }
      }}
      title={t('subtitleEditor.title')}
      size="min(1440px, calc(100vw - 32px))"
      scrollAreaComponent={ScrollArea.Autosize}
      closeOnClickOutside={!mutationBusy}
      closeOnEscape={!mutationBusy}
      withCloseButton={!mutationBusy}
    >
      <Stack>
        <div className="subtitle-editor-selectors">
          <Select
            label={t('subtitleEditor.file')}
            value={file.id}
            disabled={busy}
            data={files.filter((f) => f.available).map((f) => ({ value: f.id, label: f.name }))}
            onChange={(value) => switchSelection('file', value)}
          />
          <Select
            label={t('subtitles')}
            value={selected}
            disabled={busy || !sources.data?.canEdit}
            data={
              sources.data?.items.map((s) => ({
                value: String(s.index),
                label: `${s.name} · ${t(`subtitleEditor.${s.kind}`)}${!s.canDelete && s.reason ? ` · ${t(`subtitleEditor.reason.${s.reason}`)}` : ''}`,
                disabled: !s.canDelete,
              })) ?? []
            }
            onChange={(value) => switchSelection('track', value)}
          />
        </div>
        {sources.error && <Alert color="red">{t('error')}</Alert>}
        {!sources.isLoading && !sources.data?.items.length && (
          <Text c="dimmed">{t('subtitleEditor.empty')}</Text>
        )}
        {source?.reason && <Alert>{t(`subtitleEditor.reason.${source.reason}`)}</Alert>}
        {document?.errorCode && <Alert>{t(`subtitleEditor.reason.${document.errorCode}`)}</Alert>}
        {busy && (
          <Text role="status">
            {t(action === 'prepare' ? 'subtitleEditor.preparing' : 'subtitleEditor.saving')}
          </Text>
        )}
        {job.error && <Alert color="red">{t('error')}</Alert>}
        {job.data && ['failed', 'cancelled'].includes(job.data.job.state) && (
          <Alert color="red">
            {t(`subtitleEditor.reason.${job.data.errorCode || 'subtitle_job_failed'}`)}
          </Alert>
        )}
        <div className="subtitle-editor-workspace">
          <Stack className="subtitle-editor-player-column">
            {document && !mutationBusy && (
              <SubtitlePreview
                file={file}
                cues={cues}
                offsetMS={offsetMS}
                seek={seek}
                onTime={setTime}
                onStopReady={onStopReady}
              />
            )}
            {source?.canSync && (!document || cues.length > 0) && (
              <>
                <Text size="sm">{t('playerSub.delayHelp')}</Text>
                <Slider
                  aria-label={t('playerSub.delay')}
                  min={-60}
                  max={60}
                  step={0.1}
                  disabled={!document || busy}
                  value={Math.max(-60, Math.min(60, offsetMS / 1000))}
                  label={(v) => `${v > 0 ? '+' : ''}${v}s`}
                  onChange={(v) => setOffsetMS(Math.round(v * 1000))}
                />
                <Group align="end">
                  <NumberInput
                    label={t('playerSub.delay')}
                    value={offsetMS / 1000}
                    min={-600}
                    max={600}
                    step={0.01}
                    decimalScale={2}
                    disabled={!document || busy}
                    onChange={(v) => setOffsetMS(Math.round(Number(v) * 1000))}
                  />
                  <Button
                    variant="subtle"
                    leftSection={<RotateCcw size={15} />}
                    disabled={busy}
                    onClick={() => setOffsetMS(0)}
                  >
                    {t('playerSub.reset')}
                  </Button>
                </Group>
                {invalidOffset && (
                  <Alert color="red">
                    {t('subtitleEditor.negative', { minimum: minimumSubtitleOffset(cues) / 1000 })}
                  </Alert>
                )}
                {['ass', 'ssa'].includes(source.codec) && (
                  <Text c="dimmed" size="xs">
                    {t('subtitleEditor.assPreview')}
                  </Text>
                )}
              </>
            )}
          </Stack>
          <Stack className="subtitle-editor-cue-column">
            {Boolean(cues.length) && (
              <>
                <Group justify="space-between">
                  <Text size="sm">{t('subtitleEditor.cues', { count: cues.length })}</Text>
                  <Button
                    variant="subtle"
                    size="compact-xs"
                    disabled={activeIndex < 0}
                    onClick={() => viewport.current?.scrollTo({ top: activeIndex * 80 })}
                  >
                    {t('subtitleEditor.current')}
                  </Button>
                </Group>
                <ScrollArea
                  h="min(480px, 58dvh)"
                  className="subtitle-editor-cue-list"
                  style={{ flexShrink: 0 }}
                  viewportRef={viewport}
                  onScrollPositionChange={({ y }) => setScrollY(y)}
                >
                  <div style={{ height: cues.length * 80, position: 'relative' }}>
                    {cues.slice(first, first + 12).map((cue, i) => (
                      <button
                        type="button"
                        key={first + i}
                        className={`subtitle-cue ${activeIndex === first + i ? 'active' : ''}`}
                        style={{
                          position: 'absolute',
                          top: (first + i) * 80,
                          height: 80,
                          width: '100%',
                        }}
                        onClick={() =>
                          setSeek((s) => ({
                            time: Math.max(0, cue.start + offsetMS / 1000 - 1),
                            generation: s.generation + 1,
                          }))
                        }
                      >
                        <span className="subtitle-cue-time">
                          {subtitleClock(cue.start)} → {subtitleClock(cue.end)}
                        </span>
                        {offsetMS !== 0 && (
                          <span className="subtitle-cue-time shifted">
                            {subtitleClock(cue.start + offsetMS / 1000)} →{' '}
                            {subtitleClock(cue.end + offsetMS / 1000)}
                          </span>
                        )}
                        <span className="subtitle-cue-text">{cue.text}</span>
                      </button>
                    ))}
                  </div>
                </ScrollArea>
              </>
            )}
          </Stack>
        </div>
        <Group justify="space-between">
          <Badge variant="light">{t('subtitleEditor.shared')}</Badge>
          <Group>
            {selected && (
              <Button variant="subtle" disabled={busy} onClick={() => void prepare(selected)}>
                {t('retry')}
              </Button>
            )}
            <Button
              color="red"
              variant="light"
              leftSection={<Trash2 size={15} />}
              disabled={!document || busy || !source?.canDelete}
              onClick={() => confirmAction('delete')}
            >
              {t('subtitleEditor.delete')}
            </Button>
            <Button
              disabled={
                !document || busy || !changed || invalidOffset || !source?.canSync || !cues.length
              }
              onClick={() => confirmAction('save')}
            >
              {t('subtitleEditor.save')}
            </Button>
          </Group>
        </Group>
      </Stack>
    </Modal>
  );
}
