import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, act, within, fireEvent } from '@testing-library/react';
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

function makeApiClientStub(): Pick<APIClient, 'listEnrollmentTokens' | 'buildBundleDownloadUrl' | 'downloadAgentBundle' | 'listOfflineContentBundles' | 'importOfflineContentBundle' | 'rollbackOfflineContentBundle'> {
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
    downloadAgentBundle: vi.fn().mockResolvedValue({ blob: new Blob(['fixture']), filename: 'controlone-bundle-linux-amd64.tar.gz' }),
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
    Object.defineProperty(URL, 'createObjectURL', { configurable: true, value: vi.fn().mockReturnValue('blob:offline-fixture') });
    Object.defineProperty(URL, 'revokeObjectURL', { configurable: true, value: vi.fn() });
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {});

    windowLocationAssign = vi.fn();
    originalLocation = window.location;
    Object.defineProperty(window, 'location', {
      configurable: true,
      value: { assign: windowLocationAssign },
    });
  });

  afterEach(() => {
    vi.useRealTimers();
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

  it('labels and disables unusable tokens and selects a usable token instead', async () => {
    vi.mocked(api.listEnrollmentTokens).mockResolvedValue({ data: [
      { ...sampleTokens[0], id: 'expired', name: 'old-install', expires_at: '2020-01-01T00:00:00Z' },
      { ...sampleTokens[0], id: 'used', name: 'used-install', max_nodes: 1, nodes_enrolled: 1 },
      { ...sampleTokens[0], id: 'bad-expiry', name: 'unknown-expiry', expires_at: '' },
      { ...sampleTokens[0], id: 'bad-usage', name: 'unknown-usage', nodes_enrolled: -1 },
      { ...sampleTokens[0], id: 'unlimited', name: 'unlimited-install', max_nodes: 0, nodes_enrolled: 500 },
    ], pagination: { total: 5, count: 5, limit: 50, offset: 0, nextOffset: null, prevOffset: null } });
    render(<OfflineBundle />, { wrapper: Wrapper });
    await waitFor(() => expect(screen.getByLabelText(/enrollment token/i)).toHaveValue('unlimited'));
    for (const name of ['old-install (expired)', 'used-install (fully used)', 'unknown-expiry (expiry unavailable)', 'unknown-usage (usage unavailable)']) {
      expect(screen.getByRole('option', { name })).toBeDisabled();
    }
    expect(screen.getByRole('option', { name: 'unlimited-install' })).toBeEnabled();
  });

  it('shows the empty usable-list state without selecting an expired token', async () => {
    vi.mocked(api.listEnrollmentTokens).mockResolvedValue({ data: [
      { ...sampleTokens[0], expires_at: '2020-01-01T00:00:00Z' },
    ], pagination: { total: 1, count: 1, limit: 50, offset: 0, nextOffset: null, prevOffset: null } });
    render(<OfflineBundle />, { wrapper: Wrapper });
    expect(await screen.findByText(/No usable tokens in this list/)).toBeInTheDocument();
    expect(screen.getByLabelText(/enrollment token/i)).toHaveValue('');
  });

  it('marks an already selected token expired while the page remains open', async () => {
    vi.useFakeTimers();
    const now = new Date('2026-10-28T10:00:00Z');
    vi.setSystemTime(now);
    vi.mocked(api.listEnrollmentTokens).mockResolvedValue({ data: [
      { ...sampleTokens[0], expires_at: new Date(now.getTime() + 1000).toISOString() },
    ], pagination: { total: 1, count: 1, limit: 50, offset: 0, nextOffset: null, prevOffset: null } });
    await act(async () => { render(<OfflineBundle />, { wrapper: Wrapper }); });
    expect(screen.getByLabelText(/enrollment token/i)).toHaveValue('tok-1');
    await act(async () => { vi.advanceTimersByTime(1001); });
    expect(screen.getByRole('option', { name: 'prod-fleet (expired)' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Download bundle' })).toBeDisabled();
    expect(screen.getByRole('alert')).toHaveTextContent('selected token is expired');
  });

  it('rechecks expiry when submitting even before the UI expiry timer runs', async () => {
    const expires = Date.now() + 60_000;
    vi.mocked(api.listEnrollmentTokens).mockResolvedValue({ data: [
      { ...sampleTokens[0], expires_at: new Date(expires).toISOString() },
    ], pagination: { total: 1, count: 1, limit: 50, offset: 0, nextOffset: null, prevOffset: null } });
    render(<OfflineBundle />, { wrapper: Wrapper });
    await waitFor(() => expect(screen.getByLabelText(/enrollment token/i)).toHaveValue('tok-1'));
    await userEvent.setup().type(screen.getByLabelText(/raw token value/i), 'cot_test_fixture');
    vi.spyOn(Date, 'now').mockReturnValue(expires + 1);
    fireEvent.submit(screen.getByRole('button', { name: 'Download bundle' }).closest('form')!);
    expect(api.buildBundleDownloadUrl).not.toHaveBeenCalled();
    expect(api.downloadAgentBundle).not.toHaveBeenCalled();
    expect(windowLocationAssign).not.toHaveBeenCalled();
    expect(screen.getByText(/selected enrollment token is expired/)).toBeInTheDocument();
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

    const download = screen.getByRole('button', { name: /download bundle/i });
    expect(download).toBeDisabled();
    fireEvent.submit(download.closest('form')!);

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
      expect(api.downloadAgentBundle).toHaveBeenCalledWith({ os: 'linux', arch: 'amd64', token: 'cot_abc' });
    });
    expect(URL.createObjectURL).toHaveBeenCalledWith(expect.any(Blob));
    expect(HTMLAnchorElement.prototype.click).toHaveBeenCalledTimes(1);
    expect(windowLocationAssign).not.toHaveBeenCalled();
  });

  it('shows a server download rejection without claiming success', async () => {
    vi.mocked(api.downloadAgentBundle).mockRejectedValue(new Error('enrollment token has reached its node limit'));
    render(<OfflineBundle />, { wrapper: Wrapper });
    await waitFor(() => expect(screen.getByLabelText(/enrollment token/i)).toHaveValue('tok-1'));
    const user = userEvent.setup();
    await user.type(screen.getByLabelText(/raw token value/i), 'cot_test_fixture');
    await user.click(screen.getByRole('button', { name: 'Download bundle' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('reached its node limit');
    expect(URL.createObjectURL).not.toHaveBeenCalled();
    expect(windowLocationAssign).not.toHaveBeenCalled();
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
