import { FormEvent, useCallback, useEffect, useMemo, useState } from 'react';
import { EnrollmentToken, OfflineContentBundle } from '../lib/api';
import { useApiClient } from '../hooks/useApiClient';
import { SectionHeader, Panel } from '../components/kit';
import { Button } from '@/components/ui/button';
import { Input } from '../components/ui/input';
import { Label } from '../components/ui/label';
import { useFormFeedback } from '../hooks/useFormFeedback';
import { useToast } from '../providers/ToastProvider';
import { useTenant } from '../providers/TenantProvider';

// Air-gapped deployments only ship the platforms the CP has binaries for. If
// the binary directory is missing an entry the server returns 404 and the UI
// surfaces that directly — we don't probe ahead of time because this page is
// already dense enough.
const OS_OPTIONS: Array<{ value: string; label: string }> = [
  { value: 'linux', label: 'Linux' },
  { value: 'darwin', label: 'macOS' },
  { value: 'windows', label: 'Windows' },
];

const ARCH_OPTIONS: Array<{ value: string; label: string }> = [
  { value: 'amd64', label: 'amd64 (x86_64)' },
  { value: 'arm64', label: 'arm64 (aarch64)' },
];

export function OfflineBundle(): JSX.Element {
  const api = useApiClient();
  const { showToast } = useToast();
  const feedback = useFormFeedback();
  const { currentTenantId } = useTenant();

  const [tokens, setTokens] = useState<EnrollmentToken[]>([]);
  const [tokensLoading, setTokensLoading] = useState(true);
  const [tokensError, setTokensError] = useState<string | null>(null);

  const [selectedTokenId, setSelectedTokenId] = useState('');
  const [os, setOs] = useState(OS_OPTIONS[0].value);
  const [arch, setArch] = useState(ARCH_OPTIONS[0].value);
  const [tokenOverride, setTokenOverride] = useState('');
  const [contentBundles, setContentBundles] = useState<OfflineContentBundle[]>([]);
  const [contentLoading, setContentLoading] = useState(false);
  const [contentError, setContentError] = useState<string | null>(null);
  const [contentFile, setContentFile] = useState<File | null>(null);
  const [contentWorking, setContentWorking] = useState(false);

  const loadTokens = useCallback(async () => {
    setTokensLoading(true);
    setTokensError(null);
    try {
      const response = await api.listEnrollmentTokens({ tenant_id: currentTenantId ?? undefined, limit: 50, offset: 0 });
      setTokens(response.data);
      // Pre-select the first non-revoked token so the happy path is one-click.
      const firstUsable = response.data.find((token) => !token.revoked_at);
      if (firstUsable) {
        setSelectedTokenId((current) => current || firstUsable.id);
      }
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to load enrollment tokens.';
      setTokensError(message);
    } finally {
      setTokensLoading(false);
    }
  }, [api, currentTenantId]);

  useEffect(() => {
    void loadTokens();
  }, [loadTokens]);

  const loadContentBundles = useCallback(async () => {
    if (!currentTenantId) {
      setContentBundles([]);
      return;
    }
    setContentLoading(true);
    setContentError(null);
    try {
      const response = await api.listOfflineContentBundles({ tenantId: currentTenantId, limit: 25, offset: 0 });
      setContentBundles(response.items ?? []);
    } catch (err) {
      setContentError(err instanceof Error ? err.message : 'Failed to load offline content bundles.');
    } finally {
      setContentLoading(false);
    }
  }, [api, currentTenantId]);

  useEffect(() => {
    void loadContentBundles();
  }, [loadContentBundles]);

  const selectedToken = useMemo<EnrollmentToken | undefined>(() => {
    return tokens.find((token) => token.id === selectedTokenId);
  }, [tokens, selectedTokenId]);

  // The list endpoint returns token.token only on creation, so for downloads
  // the operator must paste the raw token value. We expose an override input
  // to make that possible while still keeping the picker for discoverability.
  const effectiveToken = tokenOverride.trim() || selectedToken?.token || '';

  const bundleFilename = `controlone-bundle-${os}-${arch}.tar.gz`;
  const scpCommand = useMemo(() => {
    return [
      `scp ${bundleFilename} operator@isolated-host.example:/tmp/`,
      `ssh operator@isolated-host.example "tar -xzf /tmp/${bundleFilename} -C /tmp && sudo /tmp/controlone-bundle/install-offline.${
        os === 'windows' ? 'ps1' : 'sh'
      }"`,
    ].join('\n');
  }, [bundleFilename, os]);

  const handleCopyScp = async () => {
    if (!navigator.clipboard) {
      showToast('Clipboard API not available in this browser.', 'error');
      return;
    }
    try {
      await navigator.clipboard.writeText(scpCommand);
      showToast('SCP command copied to clipboard.', 'success');
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Clipboard copy failed.';
      showToast(message, 'error');
    }
  };

  const handleSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    feedback.reset();

    if (!effectiveToken) {
      feedback.showError(
        'An enrollment token is required. Either enter the raw token value or create a new token so the wizard can download it.',
      );
      return;
    }
    if (!os || !arch) {
      feedback.showError('Select both an OS and an architecture.');
      return;
    }

    const url = api.buildBundleDownloadUrl({ os, arch, token: effectiveToken });
    feedback.showSuccess(`Requesting ${bundleFilename}…`);
    // Browser handles the streaming download — we intentionally do not fetch()
    // the response into memory because the bundle can be tens of megabytes.
    window.location.assign(url);
  };

  const handleContentImport = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!currentTenantId) {
      showToast('Select a tenant before importing content.', 'error');
      return;
    }
    if (!contentFile) {
      showToast('Choose a signed content bundle archive first.', 'error');
      return;
    }
    setContentWorking(true);
    setContentError(null);
    try {
      await api.importOfflineContentBundle(currentTenantId, contentFile);
      setContentFile(null);
      showToast('Offline content bundle imported.', 'success');
      await loadContentBundles();
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Import failed.';
      setContentError(message);
      showToast(message, 'error');
    } finally {
      setContentWorking(false);
    }
  };

  const handleContentRollback = async (bundle: OfflineContentBundle) => {
    if (!currentTenantId) return;
    const confirmed = window.confirm(`Activate ${bundle.bundle_id} sequence ${bundle.sequence}?`);
    if (!confirmed) return;
    setContentWorking(true);
    setContentError(null);
    try {
      await api.rollbackOfflineContentBundle(currentTenantId, bundle.bundle_id, bundle.sequence);
      showToast('Offline content bundle activated.', 'success');
      await loadContentBundles();
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Rollback failed.';
      setContentError(message);
      showToast(message, 'error');
    } finally {
      setContentWorking(false);
    }
  };

  const selectClass = 'h-9 w-full rounded-md border border-border-subtle bg-surface px-3 text-sm text-foreground focus-visible:outline-none focus-visible:border-border-strong focus-visible:ring-2 focus-visible:ring-brand-500/30 disabled:cursor-not-allowed disabled:opacity-50';

  return (
    <div className="flex flex-col gap-5">
      <SectionHeader
        eyebrow="AUTOMATION · OFFLINE BUNDLE"
        title="Offline agent bundle"
        description="Download an install tarball containing the agent binary, its signature, the CA cert, and the offline install script. SCP it into your isolated network to enroll nodes without internet access."
      />

      <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
        {/* LEFT: Build bundle */}
        <Panel padding="md" eyebrow="BUILD BUNDLE" title="Configure download" toneAccent="brand">
          <form onSubmit={handleSubmit} className="flex flex-col gap-3">
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="offline-bundle-token">Enrollment token</Label>
              <select
                id="offline-bundle-token"
                className={selectClass}
                value={selectedTokenId}
                onChange={(event) => {
                  setSelectedTokenId(event.target.value);
                  setTokenOverride('');
                }}
                disabled={tokensLoading || tokens.length === 0}
              >
                <option value="">{tokensLoading ? 'Loading tokens…' : 'Select a token'}</option>
                {tokens.map((token) => (
                  <option key={token.id} value={token.id} disabled={Boolean(token.revoked_at)}>
                    {token.name}
                    {token.revoked_at ? ' (revoked)' : ''}
                  </option>
                ))}
              </select>
            </div>

            {tokensError ? (
              <p className="text-sm text-state-critical" role="alert">{tokensError}</p>
            ) : null}
            {!tokensLoading && tokens.length === 0 ? (
              <p className="text-sm text-text-muted">
                No enrollment tokens yet. Create one in Nodes → Enrollment tokens before building a bundle.
              </p>
            ) : null}

            <div className="flex flex-col gap-1.5">
              <Label htmlFor="offline-bundle-token-override">Raw token value</Label>
              <Input
                id="offline-bundle-token-override"
                type="text"
                value={tokenOverride}
                onChange={(event) => setTokenOverride(event.target.value)}
                placeholder={selectedToken?.token ? 'Using value from picker' : 'cot_…'}
                autoComplete="off"
              />
              <p className="text-xs text-text-muted">
                Paste the raw token if the picker does not carry it — the list endpoint only returns the secret
                portion at creation time.
              </p>
            </div>

            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="offline-bundle-os">Operating system</Label>
                <select
                  id="offline-bundle-os"
                  className={selectClass}
                  value={os}
                  onChange={(event) => setOs(event.target.value)}
                >
                  {OS_OPTIONS.map((option) => (
                    <option key={option.value} value={option.value}>
                      {option.label}
                    </option>
                  ))}
                </select>
              </div>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="offline-bundle-arch">Architecture</Label>
                <select
                  id="offline-bundle-arch"
                  className={selectClass}
                  value={arch}
                  onChange={(event) => setArch(event.target.value)}
                >
                  {ARCH_OPTIONS.map((option) => (
                    <option key={option.value} value={option.value}>
                      {option.label}
                    </option>
                  ))}
                </select>
              </div>
            </div>

            {feedback.error ? (
              <p className="text-sm text-state-critical" role="alert">{feedback.error}</p>
            ) : null}
            {feedback.success ? (
              <p className="text-sm text-state-healthy" role="status">{feedback.success}</p>
            ) : null}

            <div className="flex items-center gap-2 pt-2">
              <Button type="submit" variant="primary" disabled={tokensLoading}>
                Download bundle
              </Button>
              <Button
                type="button"
                variant="ghost"
                onClick={loadTokens}
                disabled={tokensLoading}
              >
                {tokensLoading ? 'Refreshing…' : 'Reload tokens'}
              </Button>
            </div>
          </form>
        </Panel>

        {/* RIGHT: SCP instructions */}
        <Panel padding="md" eyebrow="COPY TO ISOLATED NETWORK" title="SCP commands">
          <p className="text-sm text-text-secondary">
            After the download completes, copy the tarball into the target host and run the bundled
            installer. The commands below assume Linux/macOS — on Windows run the{' '}
            <code className="font-mono text-xs text-text-secondary">install-offline.ps1</code> script instead.
          </p>
          <pre
            className="rounded-md border border-border-subtle bg-surface p-4 font-mono text-xs text-foreground overflow-x-auto whitespace-pre-wrap"
            aria-label="SCP command template"
          >
            {scpCommand}
          </pre>
          <div className="flex items-center gap-2 pt-2">
            <Button type="button" variant="ghost" onClick={handleCopyScp}>
              Copy SCP command
            </Button>
          </div>
        </Panel>
      </div>

      <Panel
        padding="md"
        eyebrow="SIGNED CONTENT BUNDLES"
        title="Geo, threat, parser, and adapter content"
        toneAccent="brand"
        actions={
          <Button type="button" variant="ghost" size="sm" onClick={() => void loadContentBundles()} disabled={contentLoading}>
            {contentLoading ? 'Refreshing...' : 'Refresh'}
          </Button>
        }
      >
        <form onSubmit={handleContentImport} className="grid grid-cols-1 gap-3 lg:grid-cols-[1fr_auto]">
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="offline-content-file">Signed archive</Label>
            <Input
              id="offline-content-file"
              type="file"
              accept=".tar,.tgz,.gz,.zip,application/gzip,application/x-tar,application/zip"
              onChange={(event) => setContentFile(event.target.files?.[0] ?? null)}
            />
            <p className="text-xs text-text-muted">
              This is separate from the agent install bundle: it imports signed geo, threat feed,
              parser profile, rule, and webserver adapter content for air-gapped scoring.
            </p>
          </div>
          <div className="flex items-end">
            <Button type="submit" variant="primary" disabled={contentWorking || !currentTenantId}>
              {contentWorking ? 'Working...' : 'Import content'}
            </Button>
          </div>
        </form>

        {contentError ? <p className="text-sm text-state-critical">{contentError}</p> : null}

        <div className="overflow-x-auto rounded-lg border border-border-subtle">
          <table className="w-full text-sm">
            <thead className="bg-surface-2 text-left text-xs uppercase tracking-wide text-text-secondary">
              <tr>
                <th className="px-3 py-2">Bundle</th>
                <th className="px-3 py-2">Version</th>
                <th className="px-3 py-2">Sequence</th>
                <th className="px-3 py-2">Status</th>
                <th className="px-3 py-2">Contents</th>
                <th className="px-3 py-2">Imported</th>
                <th className="px-3 py-2">Actions</th>
              </tr>
            </thead>
            <tbody>
              {contentLoading ? (
                <tr>
                  <td className="px-3 py-4 text-text-muted" colSpan={7}>Loading content bundles...</td>
                </tr>
              ) : contentBundles.length === 0 ? (
                <tr>
                  <td className="px-3 py-4 text-text-muted" colSpan={7}>No signed content bundles have been imported.</td>
                </tr>
              ) : (
                contentBundles.map((bundle) => (
                  <tr key={`${bundle.bundle_id}:${bundle.sequence}:${bundle.imported_at}`} className="border-t border-border-subtle">
                    <td className="px-3 py-2 font-medium">{bundle.bundle_id}</td>
                    <td className="px-3 py-2">{bundle.version}</td>
                    <td className="px-3 py-2 font-mono text-xs">{bundle.sequence}</td>
                    <td className="px-3 py-2">{bundle.status}</td>
                    <td className="px-3 py-2 text-text-secondary">{bundle.contents?.length ?? 0} artifacts</td>
                    <td className="px-3 py-2 text-text-secondary">{formatDateTime(bundle.imported_at)}</td>
                    <td className="px-3 py-2">
                      <Button
                        type="button"
                        variant="ghost"
                        size="sm"
                        onClick={() => void handleContentRollback(bundle)}
                        disabled={contentWorking}
                      >
                        Activate
                      </Button>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </Panel>
    </div>
  );
}

function formatDateTime(value?: string): string {
  if (!value) return 'unknown';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString();
}
