import { useState } from 'react';
import {
  Button,
  FileButton,
  Menu,
  Modal,
  Slider,
  SegmentedControl,
  Popover,
  ScrollArea,
  Stack,
  Text,
} from '@mantine/core';
import { Clock3, Upload, Trash2 } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import toast from 'react-hot-toast';
import { api, result, useResource } from './api';

export function SubtitleUpload({
  fileId,
  onUploaded,
}: {
  fileId: string;
  onUploaded: (id: string) => Promise<void>;
}) {
  const { t } = useTranslation();
  const [busy, setBusy] = useState(false);
  return (
    <FileButton
      accept=".srt,.vtt"
      onChange={async (file) => {
        if (!file) return;
        if (file.size > 512 * 1024) {
          toast.error(t('playerSub.invalid'));
          return;
        }
        setBusy(true);
        try {
          const content = new TextDecoder('utf-8', { fatal: true }).decode(
            await file.arrayBuffer(),
          );
          const response = await api.POST('/api/v1/files/{id}/subtitles', {
            params: { path: { id: fileId } },
            body: { name: file.name, content },
          });
          if (response.response.status === 422 || response.response.status === 413)
            throw new Error('invalid');
          const subtitle = result(response);
          await onUploaded(subtitle.id);
          toast.success(t('playerSub.saved'));
        } catch (error) {
          toast.error(
            t(
              error instanceof TypeError || (error instanceof Error && error.message === 'invalid')
                ? 'playerSub.invalid'
                : 'error',
            ),
          );
        } finally {
          setBusy(false);
        }
      }}
    >
      {(props) => (
        <Menu.Item
          {...props}
          disabled={busy}
          closeMenuOnClick={false}
          leftSection={<Upload size={15} />}
        >
          {t('playerSub.upload')}
        </Menu.Item>
      )}
    </FileButton>
  );
}

export function SubtitleTiming({
  value,
  onChange,
  onChangeEnd,
  active,
  burned,
  portalTarget,
}: {
  value: number;
  onChange: (v: number) => void;
  onChangeEnd: (v: number) => void;
  active: boolean;
  burned: boolean;
  portalTarget?: HTMLElement;
}) {
  const { t } = useTranslation();
  const [opened, setOpened] = useState(false);
  const [range, setRange] = useState(10);
  const limit = Math.max(range, Math.ceil(Math.abs(value)));
  return (
    <Popover
      opened={active && opened}
      onChange={setOpened}
      position="top-start"
      width={260}
      portalProps={{ target: portalTarget }}
      shadow="lg"
      trapFocus
    >
      <Popover.Target>
        <button
          type="button"
          className="playback-option"
          disabled={!active}
          aria-label={t('playerSub.timing')}
          onClick={() => setOpened((v) => !v)}
        >
          <Clock3 size={14} aria-hidden="true" />
          <span>{t('playerSub.timing')}</span>
          {active && value !== 0 && (
            <span>
              {value > 0 ? '+' : ''}
              {value}s
            </span>
          )}
        </button>
      </Popover.Target>
      <Popover.Dropdown className="subtitle-timing-popover">
        <ScrollArea.Autosize mah="min(280px, 50dvh)" scrollbars="y" type="auto">
          <Stack gap={4} className="subtitle-timing">
            <Text size="xs">
              {t('playerSub.delay')}: {value > 0 ? '+' : ''}
              {value.toFixed(1)}s
            </Text>
            <SegmentedControl
              size="xs"
              value={String(range)}
              onChange={(v) => setRange(Number(v))}
              data={[10, 60, 600].map((v) => ({ value: String(v), label: `±${v}s` }))}
              aria-label={t('playerSub.timingRange')}
            />
            <Slider
              thumbLabel={t('playerSub.delay')}
              value={value}
              min={-limit}
              max={limit}
              step={0.1}
              label={(v) => `${v > 0 ? '+' : ''}${v.toFixed(1)}s`}
              onChange={onChange}
              onChangeEnd={onChangeEnd}
              marks={[{ value: 0, label: '0' }]}
              mb="sm"
            />
            <Text size="xs" c="dimmed">
              {t('playerSub.delayHelp')} {t('playerSub.personalTiming')}
            </Text>
            <Button
              size="compact-xs"
              variant="subtle"
              onClick={() => {
                onChange(0);
                onChangeEnd(0);
              }}
            >
              {t('playerSub.reset')}
            </Button>
            {burned && (
              <Text size="xs" c="dimmed">
                {t('playerSub.burnTiming')}
              </Text>
            )}
          </Stack>
        </ScrollArea.Autosize>
      </Popover.Dropdown>
    </Popover>
  );
}

export function SubtitleOverlay({
  fileId,
  subtitleId,
  time,
  delay,
}: {
  fileId: string;
  subtitleId: string;
  time: number;
  delay: number;
}) {
  const { t } = useTranslation();
  const data = useResource(['subtitle', fileId, subtitleId], async (signal) =>
    result(
      await api.GET('/api/v1/files/{id}/subtitles/{subtitleId}', {
        params: { path: { id: fileId, subtitleId } },
        signal,
      }),
    ),
  );
  if (data.error) return <div className="player-subtitle-error">{t('playerSub.loadError')}</div>;
  const text = data.data?.cues
    .filter((cue) => cue.start <= time - delay && time - delay < cue.end)
    .map((cue) => cue.text)
    .join('\n');
  return text ? (
    <div className="player-subtitle-overlay" aria-live="off">
      <span>{text}</span>
    </div>
  ) : null;
}

export function UploadedSubtitleDelete({
  fileId,
  subtitleId,
  onDeleted,
}: {
  fileId: string;
  subtitleId: string;
  onDeleted: () => Promise<void>;
}) {
  const { t } = useTranslation();
  const [opened, setOpened] = useState(false);
  const [busy, setBusy] = useState(false);
  return (
    <>
      <Menu.Item color="red" leftSection={<Trash2 size={15} />} onClick={() => setOpened(true)}>
        {t('subtitleEditor.deleteUpload')}
      </Menu.Item>
      <Modal
        opened={opened}
        onClose={() => {
          if (!busy) setOpened(false);
        }}
        title={t('confirm')}
        scrollAreaComponent={ScrollArea.Autosize}
      >
        <Stack>
          <Text>{t('subtitleEditor.confirmDeleteUpload')}</Text>
          <Button
            color="red"
            loading={busy}
            onClick={async () => {
              setBusy(true);
              try {
                result(
                  await api.DELETE('/api/v1/files/{id}/subtitles/{subtitleId}', {
                    params: { path: { id: fileId, subtitleId } },
                  }),
                );
                await onDeleted();
                setOpened(false);
                toast.success(t('subtitleEditor.deleted'));
              } catch {
                toast.error(t('error'));
              } finally {
                setBusy(false);
              }
            }}
          >
            {t('subtitleEditor.deleteUpload')}
          </Button>
        </Stack>
      </Modal>
    </>
  );
}
