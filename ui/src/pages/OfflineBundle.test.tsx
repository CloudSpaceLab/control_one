import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, act, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { ReactNode } from 'react';
import { OfflineBundle } from './OfflineBundle';
import * as useApiClientModule from '../hooks/useApiClient';
import * as useToastModule from '../providers/ToastProvider';
import * as useTenantModule from '../providers/TenantProvider';
import type { APIClient, EnrollmentToken, OfflineContentBundle } from '../lib/api';

// Minimal ToastProvider stub — real provider pulls createContext whose value
// we override via the module mock below, so we can render the page in tests
// without wiring AuthProvider + ToastProvider.
function Wrapper({ children }: { children: ReactNode }): JSX.Element {
  return <div>{children}</div>;
}

const sampleTokens: EnrollmentToken[] = [
  {
    id: 'tok-1',
    tenant_id: 'tenant-a',
    name: 'prod-fleet',
    max_nodes: 50,
    nodes_enrolled: 3,
    labels: {},
    capabilities: [],
    expires_at: '2030-01-01T00:00:00Z',
    created_at: '2024-01-01T00:00:00Z',
    // The list endpoint doesn't return `token`, so we leave it undefined to
    // simulate real production behaviour — the override input must be used.
  },
  {
    id: 'tok-2',
    tenant_id: 'tenant-b',
    name: 'revoked-fleet',
    max_nodes: 10,
    nodes_enrolled: 0,
    labels: {},
    capabilities: [],
    expires_at: '2030-01-01T00:00:00Z',
    revoked_at: '2025-01-01T00:00:00Z',
    created_at: '2024-01-01T00:00:00Z',
  },
];

const sampleContentBundle: OfflineContentBundle = {
  id: 'bundle-row-1',
  tenant_id: 'tenant-a',
  bundle_id: 'geo-threat-pack',
  version: '2026.06.08',
  sequence: 42,
  status: 'imported',
  public_key_fingerprint: 'SHA256:abc123',
  manifest_sha256: 'abc123',
  storage_path: '/var/lib/control-one/offline/geo-threat-pack.tgz',
  contents: [{ kind: 'threat-feed' }, { kind: 'parser-profile' }],
  warnings: [],
  imported_at: '2026-06-08T00:00:00Z',
};

function makeApiClientStub(): Pick<APIClient, 'listEnrollmentTokens' | 'buildBundleDownloadUrl' | 'listOfflineContentBundles' | 'importOfflineContentBundle' | 'rollbackOfflineContentBundle'> {
  return {
    listEnrollmentTokens: vi.fn().mockResolvedValue({
      data: sampleTokens,
      pagination: {
        total: 2,
        count: 2,
        limit: 50,
        offset: 0,
        nextOffset: null,
        prevOffset: null,
      },
    }),
    buildBundleDownloadUrl: vi.fn((options: { os: string; arch: string; token: string }) => {
      return `https://cp.example.com/api/v1/agent/bundle?os=${options.os}&arch=${options.arch}&token=${options.token}`;
    }),
    listOfflineContentBundles: vi.fn().mockResolvedValue({
      items: [],
      pagination: { total: 0, count: 0, limit: 25, offset: 0, nextOffset: null, prevOffset: null },
    }),
    importOfflineContentBundle: vi.fn(),
    rollbackOfflineContentBundle: vi.fn(),
  };
}

describe('OfflineBundle', () => {
  const showToastMock = vi.fn();
  let windowLocationAssign: ReturnType<typeof vi.fn>;
  let originalLocation: Location;
  let api: ReturnType<typeof makeApiClientStub>;

  beforeEach(() => {
    vi.spyOn(useToastModule, 'useToast').mockReturnValue({
      toasts: [],
      showToast: showToastMock,
      dismissToast: vi.fn(),
    });

    vi.spyOn(useTenantModule, 'useTenant').mockReturnValue({
      currentTenantId: 'tenant-a',
      tenants: [{ id: 'tenant-a', name: 'Tenant A', created_at: '2024-01-01T00:00:00Z', updated_at: '2024-01-01T00:00:00Z' }],
      currentTenant: { id: 'tenant-a', name: 'Tenant A', created_at: '2024-01-01T00:00:00Z', updated_at: '2024-01-01T00:00:00Z' },
      loading: false,
      error: null,
      setCurrentTenantId: vi.fn(),
      refresh: vi.fn().mockResolvedValue(undefined),
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
    } as any);

    api = makeApiClientStub();
    vi.spyOn(useApiClientModule, 'useApiClient').mockReturnValue(api as unknown as APIClient);

    windowLocationAssign = vi.fn();
    originalLocation = window.location;
    Object.defineProperty(window, 'location', {
      configurable: true,
      value: { assign: windowLocationAssign },
    });
  });

  afterEach(() => {
    Object.defineProperty(window, 'location', {
      configurable: true,
      value: originalLocation,
    });
    vi.restoreAllMocks();
    showToastMock.mockReset();
  });

  it('loads the enrollment token list and renders the picker', async () => {
    render(<OfflineBundle />, { wrapper: Wrapper });

    await waitFor(() => {
      expect(screen.getByRole('combobox', { name: /enrollment token/i })).toBeInTheDocument();
    });
    expect(api.listEnrollmentTokens).toHaveBeenCalledWith({ tenant_id: 'tenant-a', limit: 50, offset: 0 });
    expect(screen.getByRole('option', { name: /prod-fleet/i })).toBeInTheDocument();
    expect(screen.getByRole('option', { name: /revoked-fleet/i })).toBeDisabled();
  });

  it('waits for a tenant before loading enrollment tokens', async () => {
    vi.mocked(useTenantModule.useTenant).mockReturnValue({
      currentTenantId: null,
      tenants: [],
      currentTenant: null,
      loading: true,
      error: null,
      setCurrentTenantId: vi.fn(),
      refresh: vi.fn().mockResolvedValue(undefined),
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
    } as any);

    render(<OfflineBundle />, { wrapper: Wrapper });

    await waitFor(() => {
      expect(screen.getByRole('combobox', { name: /enrollment token/i })).toBeDisabled();
    });
    expect(api.listEnrollmentTokens).not.toHaveBeenCalled();
    expect(screen.queryByText(/tenant_id query parameter is required/i)).not.toBeInTheDocument();
  });

  it('blocks submit until a token value is available', async () => {
    render(<OfflineBundle />, { wrapper: Wrapper });

    await waitFor(() => {
      expect(screen.getByRole('combobox', { name: /enrollment token/i })).toBeInTheDocument();
    });

    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: /download bundle/i }));

    expect(screen.getByText(/enrollment token is required/i)).toBeInTheDocument();
    expect(windowLocationAssign).not.toHaveBeenCalled();
  });

  it('triggers the download with the override token when the picker omits it', async () => {
    render(<OfflineBundle />, { wrapper: Wrapper });

    await waitFor(() => {
      expect(screen.getByRole('combobox', { name: /enrollment token/i })).toBeInTheDocument();
    });

    const user = userEvent.setup();
    await user.type(screen.getByLabelText(/raw token value/i), 'cot_abc');

    await user.click(screen.getByRole('button', { name: /download bundle/i }));

    await waitFor(() => {
      expect(windowLocationAssign).toHaveBeenCalledWith(
        expect.stringContaining('token=cot_abc'),
      );
    });
  });

  it('copies the SCP command via the clipboard helper', async () => {
    const writeTextMock = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: { writeText: writeTextMock },
    });

    render(<OfflineBundle />, { wrapper: Wrapper });

    const button = await screen.findByRole('button', { name: /copy scp command/i });

    // jsdom + userEvent can be flaky when crypto.randomUUID is unavailable,
    // so prefer a synchronous click and flush microtasks manually.
    await act(async () => {
      button.click();
    });

    await waitFor(() => {
      expect(writeTextMock).toHaveBeenCalledWith(
        expect.stringContaining('controlone-bundle-linux-amd64.tar.gz'),
      );
    });
    expect(showToastMock).toHaveBeenCalledWith('SCP command copied to clipboard.', 'success');
  });

  it('re-renders the SCP snippet when the OS changes', async () => {
    render(<OfflineBundle />, { wrapper: Wrapper });

    await waitFor(() => {
      expect(screen.getByLabelText(/operating system/i)).toBeInTheDocument();
    });

    await act(async () => {
      const user = userEvent.setup();
      await user.selectOptions(screen.getByLabelText(/operating system/i), 'windows');
    });

    expect(screen.getByLabelText(/scp command template/i)).toHaveTextContent(/install-offline\.ps1/);
  });

  it('requires in-app confirmation before activating signed content', async () => {
    vi.mocked(api.listOfflineContentBundles).mockResolvedValue({
      items: [sampleContentBundle],
      pagination: { total: 1, count: 1, limit: 25, offset: 0, nextOffset: null, prevOffset: null },
    });
    vi.mocked(api.rollbackOfflineContentBundle).mockResolvedValue(sampleContentBundle);
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true);
    const user = userEvent.setup();

    render(<OfflineBundle />, { wrapper: Wrapper });

    await user.click(await screen.findByRole('button', {
      name: 'Activate offline content bundle geo-threat-pack sequence 42',
    }));

    expect(confirmSpy).not.toHaveBeenCalled();
    expect(api.rollbackOfflineContentBundle).not.toHaveBeenCalled();

    const dialog = screen.getByRole('dialog', { name: 'Activate geo-threat-pack sequence 42?' });
    expect(dialog).toHaveTextContent('version 2026.06.08');

    await user.click(within(dialog).getByRole('button', { name: 'Activate bundle' }));

    await waitFor(() => {
      expect(api.rollbackOfflineContentBundle).toHaveBeenCalledWith('tenant-a', 'geo-threat-pack', 42);
    });
    expect(showToastMock).toHaveBeenCalledWith('Offline content bundle activated.', 'success');
  });

  it('keeps failed signed-content activation visible in the modal', async () => {
    vi.mocked(api.listOfflineContentBundles).mockResolvedValue({
      items: [sampleContentBundle],
      pagination: { total: 1, count: 1, limit: 25, offset: 0, nextOffset: null, prevOffset: null },
    });
    vi.mocked(api.rollbackOfflineContentBundle).mockRejectedValueOnce(new Error('signature revoked'));
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true);
    const user = userEvent.setup();

    render(<OfflineBundle />, { wrapper: Wrapper });

    await user.click(await screen.findByRole('button', {
      name: 'Activate offline content bundle geo-threat-pack sequence 42',
    }));
    const dialog = screen.getByRole('dialog', { name: 'Activate geo-threat-pack sequence 42?' });
    await user.click(within(dialog).getByRole('button', { name: 'Activate bundle' }));

    await waitFor(() => {
      expect(api.rollbackOfflineContentBundle).toHaveBeenCalledWith('tenant-a', 'geo-threat-pack', 42);
    });
    expect(confirmSpy).not.toHaveBeenCalled();
    expect(screen.getByRole('dialog', { name: 'Activate geo-threat-pack sequence 42?' })).toBeInTheDocument();
    expect(await within(dialog).findByText('Bundle activation failed')).toBeInTheDocument();
    expect(within(dialog).getByRole('alert')).toHaveTextContent('signature revoked');
    expect(showToastMock).toHaveBeenCalledWith('signature revoked', 'error');
  });
});
