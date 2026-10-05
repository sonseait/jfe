import { useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import toast from 'react-hot-toast';
import { api, result, useAuth, useResource } from './api';
import { queryClient } from '../lib/query-client';

export function usePersonalSubtitleTiming(fileId: string | undefined, subtitle: string) {
  const { t } = useTranslation();
  const userId = useAuth((state) => state.user?.id);
  const token = useAuth((state) => state.token);
  const timing = useResource(
    ['subtitleTiming', fileId],
    async (signal) =>
      result(
        await api.GET('/api/v1/files/{id}/subtitle-timing', {
          params: { path: { id: fileId! } },
          signal,
        }),
      ),
    Boolean(fileId && token),
  );
  const identity = `${userId}:${fileId}:${timing.data?.revision}:${subtitle}`;
  const [draft, setDraft] = useState<{ identity: string; value: number }>();
  const queue = useRef<Promise<void>>(Promise.resolve());
  const value =
    draft?.identity === identity ? draft.value : (timing.data?.offsets?.[subtitle] ?? 0) / 1000;
  const change = (value: number) => setDraft({ identity, value });
  const save = (value: number) => {
    if (!fileId || !timing.data?.revision || subtitle === '-1') return;
    change(value);
    // Serialize releases/reset so a slower request cannot overwrite a newer value.
    const body = { subtitle, offsetMs: Math.round(value * 1000), revision: timing.data.revision };
    queue.current = queue.current.then(async () => {
      try {
        const data = result(
          await api.PUT('/api/v1/files/{id}/subtitle-timing', {
            params: { path: { id: fileId } },
            headers: { Authorization: `Bearer ${token}` },
            body,
          }),
        );
        queryClient.setQueryData(['native', userId, 'subtitleTiming', fileId], data);
      } catch {
        toast.error(t('playerSub.timingSaveError'));
      }
    });
  };
  return { value, change, save, ready: Boolean(timing.data?.revision), loading: timing.isLoading };
}
