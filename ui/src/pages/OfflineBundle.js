import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useCallback, useEffect, useMemo, useState } from 'react';
import { useApiClient } from '../hooks/useApiClient';
import { useFormFeedback } from '../hooks/useFormFeedback';
import { useToast } from '../providers/ToastProvider';
// Air-gapped deployments only ship the platforms the CP has binaries for. If
// the binary directory is missing an entry the server returns 404 and the UI
// surfaces that directly — we don't probe ahead of time because this page is
// already dense enough.
const OS_OPTIONS = [
    { value: 'linux', label: 'Linux' },
    { value: 'darwin', label: 'macOS' },
    { value: 'windows', label: 'Windows' },
];
const ARCH_OPTIONS = [
    { value: 'amd64', label: 'amd64 (x86_64)' },
    { value: 'arm64', label: 'arm64 (aarch64)' },
];
export function OfflineBundle() {
    const api = useApiClient();
    const { showToast } = useToast();
    const feedback = useFormFeedback();
    const [tokens, setTokens] = useState([]);
    const [tokensLoading, setTokensLoading] = useState(true);
    const [tokensError, setTokensError] = useState(null);
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
        }
        catch (err) {
            const message = err instanceof Error ? err.message : 'Failed to load enrollment tokens.';
            setTokensError(message);
        }
        finally {
            setTokensLoading(false);
        }
    }, [api]);
    useEffect(() => {
        void loadTokens();
    }, [loadTokens]);
    const selectedToken = useMemo(() => {
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
            `ssh operator@isolated-host.example "tar -xzf /tmp/${bundleFilename} -C /tmp && sudo /tmp/controlone-bundle/install-offline.${os === 'windows' ? 'ps1' : 'sh'}"`,
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
        }
        catch (err) {
            const message = err instanceof Error ? err.message : 'Clipboard copy failed.';
            showToast(message, 'error');
        }
    };
    const handleSubmit = (event) => {
        event.preventDefault();
        feedback.reset();
        if (!effectiveToken) {
            feedback.showError('An enrollment token is required. Either enter the raw token value or create a new token so the wizard can download it.');
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
    return (_jsxs("section", { className: "offline-bundle-page", children: [_jsx("header", { className: "page-header", children: _jsxs("div", { children: [_jsx("p", { className: "eyebrow", children: "Air-gapped onboarding" }), _jsx("h2", { children: "Offline agent bundle" }), _jsx("p", { className: "subtitle", children: "Download an install tarball containing the agent binary, its signature, the CA cert, and the offline install script. SCP it into your isolated network to enroll nodes without internet access." })] }) }), _jsxs("form", { className: "panel offline-bundle-form", onSubmit: handleSubmit, children: [_jsx("h3", { children: "Build a bundle" }), _jsxs("label", { htmlFor: "offline-bundle-token", children: ["Enrollment token", _jsxs("select", { id: "offline-bundle-token", value: selectedTokenId, onChange: (event) => {
                                    setSelectedTokenId(event.target.value);
                                    setTokenOverride('');
                                }, disabled: tokensLoading || tokens.length === 0, children: [_jsx("option", { value: "", children: tokensLoading ? 'Loading tokens…' : 'Select a token' }), tokens.map((token) => (_jsxs("option", { value: token.id, disabled: Boolean(token.revoked_at), children: [token.name, token.revoked_at ? ' (revoked)' : ''] }, token.id)))] })] }), tokensError ? _jsx("p", { className: "form-error", children: tokensError }) : null, !tokensLoading && tokens.length === 0 ? (_jsx("p", { className: "muted", children: "No enrollment tokens yet. Create one in Nodes \u2192 Enrollment tokens before building a bundle." })) : null, _jsxs("label", { htmlFor: "offline-bundle-token-override", children: ["Raw token value", _jsx("input", { id: "offline-bundle-token-override", type: "text", value: tokenOverride, onChange: (event) => setTokenOverride(event.target.value), placeholder: selectedToken?.token ? 'Using value from picker' : 'cot_…', autoComplete: "off" }), _jsx("small", { className: "muted", children: "Paste the raw token if the picker does not carry it \u2014 the list endpoint only returns the secret portion at creation time." })] }), _jsxs("div", { className: "grid two-col", children: [_jsxs("label", { htmlFor: "offline-bundle-os", children: ["Operating system", _jsx("select", { id: "offline-bundle-os", value: os, onChange: (event) => setOs(event.target.value), children: OS_OPTIONS.map((option) => (_jsx("option", { value: option.value, children: option.label }, option.value))) })] }), _jsxs("label", { htmlFor: "offline-bundle-arch", children: ["Architecture", _jsx("select", { id: "offline-bundle-arch", value: arch, onChange: (event) => setArch(event.target.value), children: ARCH_OPTIONS.map((option) => (_jsx("option", { value: option.value, children: option.label }, option.value))) })] })] }), feedback.error ? _jsx("p", { className: "form-error", children: feedback.error }) : null, feedback.success ? _jsx("p", { className: "form-success", children: feedback.success }) : null, _jsxs("div", { className: "form-actions", children: [_jsx("button", { type: "submit", className: "primary-button", disabled: tokensLoading, children: "Download bundle" }), _jsx("button", { type: "button", className: "ghost-button", onClick: loadTokens, disabled: tokensLoading, children: tokensLoading ? 'Refreshing…' : 'Reload tokens' })] })] }), _jsxs("article", { className: "panel scp-instructions", children: [_jsx("h3", { children: "Copy into your isolated network" }), _jsxs("p", { className: "muted", children: ["After the download completes, copy the tarball into the target host and run the bundled installer. The commands below assume Linux/macOS \u2014 on Windows run the ", _jsx("code", { children: "install-offline.ps1" }), " script instead."] }), _jsx("pre", { className: "code-block", "aria-label": "SCP command template", children: scpCommand }), _jsx("div", { className: "detail-actions", children: _jsx("button", { type: "button", className: "ghost-button", onClick: handleCopyScp, children: "Copy SCP command" }) })] })] }));
}
