import createClient from 'openapi-fetch';
import { create } from 'zustand';
import { persist } from 'zustand/middleware';
import { useQuery } from '@tanstack/react-query';
import { queryClient } from '../lib/query-client';
import type { paths, components } from './generated';
export type DTO<K extends keyof components['schemas']> = components['schemas'][K];
interface Session {
  token?: string;
  user?: DTO<'UserDTO'>;
  login: (value: DTO<'LoginDTO'>) => void;
  logout: () => void;
}
export const useAuth = create<Session>()(
  persist(
    (set) => ({
      login: (value) => {
        queryClient.clear();
        set({ token: value.token, user: value.user });
      },
      logout: () => {
        queryClient.clear();
        set({ token: undefined, user: undefined });
      },
    }),
    { name: 'jfe.native-session', partialize: ({ token, user }) => ({ token, user }) },
  ),
);
export const api = createClient<paths>({ baseUrl: '' });
api.use({
  onRequest({ request }) {
    const token = useAuth.getState().token;
    if (token && !request.headers.has('Authorization'))
      request.headers.set('Authorization', `Bearer ${token}`);
    return request;
  },
  onResponse({ request, response }) {
    const token = useAuth.getState().token;
    if (
      response.status === 401 &&
      token &&
      request.headers.get('Authorization') === `Bearer ${token}`
    )
      useAuth.getState().logout();
    return response;
  },
});
export function result<T>(response: { data?: T; error?: unknown }): T {
  if (response.error || response.data === undefined) throw new Error('API request failed');
  return response.data;
}
export function useResource<T>(
  key: string | unknown[],
  fn: (signal: AbortSignal) => Promise<T>,
  enabled = true,
  interval?: number,
) {
  const id = useAuth((s) => s.user?.id);
  return useQuery({
    queryKey: ['native', id, ...(Array.isArray(key) ? key : [key])],
    queryFn: ({ signal }) => fn(signal),
    enabled,
    refetchInterval: interval,
  });
}
export function imageURL(item: DTO<'ItemDTO'>) {
  return item.poster ? `${item.poster}${encodeURIComponent(useAuth.getState().token ?? '')}` : '';
}
