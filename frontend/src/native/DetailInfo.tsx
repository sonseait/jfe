import { Badge, Button, Group, Progress } from '@mantine/core';
import { FileVideo, AudioLines, Captions, Check, Monitor } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import type { DTO } from './api';
import Cast from './Cast';

export default function DetailInfo({
  detail,
  activeFile,
  onSelect,
}: {
  detail: DTO<'DetailDTO'>;
  activeFile?: DTO<'FileDTO'>;
  onSelect: (id: string) => void;
}) {
  const { t, i18n } = useTranslation();
  const { item, files } = detail;
  const duration = activeFile?.duration ?? 0;
  const progress = duration > 0 ? Math.min(100, Math.max(0, (item.position / duration) * 100)) : 0;
  const language = (code: string) => {
    if (!code || code === 'und') return t('detail.unspecifiedLanguage');
    try {
      return new Intl.DisplayNames([i18n.language], { type: 'language' }).of(code) ?? code;
    } catch {
      return code;
    }
  };
  const size = (bytes: number) =>
    bytes > 0
      ? `${new Intl.NumberFormat(i18n.language, { maximumFractionDigits: 1 }).format(bytes / 1024 ** (bytes >= 1024 ** 3 ? 3 : 2))} ${bytes >= 1024 ** 3 ? 'GiB' : 'MiB'}`
      : t('detail.unknown');
  return (
    <div className={`film-detail-body${activeFile ? '' : ' film-detail-body-full'}`}>
      <section className="film-synopsis">
        <span className="eyebrow">{t('detail.theStory')}</span>
        <h2>{t('detail.overview')}</h2>
        <p>{item.overview || t('detail.noOverview')}</p>
        <Cast key={item.id} members={detail.cast} />
        {item.position > 0 && !item.watched && duration > 0 && (
          <div className="film-watch-progress">
            <Group justify="space-between">
              <strong>{t('detail.continueWatching')}</strong>
              <span>
                {t('detail.remaining', {
                  count: Math.ceil(Math.max(0, duration - item.position) / 60),
                })}
              </span>
            </Group>
            <Progress value={progress} aria-label={t('detail.watchProgress')} />
            <small>
              {t('detail.progress', {
                percent: Math.round(progress),
                minutes: Math.floor(item.position / 60),
              })}
            </small>
          </div>
        )}
        {files.length > 0 && (
          <section className="film-files">
            <Group justify="space-between">
              <h2>{t('detail.versions')}</h2>
              <Badge variant="light">{files.length}</Badge>
            </Group>
            <p>{t('detail.versionsHelp')}</p>
            {files.map((file) => (
              <article
                className={`film-file ${file.id === activeFile?.id ? 'is-selected' : ''}`}
                key={file.id}
              >
                <FileVideo size={24} />
                <div className="film-file-info">
                  <strong>{file.name}</strong>
                  <span>
                    {[
                      size(file.size),
                      file.width > 0 && file.height > 0 ? `${file.width} × ${file.height}` : '',
                      file.duration > 0
                        ? t('detail.minutes', { count: Math.ceil(file.duration / 60) })
                        : '',
                    ]
                      .filter(Boolean)
                      .join(' · ')}
                  </span>
                </div>
                <Button
                  size="xs"
                  variant={file.id === activeFile?.id ? 'light' : 'default'}
                  disabled={!file.available || file.id === activeFile?.id}
                  onClick={() => onSelect(file.id)}
                  leftSection={file.id === activeFile?.id ? <Check size={14} /> : undefined}
                >
                  {t(
                    !file.available
                      ? 'detail.unavailable'
                      : file.id === activeFile?.id
                        ? 'detail.selected'
                        : 'detail.select',
                  )}
                </Button>
              </article>
            ))}
          </section>
        )}
      </section>
      {activeFile && (
        <aside className="film-media-info">
          <span className="eyebrow">{t('detail.mediaInfo')}</span>
          <h2>{t('detail.pictureAndSound')}</h2>
          <dl className="film-specs">
            <div>
              <dt>{t('detail.resolution')}</dt>
              <dd>
                {activeFile.width > 0 && activeFile.height > 0
                  ? `${activeFile.width} × ${activeFile.height}`
                  : t('detail.unknown')}
              </dd>
            </div>
            <div>
              <dt>{t('detail.fileSize')}</dt>
              <dd>{size(activeFile.size)}</dd>
            </div>
            <div>
              <dt>{t('detail.duration')}</dt>
              <dd>
                {duration > 0
                  ? t('detail.minutes', { count: Math.ceil(duration / 60) })
                  : t('detail.unknown')}
              </dd>
            </div>
          </dl>
          {(['video', 'audio', 'subtitle'] as const).map((type) => {
            const tracks = activeFile.tracks.filter((track) => track.type === type);
            const Icon = type === 'video' ? Monitor : type === 'audio' ? AudioLines : Captions;
            return (
              <section className="film-track-group" key={type}>
                <h3>
                  <Icon size={17} />
                  {t(`detail.${type}`)}
                  <span>{tracks.length}</span>
                </h3>
                {tracks.length === 0 ? (
                  <p>{t('detail.noTracks')}</p>
                ) : (
                  tracks.map((track) => (
                    <div className="film-track" key={track.index}>
                      <div>
                        <strong>
                          {track.title ||
                            (type === 'video' ? t('detail.video') : language(track.language))}
                        </strong>
                        {track.title && track.language && <small>{language(track.language)}</small>}
                      </div>
                      <Badge variant="outline" color="gray">
                        {track.codec || t('detail.unknown')}
                      </Badge>
                    </div>
                  ))
                )}
              </section>
            );
          })}
        </aside>
      )}
    </div>
  );
}
