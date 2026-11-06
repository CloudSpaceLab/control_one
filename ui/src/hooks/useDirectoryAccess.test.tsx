import { act, renderHook, waitFor } from '@testing-library/react';
import { beforeEach, expect, it, vi } from 'vitest';
import { APIError } from '../lib/api';
import { useUsers } from './useUsers';
import { useRoles } from './useRoles';

const mocks = vi.hoisted(() => ({ api: { listUsers: vi.fn(), listRoles: vi.fn() }, toast: vi.fn() }));
vi.mock('./useApiClient', () => ({ useApiClient: () => mocks.api }));
vi.mock('../providers/ToastProvider', () => ({ useToast: () => ({showToast: mocks.toast}) }));
beforeEach(() => { vi.clearAllMocks(); });

for (const [name, useDirectory, list, response] of [
  ['users', useUsers, mocks.api.listUsers, { data: [], pagination: {total: 0, count: 0, limit: 50, offset: 0, nextOffset: null, prevOffset: null} }],
  ['roles', useRoles, mocks.api.listRoles, []],
] as const) {
  it(`${name}: retains HTTP 403 without a generic error toast and clears it after successful reload`, async () => {
    list.mockRejectedValueOnce(new APIError('Forbidden', 403)).mockResolvedValue(response);
    const { result } = renderHook(() => useDirectory());
    await waitFor(() => expect(result.current.errorStatus).toBe(403));
    expect(result.current.data).toEqual([]);
    expect(mocks.toast).not.toHaveBeenCalled();
    act(() => result.current.reload());
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.errorStatus).toBeNull();
    expect(result.current.error).toBeNull();
  });
  it(`${name}: keeps a server error distinct from authorization failure`, async () => {
    list.mockRejectedValue(new APIError('Service unavailable', 503));
    const { result } = renderHook(() => useDirectory());
    await waitFor(() => expect(result.current.errorStatus).toBe(503));
    expect(result.current.error).toBe('Service unavailable');
    expect(mocks.toast).toHaveBeenCalledWith('Service unavailable', 'error');
  });
}
