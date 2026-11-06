import { useEffect, useRef, useState } from 'react';
import {
  Alert,
  Button,
  Group,
  Loader,
  Modal,
  ScrollArea,
  Select,
  Stack,
  Text,
  TextInput,
} from '@mantine/core';
import { Search, Download } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import toast from 'react-hot-toast';
import { api, result, useResource, type DTO } from './api';

export function OpenSubtitles({
  fileId,
  fileName,
  onDownloaded,
}: {
  fileId: string;
  fileName: string;
  onDownloaded: (id: string) => Promise<void>;
}) {
  const { t, i18n } = useTranslation();
  const [opened, setOpened] = useState(false);
  const [search, setSearch] = useState('');
  const [language, setLanguage] = useState<'vi' | 'en'>(
    i18n.language.startsWith('vi') ? 'vi' : 'en',
  );
  const [submitted, setSubmitted] = useState<{
    search: string;
    language: 'vi' | 'en';
    revision: number;
  }>();
  const [busy, setBusy] = useState(false);
  const [job, setJob] = useState<DTO<'OpenSubtitleDownloadDTO'>>();
  const handled = useRef<string | undefined>(undefined);
  const data = useResource(
    ['openSubtitles', fileId, submitted],
    async (signal) =>
      result(
        await api.GET('/api/v1/files/{id}/opensubtitles', {
          params: {
            path: { id: fileId },
            query: { search: submitted?.search ?? '', language: submitted?.language ?? language },
          },
          signal,
        }),
      ),
    Boolean(opened && submitted),
  );
  const progress = useResource(
    ['openSubtitleDownload', fileId, job?.jobId],
    async (signal) =>
      result(
        await api.GET('/api/v1/files/{id}/opensubtitles/jobs/{jobId}', {
          params: { path: { id: fileId, jobId: job!.jobId } },
          signal,
        }),
      ),
    Boolean(busy && job?.jobId),
    2000,
  );
  useEffect(() => {
    if (!busy || !job) return;
    const status = job.state === 'completed' ? job : progress.data;
    if (progress.error || status?.state === 'failed' || status?.state === 'cancelled') {
      setBusy(false);
      toast.error(t('openSub.downloadFailed'));
    } else if (status?.state === 'completed' && handled.current !== status.subtitleId) {
      handled.current = status.subtitleId;
      void onDownloaded(status.subtitleId)
        .then(() => {
          setOpened(false);
          toast.success(t('openSub.ready'));
        })
        .catch(() => toast.error(t('error')))
        .finally(() => setBusy(false));
    }
  }, [busy, job, progress.data, progress.error, onDownloaded, t]);
  const download = async (file: DTO<'OpenSubtitleDTO'>) => {
    setBusy(true);
    setJob(undefined);
    handled.current = undefined;
    try {
      setJob(
        result(
          await api.POST('/api/v1/files/{id}/opensubtitles/download', {
            params: { path: { id: fileId } },
            body: { fileId: file.fileId },
          }),
        ),
      );
    } catch {
      setBusy(false);
      toast.error(t('openSub.downloadFailed'));
    }
  };
  return (
    <>
      <Button
        variant="default"
        leftSection={<Search size={15} />}
        onClick={() => {
          setOpened(true);
          if (!submitted) setSubmitted({ search: '', language, revision: 0 });
        }}
      >
        {t('openSub.search')}
      </Button>
      <Modal
        opened={opened}
        onClose={() => setOpened(false)}
        title={t('openSub.search')}
        size="min(1100px, calc(100vw - 32px))"
        className="opensubtitles-modal"
        scrollAreaComponent={ScrollArea.Autosize}
      >
        <Stack>
          <Text size="sm" fw={500} style={{ overflowWrap: 'anywhere' }}>
            {fileName}
          </Text>
          <Text size="sm" c="dimmed">
            {t('openSub.help')}
          </Text>
          <form
            onSubmit={(event) => {
              event.preventDefault();
              setSubmitted({
                search: search.trim(),
                language,
                revision: (submitted?.revision ?? 0) + 1,
              });
            }}
          >
            <Stack gap="xs">
              <TextInput
                label={t('search')}
                placeholder={t('openSub.automatic')}
                value={search}
                maxLength={200}
                onChange={(event) => setSearch(event.currentTarget.value)}
              />
              <Group align="end">
                <Select
                  className="opensubtitles-language"
                  label={t('language')}
                  value={language}
                  allowDeselect={false}
                  data={[
                    { value: 'vi', label: t('openSub.vietnamese') },
                    { value: 'en', label: t('openSub.english') },
                  ]}
                  onChange={(value) => setLanguage(value === 'vi' ? 'vi' : 'en')}
                />
                <Button
                  type="submit"
                  className="opensubtitles-search-button"
                  leftSection={<Search size={15} />}
                  loading={data.isFetching}
                >
                  {t('search')}
                </Button>
              </Group>
            </Stack>
          </form>
          {busy && (
            <Group>
              <Loader size="sm" />
              <Text size="sm">{t('openSub.downloading')}</Text>
            </Group>
          )}
          {data.error && <Alert color="red">{t('openSub.searchFailed')}</Alert>}
          {data.data?.searchTitle && (
            <Text size="sm" c="dimmed" style={{ overflowWrap: 'anywhere' }}>
              {t('openSub.searchTarget', { title: data.data.searchTitle })}
              {data.data.searchYear > 0 ? ` (${data.data.searchYear})` : ''}
            </Text>
          )}
          {data.data?.items.length === 0 && <Text c="dimmed">{t('openSub.empty')}</Text>}
          {data.data?.items.map((file) => (
            <div key={file.fileId} className="opensubtitles-result">
              <Stack gap={2} className="opensubtitles-result-details">
                <Text fw={500}>{file.release || file.name}</Text>
                <Text size="sm" c="dimmed">
                  {file.name}
                </Text>
                <Text size="xs" c="dimmed">
                  {t(file.language === 'vi' ? 'openSub.vietnamese' : 'openSub.english')} ·{' '}
                  {t('openSub.downloadCount', { count: file.downloads })}
                  {file.hearingImpaired ? ` · ${t('openSub.hearingImpaired')}` : ''}
                </Text>
              </Stack>
              <Button
                size="xs"
                className="opensubtitles-download-button"
                disabled={busy}
                leftSection={<Download size={14} />}
                onClick={() => void download(file)}
              >
                {t('openSub.download')}
              </Button>
            </div>
          ))}
        </Stack>
      </Modal>
    </>
  );
}
