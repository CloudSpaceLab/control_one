import { FormEvent, useCallback, useEffect, useMemo, useState } from 'react';
import { EnrollmentToken } from '../lib/api';
import { useApiClient } from '../hooks/useApiClient';
import { SectionHeader } from '../components/kit';
import { useFormFeedback } from '../hooks/useFormFeedback';
import { useToast } from '../providers/ToastProvider';

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

  const [tokens, setTokens] = useState<EnrollmentToken[]>([]);
  const [tokensLoading, setTokensLoading] = useState(true);
  const [tokensError, setTokensError] = useState<string | null>(null);

  const [selectedTokenId, setSelectedTokenId] = useState('');
  const [os, setOs] = useState(OS_OPTIONS[0].value);
  const [arch, setArch] = useState(ARCH_OPTIONS[0].value);
  const [tokenOverride, setTokenOverride] = useState('');

  const loadTokens = useCallback(async () => {
    setTokensLoading(true);
    setTokensError(null);
    try {
      const response = await api.listEnrollmentTokens({ limit: 50, offset: 0 });
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
  }, [api]);

  useEffect(() => {
    void loadTokens();
  }, [loadTokens]);

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

  return (
    <div className="flex flex-col gap-5 offline-bundle-page">
      <SectionHeader
        eyebrow="AUTOMATION · OFFLINE BUNDLE"
        title="Offline agent bundle"
        description="Download an install tarball containing the agent binary, its signature, the CA cert, and the offline install script. SCP it into your isolated network to enroll nodes without internet access."
      />

      <form className="panel offline-bundle-form" onSubmit={handleSubmit}>
        <h3>Build a bundle</h3>

        <label htmlFor="offline-bundle-token">
          Enrollment token
          <select
            id="offline-bundle-token"
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
        </label>
        {tokensError ? <p className="form-error">{tokensError}</p> : null}
        {!tokensLoading && tokens.length === 0 ? (
          <p className="muted">
            No enrollment tokens yet. Create one in Nodes → Enrollment tokens before building a bundle.
          </p>
        ) : null}

        <label htmlFor="offline-bundle-token-override">
          Raw token value
          <input
            id="offline-bundle-token-override"
            type="text"
            value={tokenOverride}
            onChange={(event) => setTokenOverride(event.target.value)}
            placeholder={selectedToken?.token ? 'Using value from picker' : 'cot_…'}
            autoComplete="off"
          />
          <small className="muted">
            Paste the raw token if the picker does not carry it — the list endpoint only returns the secret
            portion at creation time.
          </small>
        </label>

        <div className="grid two-col">
          <label htmlFor="offline-bundle-os">
            Operating system
            <select id="offline-bundle-os" value={os} onChange={(event) => setOs(event.target.value)}>
              {OS_OPTIONS.map((option) => (
                <option key={option.value} value={option.value}>
                  {option.label}
                </option>
              ))}
            </select>
          </label>
          <label htmlFor="offline-bundle-arch">
            Architecture
            <select id="offline-bundle-arch" value={arch} onChange={(event) => setArch(event.target.value)}>
              {ARCH_OPTIONS.map((option) => (
                <option key={option.value} value={option.value}>
                  {option.label}
                </option>
              ))}
            </select>
          </label>
        </div>

        {feedback.error ? <p className="form-error">{feedback.error}</p> : null}
        {feedback.success ? <p className="form-success">{feedback.success}</p> : null}

        <div className="form-actions">
          <button type="submit" className="primary-button" disabled={tokensLoading}>
            Download bundle
          </button>
          <button type="button" className="ghost-button" onClick={loadTokens} disabled={tokensLoading}>
            {tokensLoading ? 'Refreshing…' : 'Reload tokens'}
          </button>
        </div>
      </form>

      <article className="panel scp-instructions">
        <h3>Copy into your isolated network</h3>
        <p className="muted">
          After the download completes, copy the tarball into the target host and run the bundled installer. The
          commands below assume Linux/macOS — on Windows run the <code>install-offline.ps1</code> script instead.
        </p>
        <pre className="code-block" aria-label="SCP command template">{scpCommand}</pre>
        <div className="detail-actions">
          <button type="button" className="ghost-button" onClick={handleCopyScp}>
            Copy SCP command
          </button>
        </div>
      </article>
    </div>
  );
}
