import { Progress } from '@mantine/core';
import { useState, type ReactNode } from 'react';
import { Alert, Button, Loader, Modal, Stack } from '@mantine/core';
import { Film, Play, Music2 } from 'lucide-react';
import { Link } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import toast from 'react-hot-toast';
import i18n from '../locales';
import { queryClient } from '../lib/query-client';
import { imageURL, type DTO } from './api';
export function State({
  loading,
  error,
  children,
}: {
  loading?: boolean;
  error?: unknown;
  children: ReactNode;
}) {
  const { t } = useTranslation();
  if (loading) return <Loader />;
  if (error)
    return (
      <Alert color="red">
        {t('error')}
        <Button
          variant="subtle"
          onClick={() => void queryClient.refetchQueries({ type: 'active' })}
        >
          {t('retry')}
        </Button>
      </Alert>
    );
  return children;
}
export function Card({ item }: { item: DTO<'ItemDTO'> }) {
  const [failed, setFailed] = useState(false);
  const Icon = ['movie', 'series', 'episode'].includes(item.kind) ? Film : Music2;
  return (
    <Link className="poster" to={`/item/${item.id}`}>
      <div className="poster-art">
        {item.poster && !failed ? (
          <img src={imageURL(item)} alt="" loading="lazy" onError={() => setFailed(true)} />
        ) : (
          <div className="poster-fallback">
            <Icon size={40} strokeWidth={1} />
            <span>{item.title}</span>
          </div>
        )}
        <span className="poster-play">
          <Play size={23} />
        </span>
      </div>
      <h3>{item.title}</h3>
      <p>
        {item.year || ''}
        {item.kind === 'episode' ? ` S${item.season} · E${item.episode}` : ''}
      </p>
    </Link>
  );
}
export function Confirm({
  opened,
  close,
  run,
  name,
}: {
  opened: boolean;
  close: () => void;
  run: () => Promise<unknown>;
  name: string;
}) {
  const { t } = useTranslation();
  const [busy, setBusy] = useState(false);
  return (
    <Modal
      opened={opened}
      onClose={() => {
        if (!busy) close();
      }}
      title={t('confirm')}
    >
      <Stack>
        <p>{name}</p>
        <Button
          color="red"
          loading={busy}
          onClick={async () => {
            setBusy(true);
            if (await mutate(run)) close();
            setBusy(false);
          }}
        >
          {t('remove')}
        </Button>
      </Stack>
    </Modal>
  );
}
export async function mutate(run: () => Promise<unknown>) {
  try {
    await run();
    await queryClient.invalidateQueries();
    toast.success(i18n.t('saved'));
    return true;
  } catch {
    toast.error(i18n.t('error'));
    return false;
  }
}

export function LibraryStatus({ library }: { library: import('./api').DTO<'LibraryDTO'> }) {
  const { t } = useTranslation();
  const scan = library.scan;
  const active = scan?.state === 'running' || scan?.state === 'pending';
  return (
    <div>
      <p>{t('native.fileCount', { count: library.fileCount })}</p>
      {scan && (
        <p>
          {t('native.job.scan')}: {t(`native.state.${scan.state}`)}
          {scan.totalFiles > 0 &&
            ` · ${scan.processedFiles}/${scan.totalFiles} · ${scan.progress}%`}
          {scan.state === 'running' && scan.totalFiles === 0 && ` · ${t('native.discovering')}`}
        </p>
      )}
      {active && <Progress aria-label={t('native.job.scan')} value={scan.progress} animated />}
    </div>
  );
}
