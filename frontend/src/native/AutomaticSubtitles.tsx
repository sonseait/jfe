import { useState } from 'react';
import {
  Alert,
  Badge,
  Button,
  Checkbox,
  Group,
  Modal,
  ScrollArea,
  Stack,
  Text,
} from '@mantine/core';
import { Captions, RefreshCw } from 'lucide-react';
import { Link } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { api, result, useResource, type DTO } from './api';
import { mutate, State } from './shared';

function useProvider() {
  return useResource(
    'subtitle-provider',
    async (signal) => result(await api.GET('/api/v1/admin/subtitle-provider', { signal })),
    true,
    10000,
  );
}

export function SubtitleProviderSettings() {
  const { t, i18n } = useTranslation();
  const provider = useProvider();
  const [busy, setBusy] = useState(false);
  const [queued, setQueued] = useState(false);
  const p = provider.data;
  const date = (value: string) => new Date(value).toLocaleString(i18n.language);
  return (
    <section className="general-settings">
      <h2>{t('autoSub.title')}</h2>
      <p className="muted">{t('autoSub.description')}</p>
      <State loading={provider.isLoading} error={provider.error}>
        {p && (
          <Stack gap="md">
            <Group>
              <Badge color={p.configured ? 'green' : 'gray'}>
                {t(p.configured ? 'autoSub.configured' : 'autoSub.notConfigured')}
              </Badge>
              <Badge color={p.scannerReady ? 'green' : 'orange'}>
                {t(p.scannerReady ? 'autoSub.scannerReady' : 'autoSub.scannerMissing')}
              </Badge>
              {p.accountConfigured && (
                <Badge variant="outline">{t('autoSub.accountConfigured')}</Badge>
              )}
              <Badge color={p.status === 'connected' ? 'green' : 'orange'}>
                {t(`autoSub.status.${p.status}`, {
                  defaultValue: t('autoSub.status.provider_error'),
                })}
              </Badge>
              {p.accountConfigured && (p.status === 'connected' || p.vip) && (
                <Badge variant="outline">{t(p.vip ? 'autoSub.vip' : 'autoSub.standard')}</Badge>
              )}
            </Group>
            <Text size="sm">
              {t('autoSub.quota')}: {p.remaining >= 0 ? p.remaining : t('autoSub.unknown')}
            </Text>
            {new Date(p.checkedAt).getTime() > 0 && (
              <Text size="sm" c="dimmed">
                {t('autoSub.checked')}: {date(p.checkedAt)}
              </Text>
            )}
            {new Date(p.resetAt).getTime() > 0 && (
              <Text size="sm">
                {t('autoSub.reset')}: {date(p.resetAt)}
              </Text>
            )}
            {new Date(p.retryAt).getTime() > Date.now() && (
              <Alert color="orange">{t('autoSub.retryAt', { date: date(p.retryAt) })}</Alert>
            )}
            {(!p.automaticReady || !p.accountConfigured) && (
              <Alert color="gray">{t('autoSub.setup')}</Alert>
            )}
            {queued && (
              <Text size="sm" role="status">
                {t('autoSub.checkQueued')}
              </Text>
            )}
            <Group>
              <Button
                variant="default"
                leftSection={<RefreshCw size={16} />}
                loading={busy}
                disabled={
                  !p.configured ||
                  !p.scannerReady ||
                  !p.accountConfigured ||
                  new Date(p.retryAt).getTime() > Date.now()
                }
                onClick={async () => {
                  setBusy(true);
                  try {
                    if (
                      await mutate(async () =>
                        result(await api.POST('/api/v1/admin/subtitle-provider/check')),
                      )
                    )
                      setQueued(true);
                  } finally {
                    setBusy(false);
                  }
                }}
              >
                {t('autoSub.check')}
              </Button>
              <Link to="/admin/libraries">{t('manage.librarySettings')}</Link>
            </Group>
            <Text size="sm" c="dimmed">
              {t('autoSub.credentials')}
            </Text>
          </Stack>
        )}
      </State>
    </section>
  );
}

export function LibraryAutomaticSubtitles({ library }: { library: DTO<'LibraryDTO'> }) {
  const { t } = useTranslation();
  const provider = useProvider();
  const [opened, setOpened] = useState(false);
  const [busy, setBusy] = useState(false);
  return (
    <Stack gap="xs" className="library-auto-subtitles">
      <Checkbox
        label={t('autoSub.enable')}
        checked={library.autoSubtitles}
        description={t('autoSub.libraryHelp')}
        disabled={busy || (!library.autoSubtitles && !provider.data?.automaticReady)}
        onChange={async (event) => {
          const enabled = event.currentTarget.checked;
          setBusy(true);
          try {
            await mutate(async () =>
              result(
                await api.PATCH('/api/v1/libraries/{id}/automatic-subtitles', {
                  params: { path: { id: library.id } },
                  body: { enabled },
                }),
              ),
            );
          } finally {
            setBusy(false);
          }
        }}
      />
      <Group gap="sm">
        {library.autoSubtitles && (
          <Text size="sm" c="dimmed">
            {t('autoSub.missingCount', { count: library.missingSubtitles })}
          </Text>
        )}
        <Button
          variant="subtle"
          size="xs"
          leftSection={<Captions size={15} />}
          onClick={() => setOpened(true)}
        >
          {t('autoSub.details')}
        </Button>
        {!provider.data?.automaticReady && (
          <Link to="/admin/general">{t('autoSub.configure')}</Link>
        )}
      </Group>
      <Modal
        opened={opened}
        onClose={() => setOpened(false)}
        title={`${t('autoSub.title')} · ${library.name}`}
        size="xl"
        scrollAreaComponent={ScrollArea.Autosize}
      >
        {opened && <AutomaticSubtitleList library={library} />}
      </Modal>
    </Stack>
  );
}

function AutomaticSubtitleList({ library }: { library: DTO<'LibraryDTO'> }) {
  const { t, i18n } = useTranslation();
  const [cursor, setCursor] = useState('');
  const [busy, setBusy] = useState(false);
  const provider = useProvider();
  const data = useResource(
    ['automatic-subtitles', library.id, cursor],
    async (signal) =>
      result(
        await api.GET('/api/v1/libraries/{id}/automatic-subtitles', {
          params: { path: { id: library.id }, query: { cursor } },
          signal,
        }),
      ),
    true,
    5000,
  );
  return (
    <Stack>
      <Text size="sm" c="dimmed">
        {t('autoSub.libraryHelp')}
      </Text>
      <Group justify="space-between">
        <Text>
          {t('autoSub.missingTotal', {
            missing: data.data?.missing ?? library.missingSubtitles,
            total: data.data?.total ?? library.fileCount,
          })}
        </Text>
        <Button
          loading={busy}
          disabled={!data.data?.enabled || !provider.data?.automaticReady}
          leftSection={<RefreshCw size={16} />}
          onClick={async () => {
            setBusy(true);
            try {
              await mutate(async () =>
                result(
                  await api.POST('/api/v1/libraries/{id}/automatic-subtitles/search', {
                    params: { path: { id: library.id } },
                  }),
                ),
              );
            } finally {
              setBusy(false);
            }
          }}
        >
          {t('autoSub.searchNow')}
        </Button>
      </Group>
      <State loading={data.isLoading} error={data.error}>
        {!data.data?.items.length && <Text c="dimmed">{t('autoSub.empty')}</Text>}
        {data.data?.items.map((row) => (
          <Stack key={row.fileId} gap={4} className="auto-subtitle-row">
            <Group justify="space-between" wrap="nowrap">
              <Link to={`/item/${row.itemId}`}>{row.title}</Link>
              <Badge
                variant="light"
                color={row.state === 'available' ? 'green' : row.state === 'error' ? 'red' : 'gray'}
              >
                {t(`autoSub.state.${row.state}`, { defaultValue: t('autoSub.state.error') })}
              </Badge>
            </Group>
            <Text size="xs" c="dimmed" style={{ overflowWrap: 'anywhere' }}>
              {row.filename}
            </Text>
            {row.errorCode && (
              <Text size="sm" c="orange">
                {t(`autoSub.error.${row.errorCode}`, {
                  defaultValue: t('autoSub.error.subtitle_download_failed'),
                })}
              </Text>
            )}
            {row.state !== 'available' && (
              <Text size="xs" c="dimmed">
                {t('autoSub.retryAt', {
                  date: new Date(row.nextSearchAt).toLocaleString(i18n.language),
                })}
              </Text>
            )}
          </Stack>
        ))}
      </State>
      <Group>
        {cursor && (
          <Button variant="default" onClick={() => setCursor('')}>
            {t('autoSub.firstPage')}
          </Button>
        )}
        {data.data?.nextCursor && (
          <Button variant="default" onClick={() => setCursor(data.data!.nextCursor)}>
            {t('autoSub.nextPage')}
          </Button>
        )}
      </Group>
    </Stack>
  );
}
