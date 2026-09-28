import { useState } from 'react';
import {
  Alert,
  Badge,
  Button,
  Checkbox,
  Group,
  NumberInput,
  Select,
  Stack,
  TextInput,
} from '@mantine/core';
import { useTranslation } from 'react-i18next';
import { api, result, useResource, type DTO } from './api';
import { mutate, State } from './shared';

export default function GeneralSettings() {
  const settings = useResource('general-settings', async (signal) =>
    result(await api.GET('/api/v1/admin/settings', { signal })),
  );
  return (
    <State loading={settings.isLoading} error={settings.error}>
      {settings.data && <SettingsForm settings={settings.data} />}
    </State>
  );
}

function SettingsForm({ settings }: { settings: DTO<'GeneralSettingsDTO'> }) {
  const { t } = useTranslation();
  const [draft, setDraft] = useState<DTO<'GeneralSettingsPatch'>>({});
  const [busy, setBusy] = useState(false);
  const values = { ...settings, ...draft };
  const dirty = Object.keys(draft).length > 0;
  function change<K extends keyof DTO<'GeneralSettingsPatch'>>(
    key: K,
    value: DTO<'GeneralSettingsPatch'>[K],
  ) {
    setDraft((old) => {
      const next = { ...old, [key]: value };
      if (value === settings[key]) delete next[key];
      return next;
    });
  }
  return (
    <form
      className="general-settings"
      onSubmit={async (event) => {
        event.preventDefault();
        if (!dirty || busy) return;
        setBusy(true);
        try {
          if (
            await mutate(async () =>
              result(await api.PATCH('/api/v1/admin/settings', { body: draft })),
            )
          )
            setDraft({});
        } finally {
          setBusy(false);
        }
      }}
    >
      <h2>{t('general.title')}</h2>
      <p className="muted">{t('general.liveHelp')}</p>
      <fieldset className="admin-fieldset" disabled={busy}>
        <Stack gap="xl">
          <section>
            <h3>{t('general.identity')}</h3>
            <TextInput
              label={t('general.serverName')}
              value={values.serverName ?? ''}
              required
              maxLength={80}
              onChange={(event) => change('serverName', event.currentTarget.value)}
            />
          </section>
          <section>
            <Group justify="space-between">
              <h3>{t('general.metadata')}</h3>
              <Badge color={settings.tmdbConfigured ? 'green' : 'gray'} variant="light">
                {t(settings.tmdbConfigured ? 'general.configured' : 'general.notConfigured')}
              </Badge>
            </Group>
            <Stack gap="md">
              {!settings.tmdbConfigured && <Alert color="gray">{t('general.tmdbHelp')}</Alert>}
              <Checkbox
                label={t('general.autoMetadata')}
                description={t('general.autoHelp')}
                disabled={!settings.tmdbConfigured}
                checked={values.autoMetadata ?? false}
                onChange={(event) => change('autoMetadata', event.currentTarget.checked)}
              />
              <Select
                label={t('general.metadataLanguage')}
                description={t('general.languageHelp')}
                disabled={!settings.tmdbConfigured}
                value={values.metadataLanguage}
                allowDeselect={false}
                data={[
                  { value: 'en-US', label: 'English' },
                  { value: 'vi-VN', label: 'Tiếng Việt' },
                ]}
                onChange={(value) => {
                  if (value === 'en-US' || value === 'vi-VN') change('metadataLanguage', value);
                }}
              />
              <Checkbox
                label={t('general.castImages')}
                description={t('general.imagesHelp')}
                disabled={!settings.tmdbConfigured}
                checked={values.castImages ?? false}
                onChange={(event) => change('castImages', event.currentTarget.checked)}
              />
            </Stack>
          </section>
          <section>
            <h3>{t('general.playback')}</h3>
            <NumberInput
              label={t('general.watchedPercent')}
              description={t('general.watchedHelp')}
              value={values.watchedPercent ?? 95}
              min={50}
              max={100}
              allowDecimal={false}
              allowNegative={false}
              required
              suffix="%"
              onChange={(value) => {
                if (typeof value === 'number') change('watchedPercent', value);
              }}
            />
          </section>
          <Group>
            <Button type="submit" disabled={!dirty || !values.serverName?.trim()} loading={busy}>
              {t('save')}
            </Button>
            <Button variant="default" disabled={!dirty || busy} onClick={() => setDraft({})}>
              {t('general.discard')}
            </Button>
            {dirty && (
              <span className="muted" role="status">
                {t('general.unsaved')}
              </span>
            )}
          </Group>
        </Stack>
      </fieldset>
    </form>
  );
}
