import { Popover } from '@mantine/core';
import { useTranslation } from 'react-i18next';
import type { DTO } from './api';

export function StreamIndicator({
  method,
  stream,
  size,
  portalTarget,
}: {
  method: string;
  stream?: DTO<'PlaybackStreamDTO'>;
  size: { width: number; height: number };
  portalTarget?: HTMLElement;
}) {
  const { t, i18n } = useTranslation();
  const format = (rate?: number) =>
    rate && rate > 0
      ? `${new Intl.NumberFormat(i18n.language, { maximumFractionDigits: 2 }).format(rate / 1_000_000)} Mbps`
      : t('playerMetrics.unavailable');
  // Only the worker's output report confirms video encoding, not server capability.
  const mode = stream
    ? stream.videoTranscoded
      ? 'transcode'
      : method === 'direct'
        ? 'direct'
        : 'remux'
    : method === 'direct'
      ? 'direct'
      : 'hls';
  return (
    <Popover position="top-end" width={280} portalProps={{ target: portalTarget }} shadow="lg">
      <Popover.Target>
        <button
          type="button"
          className="playback-indicator"
          data-method={mode}
          aria-label={t('playerStream.label')}
        >
          <span className="playback-indicator-dot" aria-hidden="true" />
          <span>{t(`playerStream.${mode}`)}</span>
          <strong>
            {size.width} × {size.height}
          </strong>
          {stream && stream.totalBitrate > 0 && <span>{format(stream.totalBitrate)}</span>}
        </button>
      </Popover.Target>
      <Popover.Dropdown className="playback-stream-details">
        <strong>{t('playerStream.label')}</strong>
        <p>{t(`playerStream.${mode}Help`)}</p>
        {stream?.toneMapped && <p>{t('playerStream.toneMapped')}</p>}
        <dl>
          <dt>{t('playerStream.resolution')}</dt>
          <dd>
            {size.width} × {size.height}
          </dd>
          <dt>{t('playerStream.total')}</dt>
          <dd>{format(stream?.totalBitrate)}</dd>
          <dt>{t('playerStream.video')}</dt>
          <dd>
            {format(stream?.videoBitrate)}
            {stream?.videoCodec && <small>{stream.videoCodec.toUpperCase()}</small>}
          </dd>
          <dt>{t('playerStream.audio')}</dt>
          <dd>
            {stream?.audioCodec ? (
              <>
                {format(stream.audioBitrate)}
                <small>{stream.audioCodec.toUpperCase()}</small>
              </>
            ) : (
              t('playerMetrics.unavailable')
            )}
          </dd>
        </dl>
        <p>
          {t(
            stream?.bitrateSource === 'segment'
              ? 'playerStream.sample'
              : stream?.bitrateSource === 'metadata'
                ? 'playerStream.metadata'
                : stream?.bitrateSource === 'pending'
                  ? 'playerStream.measuring'
                  : 'playerMetrics.unavailable',
          )}
        </p>
      </Popover.Dropdown>
    </Popover>
  );
}
