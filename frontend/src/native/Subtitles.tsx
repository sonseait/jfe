import { useState } from 'react';
import {
  Button,
  FileButton,
  Menu,
  NumberInput,
  Popover,
  ScrollArea,
  Stack,
  Text,
} from '@mantine/core';
import { Clock3, Upload } from 'lucide-react';
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
  active,
  burned,
  portalTarget,
}: {
  value: number;
  onChange: (v: number) => void;
  active: boolean;
  burned: boolean;
  portalTarget?: HTMLElement;
}) {
  const { t } = useTranslation();
  const [opened, setOpened] = useState(false);
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
            <NumberInput
              size="xs"
              label={t('playerSub.delay')}
              value={value}
              min={-600}
              max={600}
              step={0.1}
              decimalScale={1}
              onChange={(v) => onChange(Number(v))}
            />
            <Text size="xs" c="dimmed">
              {t('playerSub.delayHelp')}
            </Text>
            <Button size="compact-xs" variant="subtle" onClick={() => onChange(0)}>
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
