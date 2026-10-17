import { useEffect, useRef, useState } from 'react';
import { Alert, Anchor, Badge, Button, Group, Select, Stack, Textarea } from '@mantine/core';
import { ArrowUpRight, Sparkles } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { api, result, useResource, type DTO } from './api';
import { State } from './shared';

export default function Recommendations() {
  const { t, i18n } = useTranslation();
  const status = useResource('recommendations', async (signal) =>
    result(await api.GET('/api/v1/admin/recommendations', { signal })),
  );
  const [focus, setFocus] = useState<DTO<'RecommendationRequest'>['focus']>('mixed');
  const [preferences, setPreferences] = useState('');
  const [generated, setGenerated] = useState<DTO<'RecommendationsDTO'>>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const request = useRef<AbortController | null>(null);
  useEffect(() => () => request.current?.abort(), []);
  const ready = Boolean(status.data?.geminiConfigured && status.data?.tmdbConfigured);
  const data = generated ?? status.data?.last;
  return (
    <Stack gap="lg" className="ai-recommendations">
      <div>
        <h2>{t('ai.title')}</h2>
        <p className="muted">{t('ai.description')}</p>
      </div>
      <State loading={status.isLoading} error={status.error}>
        {status.data && (
          <>
            <Group gap="xs">
              <Badge color={status.data.geminiConfigured ? 'green' : 'gray'}>
                {t(status.data.geminiConfigured ? 'ai.geminiReady' : 'ai.geminiMissing')}
              </Badge>
              <Badge color={status.data.tmdbConfigured ? 'green' : 'gray'}>
                {t(status.data.tmdbConfigured ? 'ai.tmdbReady' : 'ai.tmdbMissing')}
              </Badge>
            </Group>
            {!ready && <Alert color="yellow">{t('ai.setup')}</Alert>}
            <form
              onSubmit={async (event) => {
                event.preventDefault();
                if (!ready || request.current) return;
                const controller = new AbortController();
                request.current = controller;
                setBusy(true);
                setError('');
                try {
                  const response = await api.POST('/api/v1/admin/recommendations', {
                    body: {
                      focus,
                      preferences,
                      language: i18n.language.startsWith('vi') ? 'vi-VN' : 'en-US',
                    },
                    signal: controller.signal,
                  });
                  if (response.error || !response.data) {
                    const code = response.response.status;
                    setError(
                      code === 429
                        ? 'ai.rateLimited'
                        : code === 422
                          ? 'ai.collectionRequired'
                          : code === 503
                            ? 'ai.setup'
                            : 'ai.failed',
                    );
                  } else setGenerated(response.data);
                } catch {
                  if (!controller.signal.aborted) setError('ai.failed');
                } finally {
                  request.current = null;
                  if (!controller.signal.aborted) setBusy(false);
                }
              }}
            >
              <Stack gap="md">
                <Select
                  label={t('ai.focus')}
                  value={focus}
                  allowDeselect={false}
                  disabled={!ready || busy}
                  data={(['mixed', 'missing', 'discover'] as const).map((value) => ({
                    value,
                    label: t(`ai.focus.${value}`),
                  }))}
                  onChange={(value) => {
                    if (value === 'mixed' || value === 'missing' || value === 'discover')
                      setFocus(value);
                  }}
                />
                <Textarea
                  label={t('ai.preferences')}
                  placeholder={t('ai.preferencesPlaceholder')}
                  value={preferences}
                  maxLength={1000}
                  autosize
                  minRows={2}
                  maxRows={5}
                  disabled={!ready || busy}
                  onChange={(event) => setPreferences(event.currentTarget.value)}
                />
                <p className="muted">{t('ai.disclosure')}</p>
                <Group>
                  <Button
                    type="submit"
                    disabled={!ready}
                    loading={busy}
                    leftSection={<Sparkles size={16} />}
                  >
                    {t('ai.generate')}
                  </Button>
                  {busy && <span role="status">{t('ai.generating')}</span>}
                </Group>
              </Stack>
            </form>
          </>
        )}
      </State>
      {error && (
        <Alert color="red" role="alert">
          {t(error)}
        </Alert>
      )}
      {data && (
        <Stack gap="md" aria-live="polite">
          <p className="muted">
            {t('ai.summary', { analyzed: data.analyzedCount, total: data.catalogCount })}
            {' · '}
            {new Date(data.generatedAt).toLocaleString(i18n.language)}
            {' · '}
            {t(`ai.focus.${data.focus}`)}
          </p>
          {data.analyzedCount < data.catalogCount && <Alert color="gray">{t('ai.sampled')}</Alert>}
          <p className="muted">{t('ai.review')}</p>
          {!data.movies.length ? (
            <Alert color="gray">{t('ai.empty')}</Alert>
          ) : (
            <div className="ai-recommendation-grid">
              {data.movies.map((movie) => (
                <article className="ai-recommendation-card" key={movie.tmdbId}>
                  <h3>
                    {movie.title} <span className="muted">({movie.year})</span>
                  </h3>
                  {movie.originalTitle && movie.originalTitle !== movie.title && (
                    <small className="muted">{movie.originalTitle}</small>
                  )}
                  <p>{movie.reason}</p>
                  <small className="muted">{t('ai.basedOn')}</small>
                  <Group gap="xs">
                    {movie.relatedTitles.map((title) => (
                      <Badge
                        key={title}
                        variant="light"
                        style={{ whiteSpace: 'normal', height: 'auto' }}
                      >
                        {title}
                      </Badge>
                    ))}
                  </Group>
                  <Anchor
                    href={`https://www.themoviedb.org/movie/${movie.tmdbId}`}
                    target="_blank"
                    rel="noopener noreferrer"
                  >
                    {t('ai.viewTMDB')} <ArrowUpRight size={14} aria-hidden="true" />
                  </Anchor>
                </article>
              ))}
            </div>
          )}
        </Stack>
      )}
    </Stack>
  );
}
