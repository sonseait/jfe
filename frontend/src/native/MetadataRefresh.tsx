import { useState } from 'react';
import { Alert, Button, Group, Modal, Radio, Stack } from '@mantine/core';
import { RefreshCw } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { api, result, type DTO } from './api';
import { mutate } from './shared';

export default function MetadataRefresh({ item }: { item: DTO<'ItemDTO'> }) {
  const { t } = useTranslation();
  const [opened, setOpened] = useState(false);
  const [mode, setMode] = useState<'replace' | 'missing'>('missing');
  const [busy, setBusy] = useState(false);
  const refresh = async () => {
    setBusy(true);
    const ok = await mutate(async () =>
      result(
        await api.POST('/api/v1/items/{id}/metadata/refresh', {
          params: { path: { id: item.id } },
          body: { mode },
        }),
      ),
    );
    setBusy(false);
    if (ok) setOpened(false);
  };
  return (
    <>
      <Button
        variant="default"
        leftSection={<RefreshCw size={18} />}
        onClick={() => {
          setMode(item.providerId ? 'missing' : 'replace');
          setOpened(true);
        }}
      >
        {t('metadata.refreshSeries')}
      </Button>
      <Modal
        opened={opened}
        onClose={() => {
          if (!busy) setOpened(false);
        }}
        title={t('metadata.refreshSeries')}
      >
        <Stack>
          <strong>{item.title}</strong>
          <Radio.Group
            label={t('metadata.refreshMode')}
            value={mode}
            onChange={(value) => setMode(value as 'replace' | 'missing')}
          >
            <Stack gap="xs" mt="xs">
              <Radio value="replace" label={t('metadata.replaceAll')} disabled={busy} />
              <Radio
                value="missing"
                label={t('metadata.missingOnly')}
                disabled={busy || !item.providerId}
              />
            </Stack>
          </Radio.Group>
          <p>{t(item.providerId ? 'metadata.missingHelp' : 'metadata.identifyFirst')}</p>
          {mode === 'replace' && <Alert color="orange">{t('metadata.replaceConfirm')}</Alert>}
          <Group justify="flex-end">
            <Button variant="default" disabled={busy} onClick={() => setOpened(false)}>
              {t('cancel')}
            </Button>
            <Button
              color={mode === 'replace' ? 'orange' : undefined}
              loading={busy}
              disabled={mode === 'missing' && !item.providerId}
              onClick={() => void refresh()}
            >
              {t(mode === 'replace' ? 'metadata.replaceAll' : 'metadata.refreshSeries')}
            </Button>
          </Group>
        </Stack>
      </Modal>
    </>
  );
}
