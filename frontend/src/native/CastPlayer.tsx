import { useEffect, useEffectEvent, useRef, useState } from 'react';
import {
  Alert,
  Button,
  CopyButton,
  Group,
  Loader,
  Slider,
  Stack,
  Textarea,
  Tooltip,
} from '@mantine/core';
import { Cast, Pause, Play, SkipBack, SkipForward, Volume2 } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { api, result, useAuth, type DTO } from './api';
import {
  castAudioMIME,
  castCapabilities,
  castErrorCode,
  castMediaInfo,
  castSessionMedia,
  castSupported,
  loadCast,
} from './chromecast';
import { createPortal } from 'react-dom';
import { queryClient } from '../lib/query-client';

// Keep the selected receiver across episode/queue changes within this account.
let castOwner: string | undefined;
let receiverLoads = Promise.resolve();
const RETRY_DELAYS = [1000, 2000, 4000, 8000, 16000, 30000];

type Options = Pick<
  DTO<'PlaybackRequest'>,
  'audioIndex' | 'subtitleIndex' | 'subtitleId' | 'subtitleDelay' | 'subtitleFont'
>;
export function CastPlayer({
  file,
  title,
  position,
  options,
  onActive,
  onPosition,
  onFinished,
  onNext,
  onPrevious,
  buttonTarget,
}: {
  file?: DTO<'FileDTO'>;
  title: string;
  position: number;
  options?: Options;
  onActive: (active: boolean) => void;
  onPosition: (position: number) => void;
  onFinished: () => void;
  onNext?: () => void;
  onPrevious?: () => void;
  buttonTarget?: HTMLElement | null;
}) {
  const { t } = useTranslation();
  const auth = useAuth((s) => s.token);
  const user = useAuth((s) => s.user?.id);
  const exhausted = useRef(false);
  const [available, setAvailable] = useState(false);
  const [discovery, setDiscovery] = useState<'loading' | 'ready' | 'failed'>('loading');
  const [active, setActive] = useState(false);
  const [connecting, setConnecting] = useState(false);
  const [ready, setReady] = useState(false);
  const [failed, setFailed] = useState(false);
  const [failureKey, setFailureKey] = useState('cast.failed');
  const [errorCode, setErrorCode] = useState<string>();
  const [phase, setPhase] = useState<'preparing' | 'loading' | 'playing'>('preparing');
  const preferHLS = useRef(false);
  const [retrying, setRetrying] = useState(false);
  const [needsDevice, setNeedsDevice] = useState(false);
  const retryAttempts = useRef(0);
  const manualRetry = useRef<() => void>(() => {});
  const desiredPaused = useRef(false);
  const selectingDevice = useRef(false);
  const cancelRecovery = useRef<() => void>(() => {});
  const [paused, setPaused] = useState(false);
  const [remotePosition, setRemotePosition] = useState(position);
  const [volume, setVolume] = useState(0.8);
  const [device, setDevice] = useState('');
  const [inactiveInput, setInactiveInput] = useState(false);
  const [receiverURL, setReceiverURL] = useState('');
  const [revision, setRevision] = useState(0);
  const initial = useRef(position);
  const latest = useRef(position);
  const control = useRef<cast.framework.RemotePlayerController | undefined>(undefined);
  const remote = useRef<cast.framework.RemotePlayer | undefined>(undefined);
  const owned = useRef<DTO<'PlaybackDTO'> | undefined>(undefined);
  const selected = useRef<Options | undefined>(undefined);
  const notifyActive = useEffectEvent(onActive);
  const notifyPosition = useEffectEvent(onPosition);
  const notifyFinished = useEffectEvent(onFinished);

  const observeSession = useEffectEvent((context: cast.framework.CastContext) => {
    // A missing session is also a transient transport state. The playback
    // effect owns recovery while active; do not resume the local player here.
    if (!active && !context.getCurrentSession()) castOwner = undefined;
  });
  useEffect(() => {
    if (!castSupported()) return;
    let disposed = false;
    let context: cast.framework.CastContext | undefined;
    const update = () => {
      if (disposed || !context) return;
      setDiscovery('ready');
      setAvailable(context.getCastState() !== cast.framework.CastState.NO_DEVICES_AVAILABLE);
      observeSession(context);
    };
    void loadCast()
      .then((value) => {
        if (disposed) return;
        context = value;
        context.addEventListener(cast.framework.CastContextEventType.CAST_STATE_CHANGED, update);
        update();
      })
      .catch(() => {
        if (!disposed) setDiscovery('failed');
      });
    return () => {
      disposed = true;
      context?.removeEventListener(cast.framework.CastContextEventType.CAST_STATE_CHANGED, update);
    };
  }, []);

  useEffect(() => {
    notifyActive(active);
  }, [active]);
  const prepare = useEffectEvent(() => {
    initial.current = position;
    latest.current = position;
    selected.current = options;
    setRemotePosition(position);
    setActive(true);
  });
  useEffect(() => {
    if (
      file &&
      available &&
      !active &&
      !exhausted.current &&
      user &&
      castOwner === user &&
      window.cast?.framework &&
      cast.framework.CastContext.getInstance().getCurrentSession()
    ) {
      prepare();
    }
  }, [file, available, active, user]);

  const optionIdentity = JSON.stringify(options);
  const synchronizeOptions = useEffectEvent(() => {
    if (JSON.stringify(selected.current) === optionIdentity) return;
    initial.current = latest.current;
    selected.current = options;
    setRevision((value) => value + 1);
  });
  useEffect(() => {
    if (active) synchronizeOptions();
  }, [active, optionIdentity]);

  useEffect(() => {
    if (!active || !file || !auth) return;
    const headers = { Authorization: `Bearer ${auth}` };
    const abort = new AbortController();
    let disposed = false;
    let session: DTO<'PlaybackDTO'> | undefined;
    let context: cast.framework.CastContext | undefined;
    let receiver: cast.framework.CastSession | null = null;
    let player: cast.framework.RemotePlayer | undefined;
    let controller: cast.framework.RemotePlayerController | undefined;
    let mediaURL = '';
    let base = 0;
    let sequence = 0;
    let progress = Promise.resolve();
    let loaded = false;
    let finished = false;
    let receiverID = '';
    let retryTimer: number | undefined;
    let healthySince = 0;
    let bufferingSince = 0;
    let missingMediaSince = 0;
    let lastObservedPosition = latest.current;
    let recoveryAllowed = true;
    let loadingReceiver = false;
    let observedMedia: chrome.cast.media.Media | null = null;
    let statusPending = false;
    let statusUnavailable = false;
    setReceiverURL('');
    setRetrying(false);
    setNeedsDevice(false);
    setReady(false);
    setFailed(false);
    if (retryAttempts.current === 0) {
      setFailureKey('cast.failed');
      setErrorCode(undefined);
    }
    setPhase('preparing');
    const report = () => {
      if (!session || !loaded) return;
      const id = session.id;
      const body = { sequence: ++sequence, position: latest.current };
      progress = progress
        .then(async () => {
          result(
            await api.POST('/api/v1/playback/{id}/progress', {
              params: { path: { id } },
              body,
              headers,
              keepalive: true,
              signal: AbortSignal.timeout(5000),
            }),
          );
          void queryClient.invalidateQueries({ queryKey: ['native'] });
        })
        .catch(() => {});
    };
    const scheduleRecovery = (kind: 'connection' | 'media') => {
      if (disposed || finished || !recoveryAllowed || retryTimer !== undefined) return;
      healthySince = 0;
      setReady(false);
      if (kind === 'connection' && !navigator.onLine) {
        setRetrying(true);
        return;
      }
      if (retryAttempts.current >= RETRY_DELAYS.length) {
        setRetrying(false);
        setNeedsDevice(kind === 'connection');
        setFailed(true);
        return;
      }
      setRetrying(true);
      setFailed(false);
      const delay = RETRY_DELAYS[retryAttempts.current++];
      retryTimer = window.setTimeout(() => {
        retryTimer = undefined;
        if (disposed) return;
        if (kind === 'media') {
          initial.current = latest.current;
          setRevision((value) => value + 1);
        } else if (!navigator.onLine) {
          retryAttempts.current = Math.max(0, retryAttempts.current - 1);
        } else if (receiverID) {
          // This rejoins an existing session; requestSession() would open a
          // device picker and is reserved for an explicit user click.
          try {
            chrome.cast.requestSessionById(receiverID);
          } catch {
            /* retry on next tick */
          }
        }
      }, delay);
    };
    const connectedReceiver = () => {
      const current = context?.getCurrentSession();
      return current &&
        current.getSessionObj().status === chrome.cast.SessionStatus.CONNECTED &&
        [
          cast.framework.SessionState.SESSION_STARTED,
          cast.framework.SessionState.SESSION_RESUMED,
        ].includes(current.getSessionState())
        ? current
        : null;
    };
    const releaseToLocal = () => {
      recoveryAllowed = false;
      castOwner = undefined;
      setActive(false);
    };
    cancelRecovery.current = () => {
      recoveryAllowed = false;
      window.clearTimeout(retryTimer);
      retryTimer = undefined;
    };
    manualRetry.current = () => {
      retryAttempts.current = 0;
      recoveryAllowed = true;
      window.clearTimeout(retryTimer);
      retryTimer = undefined;
      scheduleRecovery(connectedReceiver() ? 'media' : 'connection');
    };
    const update = () => {
      if (disposed || selectingDevice.current || !loaded || !player || !receiver || finished)
        return;
      const current = connectedReceiver();
      if (!current || !player.isConnected) {
        if (navigator.onLine) scheduleRecovery('connection');
        else {
          setRetrying(true);
          setReady(false);
        }
        return;
      }
      if (
        current.getApplicationMetadata().applicationId !==
        chrome.cast.media.DEFAULT_MEDIA_RECEIVER_APP_ID
      ) {
        releaseToLocal();
        return;
      }
      if (current.getSessionId() !== receiverID) {
        releaseToLocal();
        return;
      }
      receiver = current;
      setInactiveInput(
        receiver.getActiveInputState() === cast.framework.ActiveInputState.ACTIVE_INPUT_STATE_NO,
      );
      const activeMedia = receiver.getMediaSession();
      if (activeMedia?.media?.contentId && activeMedia.media.contentId !== mediaURL) {
        releaseToLocal();
        return;
      }
      const media = castSessionMedia(receiver, mediaURL);
      if (observedMedia !== media) {
        observedMedia?.removeUpdateListener(mediaUpdated);
        observedMedia = media;
        observedMedia?.addUpdateListener(mediaUpdated);
      }
      if (!media?.media) {
        setRetrying(true);
        setReady(false);
        if (!missingMediaSince) missingMediaSince = Date.now();
        if (Date.now() - missingMediaSince >= 5000) scheduleRecovery('media');
        return;
      }
      missingMediaSince = 0;
      if (media.idleReason === chrome.cast.media.IdleReason.ERROR) {
        setErrorCode('receiver_media_error');
        setFailureKey('cast.receiverLoadFailed');
        if (session?.method === 'direct') preferHLS.current = true;
        scheduleRecovery('media');
        return;
      }
      if (media.idleReason === chrome.cast.media.IdleReason.CANCELLED) {
        releaseToLocal();
        return;
      }
      if (media.idleReason === chrome.cast.media.IdleReason.FINISHED && !finished) {
        finished = true;
        exhausted.current = true;
        latest.current = file.duration;
        notifyPosition(latest.current);
        report();
        notifyFinished();
      }
      if (finished) return;
      // Cached PLAYING state must not cancel recovery after a failed status request.
      if (statusUnavailable) {
        scheduleRecovery('connection');
        return;
      }
      // Do not clear a scheduled stall retry on every BUFFERING heartbeat.
      if (
        media.playerState === chrome.cast.media.PlayerState.PLAYING ||
        media.playerState === chrome.cast.media.PlayerState.PAUSED
      ) {
        window.clearTimeout(retryTimer);
        retryTimer = undefined;
        setRetrying(false);
        setNeedsDevice(false);
        setFailed(false);
        setReady(true);
        setPhase('playing');
        setErrorCode(undefined);
      }
      // CAF resets currentTime to zero on IDLE. Keep the final source position.
      if (media.playerState !== chrome.cast.media.PlayerState.IDLE) {
        latest.current = Math.max(0, Math.min(file.duration, base + media.getEstimatedTime()));
        setRemotePosition(latest.current);
        notifyPosition(latest.current);
      }
      if (
        media.playerState === chrome.cast.media.PlayerState.PLAYING ||
        media.playerState === chrome.cast.media.PlayerState.PAUSED
      )
        desiredPaused.current = media.playerState === chrome.cast.media.PlayerState.PAUSED;
      setPaused(media.playerState === chrome.cast.media.PlayerState.PAUSED);
      setVolume(player.volumeLevel);
    };
    function mediaUpdated(alive: boolean) {
      if (alive) statusUnavailable = false;
      update();
    }
    const refreshStatus = () => {
      if (
        disposed ||
        selectingDevice.current ||
        !loaded ||
        finished ||
        statusPending ||
        !connectedReceiver()
      )
        return;
      const current = connectedReceiver();
      const media = current ? castSessionMedia(current, mediaURL) : null;
      if (!media || media.idleReason) {
        update();
        return;
      }
      const isCurrent = () => {
        const latestReceiver = connectedReceiver();
        return (
          !disposed &&
          latestReceiver?.getSessionId() === receiverID &&
          castSessionMedia(latestReceiver, mediaURL) === media
        );
      };
      statusPending = true;
      media.getStatus(
        new chrome.cast.media.GetStatusRequest(),
        () => {
          statusPending = false;
          if (!isCurrent()) return;
          statusUnavailable = false;
          update();
        },
        (cause) => {
          statusPending = false;
          if (!isCurrent()) return;
          if (media.idleReason) {
            update();
            return;
          }
          statusUnavailable = true;
          setErrorCode(castErrorCode(cause) ?? 'receiver_status_unavailable');
          setFailureKey('cast.statusUnavailable');
          scheduleRecovery('connection');
        },
      );
    };
    const sessionChanged = (event: cast.framework.SessionStateEventData) => {
      if (disposed || selectingDevice.current || finished) return;
      if (event.sessionState === cast.framework.SessionState.SESSION_ENDING) releaseToLocal();
      else if (event.sessionState === cast.framework.SessionState.SESSION_ENDED) {
        scheduleRecovery('connection');
      } else if (
        event.sessionState === cast.framework.SessionState.SESSION_RESUMED ||
        event.sessionState === cast.framework.SessionState.SESSION_STARTED
      ) {
        if (loaded) {
          update();
          refreshStatus();
        } else if (retryTimer !== undefined) {
          window.clearTimeout(retryTimer);
          retryTimer = undefined;
          initial.current = latest.current;
          setRevision((value) => value + 1);
        }
      }
    };
    const checkHealth = () => {
      if (disposed || selectingDevice.current || finished || !context) return;
      if (!connectedReceiver()) {
        if (navigator.onLine) scheduleRecovery('connection');
        else {
          setRetrying(true);
          setReady(false);
        }
        return;
      }
      if (!loaded || !player) return;
      update();
      if (!recoveryAllowed || !player.isConnected || statusUnavailable) return;
      const media = receiver ? castSessionMedia(receiver, mediaURL) : null;
      if (!media?.media || media.idleReason) return;
      const now = Date.now();
      if (media.playerState === chrome.cast.media.PlayerState.BUFFERING && !desiredPaused.current) {
        if (!bufferingSince || latest.current !== lastObservedPosition) bufferingSince = now;
        if (now - bufferingSince >= 30000) scheduleRecovery('media');
      } else {
        bufferingSince = 0;
        if (
          (media.playerState === chrome.cast.media.PlayerState.PLAYING &&
            latest.current > lastObservedPosition) ||
          media.playerState === chrome.cast.media.PlayerState.PAUSED
        ) {
          if (!healthySince) healthySince = now;
          if (now - healthySince >= 30000) retryAttempts.current = 0;
        }
      }
      lastObservedPosition = latest.current;
    };
    void (async () => {
      try {
        context = await loadCast();
        if (disposed) return;
        receiver = context.getCurrentSession();
        if (!receiver) {
          recoveryAllowed = false;
          setNeedsDevice(true);
          throw new Error('No receiver');
        }
        // Cast always uses receiver-side URL playback. Never accept a tab or
        // screen mirroring application, including during automatic recovery.
        if (
          receiver.getApplicationMetadata().applicationId !==
          chrome.cast.media.DEFAULT_MEDIA_RECEIVER_APP_ID
        ) {
          recoveryAllowed = false;
          setFailureKey('cast.receiverRequired');
          throw new Error('Unsupported Cast receiver');
        }
        receiverID = receiver.getSessionId();
        context.addEventListener(
          cast.framework.CastContextEventType.SESSION_STATE_CHANGED,
          sessionChanged,
        );
        setDevice(receiver.getCastDevice().friendlyName);
        player = new cast.framework.RemotePlayer();
        controller = new cast.framework.RemotePlayerController(player);
        remote.current = player;
        control.current = controller;
        const audio = !file.tracks.some((track) => track.type === 'video');
        const mime = audio ? castAudioMIME(file) : 'video/mp4';
        const capabilities = receiver.getCastDevice().capabilities;
        if (
          !audio &&
          capabilities?.length &&
          !capabilities.includes(chrome.cast.Capability.VIDEO_OUT)
        ) {
          recoveryAllowed = false;
          setFailureKey('cast.videoReceiverRequired');
          setNeedsDevice(true);
          throw new Error('Receiver has no video output');
        }
        const creation = api.POST('/api/v1/playback', {
          headers,
          body: {
            target: 'chromecast',
            fileId: file.id,
            position: initial.current,
            directPlay: !preferHLS.current && (audio ? Boolean(mime) : true),
            ...(audio ? {} : { capabilities: castCapabilities(file) }),
            audioIndex: -1,
            subtitleIndex: -1,
            maxHeight: audio ? 0 : 1080,
            maxBitrate: 0,
            ...selected.current,
          },
        });
        // Keep the create request alive so its eventual ID can be released,
        // but bound how long an idle TV waits for the backend response.
        let creationExpired = false;
        let creationTimer: number | undefined;
        const creationDeadline = new Promise<never>((_, reject) => {
          creationTimer = window.setTimeout(() => {
            creationExpired = true;
            reject(new DOMException('Playback creation timed out', 'TimeoutError'));
          }, 30000);
        });
        void creation
          .then((response) => {
            if (creationExpired && response.data)
              void api
                .DELETE('/api/v1/playback/{id}', {
                  params: { path: { id: response.data.id } },
                  headers,
                  keepalive: true,
                })
                .catch(() => {});
          })
          .catch(() => {});
        const response = await Promise.race([creation, creationDeadline]).finally(() =>
          window.clearTimeout(creationTimer),
        );
        recoveryAllowed =
          response.response.ok ||
          response.response.status >= 500 ||
          [408, 429].includes(response.response.status);
        if (!response.response.ok) {
          setErrorCode(String(response.response.status));
          if (response.error?.detail === 'Video transcoding is disabled')
            setFailureKey('native.transcodingDisabled');
          else if (response.error?.detail === 'HDR video transcoding is unavailable')
            setFailureKey('native.hdrTranscodingUnavailable');
          else setFailureKey('cast.prepareFailed');
        }
        session = result(response);
        if (disposed) return;
        const token = session.streamToken;
        const playbackID: string = session.id;
        const deadline = Date.now() + 120000;
        while (session.state === 'preparing') {
          if (disposed) return;
          if (Date.now() > deadline)
            throw new DOMException('Preparation timed out', 'TimeoutError');
          await new Promise<void>((resolve) => setTimeout(resolve, 500));
          if (disposed) return;
          const response = await api.GET('/api/v1/playback/{id}', {
            params: { path: { id: playbackID } },
            headers,
            signal: AbortSignal.any([abort.signal, AbortSignal.timeout(15000)]),
          });
          recoveryAllowed =
            response.response.ok ||
            response.response.status >= 500 ||
            [408, 429].includes(response.response.status);
          if (!response.response.ok) {
            setErrorCode(String(response.response.status));
            setFailureKey('cast.prepareFailed');
          }
          session = result(response);
        }
        if (session.state !== 'ready' || !token) {
          recoveryAllowed = false;
          setFailureKey('cast.prepareFailed');
          throw new Error('Playback unavailable');
        }
        owned.current = session;
        base = session.method === 'direct' ? 0 : session.position;
        const url = new URL(session.url, window.location.origin);
        url.searchParams.set('token', token);
        mediaURL = url.href;
        const info = castMediaInfo(session, mediaURL, mime);
        info.streamType = chrome.cast.media.StreamType.BUFFERED;
        info.duration = Math.max(0, file.duration - base);
        const metadata = new chrome.cast.media.GenericMediaMetadata();
        metadata.title = title;
        info.metadata = metadata;
        const request = new chrome.cast.media.LoadRequest(info);
        request.currentTime = session.method === 'direct' ? initial.current : 0;
        request.autoplay = !desiredPaused.current;
        // Serialize loads across retries and item changes. A cancelled old
        // receiver load must finish before a new one can replace its media.
        loadingReceiver = true;
        setPhase('loading');
        const load = receiverLoads
          .catch(() => {})
          .then(async () => {
            if (disposed) return;
            setReceiverURL(mediaURL);
            await receiver!.loadMedia(request);
            if (disposed && receiver!.getMediaSession()?.media?.contentId === mediaURL)
              controller!.stop();
          });
        receiverLoads = load;
        await load;
        if (disposed) {
          if (receiver.getMediaSession()?.media?.contentId === mediaURL) controller.stop();
          return;
        }
        loaded = true;
        controller.addEventListener(cast.framework.RemotePlayerEventType.ANY_CHANGE, update);
        setReady(true);
        update();
      } catch (cause) {
        if (!disposed) {
          if (!loadingReceiver && cause instanceof DOMException && cause.name === 'TimeoutError') {
            setErrorCode('request_timeout');
            setFailureKey('cast.prepareTimeout');
          } else if (!loadingReceiver && cause instanceof TypeError) {
            setErrorCode('network_error');
            setFailureKey('cast.prepareFailed');
          }
          if (loadingReceiver) {
            const code = castErrorCode(cause);
            setErrorCode(code ?? 'receiver_load_failed');
            setFailureKey('cast.receiverLoadFailed');
            if (code === chrome.cast.ErrorCode.LOAD_MEDIA_FAILED && session?.method === 'direct')
              preferHLS.current = true;
          }
          setFailed(true);
          setReady(false);
          if (mediaURL && receiver && receiver.getMediaSession()?.media?.contentId === mediaURL)
            controller?.stop();
          scheduleRecovery(connectedReceiver() ? 'media' : 'connection');
          if (session)
            void api
              .DELETE('/api/v1/playback/{id}', {
                params: { path: { id: session.id } },
                headers,
                keepalive: true,
              })
              .catch(() => {});
        }
      } finally {
        // The create call intentionally is not aborted: retain its ID to clean
        // up even when the user closes the player while creation is in flight.
        if (disposed && session)
          void api
            .DELETE('/api/v1/playback/{id}', {
              params: { path: { id: session.id } },
              headers,
              keepalive: true,
            })
            .catch(() => {});
      }
    })();
    const heartbeat = window.setInterval(() => {
      refreshStatus();
      report();
    }, 10000);
    const health = window.setInterval(checkHealth, 1000);
    window.addEventListener('online', checkHealth);
    window.addEventListener('pagehide', report);
    return () => {
      report();
      disposed = true;
      abort.abort();
      window.clearInterval(heartbeat);
      window.clearInterval(health);
      window.clearTimeout(retryTimer);
      manualRetry.current = () => {};
      cancelRecovery.current = () => {};
      window.removeEventListener('online', checkHealth);
      context?.removeEventListener(
        cast.framework.CastContextEventType.SESSION_STATE_CHANGED,
        sessionChanged,
      );
      window.removeEventListener('pagehide', report);
      observedMedia?.removeUpdateListener(mediaUpdated);
      controller?.removeEventListener(cast.framework.RemotePlayerEventType.ANY_CHANGE, update);
      if (mediaURL && receiver && receiver.getMediaSession()?.media?.contentId === mediaURL)
        controller?.stop();
      control.current = undefined;
      remote.current = undefined;
      owned.current = undefined;
      if (session) {
        const id = session.id;
        void progress
          .finally(() =>
            api.DELETE('/api/v1/playback/{id}', {
              params: { path: { id } },
              headers,
              keepalive: true,
            }),
          )
          .catch(() => {});
      }
    };
  }, [active, file, auth, revision, title]);

  if (!file) return null;
  const unavailableKey = !window.isSecureContext
    ? 'cast.httpsRequired'
    : !castSupported()
      ? 'cast.browserRequired'
      : discovery === 'failed'
        ? 'cast.sdkUnavailable'
        : discovery === 'loading'
          ? 'cast.discovering'
          : 'cast.noDevices';
  const blocked = !available && !active;
  const action = (
    <Tooltip label={t(blocked ? unavailableKey : 'cast.directHelp')} withArrow>
      <span className="cast-action-wrapper" tabIndex={blocked ? 0 : undefined}>
        <button
          type="button"
          className={`icon-button cast-action${active ? ' active' : ''}`}
          disabled={connecting || blocked}
          aria-busy={connecting}
          aria-label={t(active ? 'cast.disconnect' : 'cast.connect')}
          title={t(blocked ? unavailableKey : 'cast.directHelp')}
          onClick={() => {
            if (active) {
              cancelRecovery.current();
              retryAttempts.current = 0;
              castOwner = undefined;
              cast.framework.CastContext.getInstance().endCurrentSession(true);
              setActive(false);
              return;
            }
            setConnecting(true);
            void loadCast()
              .then(async (context) => {
                await context.requestSession();
                retryAttempts.current = 0;
                preferHLS.current = false;
                setErrorCode(undefined);
                desiredPaused.current = false;
                castOwner = user;
                exhausted.current = false;
                initial.current = position;
                latest.current = position;
                selected.current = options;
                setRemotePosition(position);
                setActive(true);
              })
              .catch(() => {})
              .finally(() => setConnecting(false));
          }}
        >
          {connecting ? <Loader size={18} /> : <Cast size={18} aria-hidden="true" />}
          <span className="cast-action-label">
            {t(active ? 'cast.disconnect' : 'cast.connect')}
          </span>
        </button>
      </span>
    </Tooltip>
  );
  return (
    <div className="cast-player" data-active={active}>
      {buttonTarget ? createPortal(action, buttonTarget) : action}
      {active && (
        <Stack gap="xs" aria-label={t('cast.controls')}>
          <span role="status">
            {t(
              retrying
                ? 'cast.reconnecting'
                : phase === 'preparing'
                  ? 'cast.preparing'
                  : phase === 'loading'
                    ? 'cast.loadingReceiver'
                    : 'cast.playingOn',
              { device },
            )}
          </span>
          {phase === 'playing' && <small>{t('cast.directPlayback')}</small>}
          {receiverURL && (
            <details>
              <summary>{t('cast.mediaURL')}</summary>
              <Stack gap="xs" mt="xs">
                <Textarea
                  aria-label={t('cast.mediaURL')}
                  value={receiverURL}
                  readOnly
                  autosize
                  minRows={2}
                  maxRows={5}
                  onFocus={(event) => event.currentTarget.select()}
                />
                <small>{t('cast.mediaURLHelp')}</small>
                <CopyButton value={receiverURL}>
                  {({ copied, copy }) => (
                    <Button variant="subtle" onClick={copy}>
                      {t(copied ? 'copied' : 'copy')}
                    </Button>
                  )}
                </CopyButton>
              </Stack>
            </details>
          )}
          {inactiveInput && <Alert color="yellow">{t('cast.inactiveInput')}</Alert>}
          {failed || errorCode ? (
            <Alert color={failed ? 'red' : 'yellow'}>
              {t(
                failureKey === 'cast.videoReceiverRequired'
                  ? failureKey
                  : needsDevice
                    ? 'cast.reconnectFailed'
                    : failureKey,
              )}
              {errorCode && <div>{t('cast.errorCode', { code: errorCode })}</div>}
              {failed && (
                <Button
                  variant="subtle"
                  loading={connecting}
                  onClick={() => {
                    if (!needsDevice) {
                      manualRetry.current();
                      return;
                    }
                    // A destroyed receiver session cannot be silently relaunched
                    // by the Web Sender SDK. Only this user click opens a picker.
                    selectingDevice.current = true;
                    setConnecting(true);
                    void loadCast()
                      .then(async (context) => {
                        await context.requestSession();
                        castOwner = user;
                        exhausted.current = false;
                        retryAttempts.current = 0;
                        initial.current = latest.current;
                        setActive(true);
                        setRevision((value) => value + 1);
                      })
                      .catch(() => {})
                      .finally(() => {
                        selectingDevice.current = false;
                        setConnecting(false);
                      });
                  }}
                >
                  {t(needsDevice ? 'cast.chooseDevice' : 'retry')}
                </Button>
              )}
            </Alert>
          ) : (
            <>
              <Group>
                {onPrevious && (
                  <Button
                    variant="subtle"
                    disabled={!ready}
                    aria-label={t('previous')}
                    onClick={onPrevious}
                  >
                    <SkipBack size={18} />
                  </Button>
                )}
                <Button
                  loading={!ready}
                  aria-label={t(paused ? 'play' : 'pause')}
                  onClick={() => control.current?.playOrPause()}
                >
                  {paused ? <Play size={18} /> : <Pause size={18} />}
                </Button>
                {onNext && (
                  <Button
                    variant="subtle"
                    disabled={!ready}
                    aria-label={t('next')}
                    onClick={onNext}
                  >
                    <SkipForward size={18} />
                  </Button>
                )}
                <Volume2 size={18} aria-hidden />
                <Slider
                  w={100}
                  min={0}
                  max={1}
                  step={0.01}
                  value={volume}
                  disabled={!ready}
                  aria-label={t('volume')}
                  onChange={(value) => {
                    setVolume(value);
                    if (remote.current) remote.current.volumeLevel = value;
                    control.current?.setVolumeLevel();
                  }}
                />
              </Group>
              <span>
                {Math.floor(remotePosition / 60)}:
                {String(Math.floor(remotePosition % 60)).padStart(2, '0')} /{' '}
                {Math.floor(file.duration / 60)}:
                {String(Math.floor(file.duration % 60)).padStart(2, '0')}
              </span>
              <Slider
                min={0}
                max={file.duration || 1}
                value={remotePosition}
                disabled={!ready}
                aria-label={t('position')}
                onChange={setRemotePosition}
                onChangeEnd={(value) => {
                  latest.current = value;
                  onPosition(value);
                  if (owned.current?.method === 'direct' && remote.current) {
                    remote.current.currentTime = value;
                    control.current?.seek();
                  } else {
                    initial.current = value;
                    setRevision((v) => v + 1);
                  }
                }}
              />
            </>
          )}
        </Stack>
      )}
    </div>
  );
}
