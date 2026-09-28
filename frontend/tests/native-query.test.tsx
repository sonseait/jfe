import { afterEach, expect, it } from 'vitest';
import { act, renderHook, waitFor } from '@testing-library/react';
import { QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { queryClient } from '../src/lib/query-client';
import { useAuth, useResource, type DTO } from '../src/native/api';

const wrapper = ({ children }: { children: ReactNode }) => (
  <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
);
const session = (id: string): DTO<'LoginDTO'> => ({
  token: `token-${id}`,
  user: { id, username: id, role: 'user', disabled: false, libraryIds: [] },
});
afterEach(() => {
  useAuth.getState().logout();
  queryClient.clear();
});

it('changes resource keys when navigating between native detail pages', async () => {
  const hook = renderHook(({ id }) => useResource(['item', id], async () => ({ id })), {
    initialProps: { id: 'one' },
    wrapper,
  });
  await waitFor(() => expect(hook.result.current.data?.id).toBe('one'));
  hook.rerender({ id: 'two' });
  await waitFor(() => expect(hook.result.current.data?.id).toBe('two'));
  hook.unmount();
});

it('isolates resource data when the signed-in user changes', async () => {
  useAuth.getState().login(session('alice'));
  const hook = renderHook(() => useResource('library', async () => useAuth.getState().user?.id), {
    wrapper,
  });
  await waitFor(() => expect(hook.result.current.data).toBe('alice'));
  act(() => useAuth.getState().login(session('bob')));
  await waitFor(() => expect(hook.result.current.data).toBe('bob'));
  expect(queryClient.getQueryData(['native', 'alice', 'library'])).toBeUndefined();
  hook.unmount();
});

it('clears private cached data and credentials on logout', () => {
  useAuth.getState().login(session('alice'));
  queryClient.setQueryData(['native', 'alice', 'library'], ['private-item']);
  useAuth.getState().logout();
  expect(queryClient.getQueryCache().getAll()).toHaveLength(0);
  expect(useAuth.getState().token).toBeUndefined();
  expect(JSON.parse(localStorage.getItem('jfe.native-session')!).state).toEqual({});
});
