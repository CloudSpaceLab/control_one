import { useMutation, useQuery } from '@tanstack/react-query';
import {
  ArrowRight,
  CheckCircle2,
  Clipboard,
  ClipboardCheck,
  Globe,
  Key,
  Lock,
  Layers,
  Monitor,
  Network,
  Package,
  Plus,
  Server,
  ShieldCheck,
  Sparkles,
  Terminal,
  Wrench,
  XCircle,
} from 'lucide-react';
import { useEffect, useState, type ReactNode } from 'react';
import { Link } from 'react-router-dom';
import { Button } from '../components/ui/button';
import { Input } from '../components/ui/input';
import { Label } from '../components/ui/label';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '../components/ui/tabs';
import {
  EmptyState,
  Eyebrow,
  FileUploadButton,
  Panel,
  SectionHeader,
  SelectField,
  StatusTag,
  type StateTone,
} from '../components/kit';
import { useApiClient } from '../hooks/useApiClient';
import { useTenant } from '../providers/TenantProvider';
import { OnboardAIPanel } from '../components/settings/OnboardAIPanel';
import { toast } from 'sonner';
import type {
  ConnectionProbe,
  IpEnrichment,
  OnboardingAuth,
  OnboardingProtocol,
  TestConnectionPayload,
  TestConnectionResult,
  EnrollmentToken,
} from '../lib/api';

const PROTO_HINT: Record<OnboardingProtocol, string> = {
  ssh: 'Linux or macOS with SSH enabled. Default port 22.',
  winrm: 'Windows machine with WinRM enabled. Default port 5985 (HTTP) / 5986 (HTTPS).',
  rdp: 'TCP reachability check only. Pair with a WinRM credential to enrol the machine.',
};

type Scenario = 'local' | 'remote' | 'bulk' | 'offline' | 'repair';
type InstallOS = 'windows' | 'macos' | 'linux';

function detectOS(): InstallOS {
  const ua = navigator.userAgent.toLowerCase();
  const plat = navigator.platform.toLowerCase();
  if (ua.includes('win') || plat.includes('win')) return 'windows';
  if (ua.includes('mac') || plat.includes('mac')) return 'macos';
  return 'linux';
}

function installPlatform(os: InstallOS): string {
  return os === 'macos' ? 'darwin' : os;
}

function buildInstallScriptUrl(origin: string, token: string, os: InstallOS): string {
  const search = new URLSearchParams({
    token,
    platform: installPlatform(os),
  });
  return `${origin}/api/v1/agent/install-script?${search.toString()}`;
}

function buildInstallCommand(origin: string, token: string, os: InstallOS): string {
  const url = buildInstallScriptUrl(origin, token, os);
  if (os === 'windows') {
    return `powershell.exe -NoProfile -ExecutionPolicy Bypass -Command "Invoke-RestMethod -Uri '${url}' | Invoke-Expression"`;
  }
  return `curl -fsSL '${url}' | sudo bash`;
}

export function Onboard(): JSX.Element {
  const client = useApiClient();
  const { currentTenantId, tenants, refresh: refreshTenants } = useTenant();
  const [scenario, setScenario] = useState<Scenario | null>(null);

  const [protocol, setProtocol] = useState<OnboardingProtocol>('ssh');
  const [host, setHost] = useState('');
  const [port, setPort] = useState<string>('');
  const [username, setUsername] = useState('');
  const [auth, setAuth] = useState<OnboardingAuth>('password');
  const [password, setPassword] = useState('');
  const [privateKey, setPrivateKey] = useState('');
  const [passphrase, setPassphrase] = useState('');
  const [https, setHttps] = useState(true);
  const [skipVerify, setSkipVerify] = useState(false);
  const [result, setResult] = useState<TestConnectionResult | null>(null);

  // Step 2 state — only relevant after a successful test.
  const [groupName, setGroupName] = useState('');
  const [enrolTenantId, setEnrolTenantId] = useState<string | null>(currentTenantId ?? null);
  const [groupTouched, setGroupTouched] = useState(false);
  const [jobId, setJobId] = useState<string | null>(null);
  const [creatingTenant, setCreatingTenant] = useState(false);
  const [newTenantName, setNewTenantName] = useState('');

  // Command install copy state
  const [copied, setCopied] = useState(false);
  const detectedOS = detectOS();
  const [installOS, setInstallOS] = useState<InstallOS>(detectedOS);
  const [installToken, setInstallToken] = useState<EnrollmentToken | null>(null);
  const [installError, setInstallError] = useState<string | null>(null);
  const installOrigin = window.location.origin;
  const installCommand = installToken?.token
    ? buildInstallCommand(installOrigin, installToken.token, installOS)
    : buildInstallCommand(installOrigin, '<generate-token>', installOS);

  const resetInstallCommand = () => {
    setInstallToken(null);
    setInstallError(null);
    setCopied(false);
  };

  const selectEnrollmentTenant = (tenantId: string | null) => {
    if (tenantId !== enrolTenantId) resetInstallCommand();
    setEnrolTenantId(tenantId);
  };

  useEffect(() => {
    if (!enrolTenantId && currentTenantId) setEnrolTenantId(currentTenantId);
  }, [currentTenantId, enrolTenantId]);

  useEffect(() => {
    setResult(null);
    setJobId(null);
  }, [scenario]);

  // Auto country lookup once we know the address is reachable. Uses the
  // existing ipintel pipeline (akyriako/ipquery + AbuseIPDB fallback).
  const enrichQ = useQuery<IpEnrichment | null>({
    queryKey: ['onboard.enrich', host, result?.ok, enrolTenantId ?? currentTenantId],
    queryFn: async () => {
      try {
        return await client.enrichIp(host, enrolTenantId ?? currentTenantId);
      } catch {
        return null;
      }
    },
    enabled: !!result?.ok && !!(enrolTenantId ?? currentTenantId) && /^[\d.:a-fA-F]+$/.test(host),
    staleTime: 5 * 60_000,
  });

  // Suggest "Group {CC}" when probe + enrichment land. User can override.
  useEffect(() => {
    if (groupTouched) return;
    const cc = enrichQ.data?.geo?.country_code;
    if (cc) {
      setGroupName(`Group ${cc.toUpperCase()}`);
    } else if (result?.ok && !groupName) {
      setGroupName('Group');
    }
  }, [enrichQ.data, result?.ok, groupTouched, groupName]);

  const test = useMutation({
    mutationFn: (payload: TestConnectionPayload) => client.testServerConnection(payload),
    onSuccess: (r) => {
      setResult(r);
      setJobId(null);
      if (r.ok) toast.success('Connection succeeded');
      else toast.error(r.error || 'Connection failed');
    },
    onError: (err) => {
      const msg = err instanceof Error ? err.message : 'Test failed';
      setResult({ ok: false, error: msg });
      toast.error(msg);
    },
  });

  const enrol = useMutation({
    mutationFn: async () => {
      if (!enrolTenantId) throw new Error('Select a tenant before enrolling');
      const trimmedGroup = groupName.trim() || 'Group';

      // Step 1: create a single-use enrollment token scoped to the group.
      const token = await client.createEnrollmentToken({
        name: `onboard-${host}-${Date.now()}`,
        tenant_id: enrolTenantId,
        max_nodes: 1,
        ttl: '24h',
        labels: { group: trimmedGroup, onboard_source: 'wizard' },
        capabilities: ['agent.run'],
      });
      if (!token.token) throw new Error('controlplane returned no raw enrolment token');

      // Step 2: dispatch the existing fleet enroller. Reuses the same
      // queued-job + per-target result tracking the bulk enrol page already
      // has, so the wizard never duplicates that machinery.
      const payload = {
        targets: [
          {
            host: host.trim(),
            port: port ? Number(port) : undefined,
            user: username.trim() || undefined,
          },
        ],
        ssh_user: username.trim() || undefined,
        ssh_key: auth === 'private_key' && privateKey ? toBase64(privateKey) : undefined,
        ssh_password: auth === 'password' && password ? password : undefined,
        token: token.token,
        tenant_id: enrolTenantId,
        labels: { group: trimmedGroup },
        parallel: 1,
      };
      const job = await client.startFleetEnroll(payload);
      return job;
    },
    onSuccess: (r) => {
      setJobId(r.job_id);
      toast.success(`Enrolment started: ${r.job_id.slice(0, 8)}…`);
    },
    onError: (err) => toast.error(err instanceof Error ? err.message : 'Enrolment failed'),
  });

  const generateInstallToken = useMutation({
    mutationFn: async () => {
      const tenantIdForToken = enrolTenantId ?? currentTenantId;
      if (!tenantIdForToken) throw new Error('Select a tenant before generating a token');
      const issued = await client.createEnrollmentToken({
        name: `command-install-${installOS}-${Date.now()}`,
        tenant_id: tenantIdForToken,
        max_nodes: 1,
        ttl: '24h',
        labels: { onboard_source: 'command-install', platform: installPlatform(installOS) },
        capabilities: ['agent.run'],
      });
      if (!issued.token) throw new Error('controlplane returned no raw enrollment token');
      return issued;
    },
    onSuccess: (issued) => {
      setInstallToken(issued);
      setInstallError(null);
      setCopied(false);
      toast.success('Token generated');
    },
    onError: (err) => {
      const msg = err instanceof Error ? err.message : 'Token generation failed';
      setInstallError(msg);
      toast.error(msg);
    },
  });

  const createTenant = useMutation({
    mutationFn: () => client.createTenant({ name: newTenantName.trim() }),
    onSuccess: async (t) => {
      await refreshTenants();
      selectEnrollmentTenant(t.id);
      setNewTenantName('');
      setCreatingTenant(false);
      toast.success(`Tenant "${t.name}" created`);
    },
    onError: (err) =>
      toast.error(err instanceof Error ? err.message : 'Failed to create tenant'),
  });

  const submitTest = (e: React.FormEvent) => {
    e.preventDefault();
    setJobId(null);
    const payload: TestConnectionPayload = {
      protocol,
      host: host.trim(),
      port: port ? Number(port) : undefined,
      username: username.trim() || undefined,
      auth: protocol === 'rdp' ? undefined : auth,
      password: auth === 'password' ? password : undefined,
      private_key: auth === 'private_key' ? privateKey : undefined,
      passphrase: auth === 'private_key' && passphrase ? passphrase : undefined,
      https: protocol === 'winrm' ? https : undefined,
      skip_verify: protocol === 'winrm' ? skipVerify : undefined,
      timeout_ms: 12_000,
    };
    test.mutate(payload);
  };

  const submittable =
    host.trim().length > 0 &&
    (protocol === 'rdp' ||
      (username.trim().length > 0 &&
        ((auth === 'password' && password.length > 0) ||
          (auth === 'private_key' && privateKey.trim().length > 0))));

  const canEnrol =
    !!result?.ok &&
    protocol !== 'rdp' &&
    !!enrolTenantId &&
    groupName.trim().length > 0 &&
    !enrol.isPending;

  return (
    <div className="flex flex-col gap-5">
      <SectionHeader
        eyebrow="ONBOARDING"
        title="Add machines to Control One"
        description="Agent identity survives hostname and IP changes."
      />

      <OnboardAIPanel />

      {/* ── Scenario cards ───────────────────────────────────────────── */}
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
        <ScenarioCard
          icon={<Monitor className="h-5 w-5" />}
          title="Command install"
          description="No inbound access required."
          outcome="First heartbeat activates the machine."
          active={scenario === 'local'}
          onClick={() => setScenario(scenario === 'local' ? null : 'local')}
        />
        <ScenarioCard
          icon={<Globe className="h-5 w-5" />}
          title="Install on another machine"
          description="SSH or WinRM push install."
          outcome="Credential probe required before enrollment."
          active={scenario === 'remote'}
          onClick={() => setScenario(scenario === 'remote' ? null : 'remote')}
        />
        <ScenarioCard
          icon={<Layers className="h-5 w-5" />}
          title="Bulk enroll"
          description="CSV, pasted target list, or API-driven enrollment."
          outcome="Many machines, one tracked job."
          active={false}
          onClick={() => {}}
          link="/fleet-enroll"
        />
        <ScenarioCard
          icon={<Package className="h-5 w-5" />}
          title="Offline or restricted network"
          description="Signed bundle for restricted networks."
          outcome="No internet fetch during install."
          active={false}
          onClick={() => {}}
          link="/offline-bundle"
        />
        <ScenarioCard
          icon={<Wrench className="h-5 w-5" />}
          title="Repair existing agent"
          description="One-shot reinstall token."
          outcome="Identity and history preserved."
          active={false}
          onClick={() => {}}
          link="/nodes"
        />
      </div>

      {/* ── Command install panel ─────────────────────────────────────── */}
      {scenario === 'local' && (
        <Panel padding="md" eyebrow="COMMAND INSTALL" title="Run install command" toneAccent="brand">
          <p className="text-sm text-text-secondary mb-3">
            Run from an elevated terminal on the machine being enrolled.
          </p>
          <Tabs
            value={installOS}
            onValueChange={(v) => {
              setInstallOS(v as InstallOS);
              resetInstallCommand();
            }}
            className="mb-3"
          >
            <TabsList>
              <TabsTrigger value="linux">
                <Terminal className="h-4 w-4" /> Linux
              </TabsTrigger>
              <TabsTrigger value="macos">
                <Monitor className="h-4 w-4" /> macOS
              </TabsTrigger>
              <TabsTrigger value="windows">
                <Monitor className="h-4 w-4" /> Windows
              </TabsTrigger>
            </TabsList>
          </Tabs>
          <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
            <p className="text-xs text-text-secondary">One machine, 24h token.</p>
            <Button
              type="button"
              variant="secondary"
              size="sm"
              loading={generateInstallToken.isPending}
              disabled={generateInstallToken.isPending || !(enrolTenantId ?? currentTenantId)}
              onClick={() => generateInstallToken.mutate()}
            >
              <Key className="h-4 w-4" />
              {installToken?.token ? 'Regenerate token' : 'Generate token'}
            </Button>
          </div>
          <div className="flex items-center gap-2 rounded-md border border-border-subtle bg-surface px-3 py-2 font-mono text-xs text-foreground">
            <code className="flex-1 break-all">{installCommand}</code>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              disabled={!installToken?.token}
              onClick={() => {
                if (!installToken?.token) return;
                navigator.clipboard.writeText(installCommand);
                setCopied(true);
                toast.success('Copied to clipboard');
                setTimeout(() => setCopied(false), 2000);
              }}
            >
              {copied ? <ClipboardCheck className="h-4 w-4" /> : <Clipboard className="h-4 w-4" />}
              {copied ? 'Copied' : 'Copy'}
            </Button>
          </div>
          {installError ? (
            <p className="mt-2 text-xs text-state-critical">{installError}</p>
          ) : null}
          <div className="mt-3 rounded-md border border-accent-400/20 bg-accent-400/5 px-3 py-2 text-xs text-text-secondary">
            First heartbeat activates the machine. Static IP and inbound access are not required.
          </div>
        </Panel>
      )}

      {/* ── "Install on another machine" — existing protocol-pick flow ── */}
      {scenario === 'remote' && (
        <>
          <div className="rounded-md border border-accent-400/20 bg-accent-400/5 px-3 py-2 text-xs text-text-secondary">
            First heartbeat activates the machine. Inbound access is not required after install. IP changes remain network observations.
          </div>

          <Panel padding="md" eyebrow="STEP 1 · PROTOCOL" title="Remote install method" toneAccent="brand">
            <Tabs
              value={protocol}
              onValueChange={(v) => {
                setProtocol(v as OnboardingProtocol);
                setResult(null);
                setJobId(null);
              }}
            >
              <TabsList>
                <TabsTrigger value="ssh">
                  <Terminal className="h-4 w-4" /> SSH
                </TabsTrigger>
                <TabsTrigger value="winrm">
                  <Server className="h-4 w-4" /> WinRM
                </TabsTrigger>
                <TabsTrigger value="rdp">
                  <Network className="h-4 w-4" /> RDP
                </TabsTrigger>
              </TabsList>
              <TabsContent value={protocol}>
                <p className="text-xs text-text-secondary">{PROTO_HINT[protocol]}</p>
              </TabsContent>
            </Tabs>
          </Panel>

          <form onSubmit={submitTest} className="grid grid-cols-1 gap-4 lg:grid-cols-2">
            <Panel padding="md" eyebrow="TARGET" title="Current network address">
              <Field label="Address" htmlFor="onboard-address" icon={<Globe className="h-3.5 w-3.5" />}>
                <Input
                  id="onboard-address"
                  placeholder="10.0.0.42 or machine.example.com"
                  value={host}
                  onChange={(e) => setHost(e.target.value)}
                  required
                />
              </Field>
              <Field label="Port (optional)" htmlFor="onboard-port">
                <Input
                  id="onboard-port"
                  type="number"
                  placeholder={
                    protocol === 'ssh' ? '22' : protocol === 'winrm' ? (https ? '5986' : '5985') : '3389'
                  }
                  value={port}
                  onChange={(e) => setPort(e.target.value)}
                />
              </Field>
              {protocol === 'winrm' && (
                <div className="flex flex-col gap-2 rounded-md border border-border-subtle bg-surface px-3 py-2">
                  <label className="inline-flex cursor-pointer items-center gap-2 text-sm text-foreground">
                    <input
                      type="checkbox"
                      className="h-4 w-4 rounded border-border-subtle accent-brand-500 cursor-pointer"
                      checked={https}
                      onChange={(e) => setHttps(e.target.checked)}
                    />
                    HTTPS (recommended — port 5986)
                  </label>
                  <label className="inline-flex cursor-pointer items-center gap-2 text-sm text-foreground">
                    <input
                      type="checkbox"
                      className="h-4 w-4 rounded border-border-subtle accent-brand-500 cursor-pointer"
                      checked={skipVerify}
                      onChange={(e) => setSkipVerify(e.target.checked)}
                    />
                    Skip TLS verification (lab use only)
                  </label>
                </div>
              )}
            </Panel>

            {protocol !== 'rdp' && (
              <Panel padding="md" eyebrow="CREDENTIALS" title="Authentication" toneAccent="accent">
                <Field label="Username" htmlFor="onboard-username" icon={<ShieldCheck className="h-3.5 w-3.5" />}>
                  <Input
                    id="onboard-username"
                    placeholder={protocol === 'ssh' ? 'ubuntu' : 'Administrator'}
                    value={username}
                    onChange={(e) => setUsername(e.target.value)}
                    required
                  />
                </Field>

                {protocol === 'ssh' ? (
                  <Tabs value={auth} onValueChange={(v) => setAuth(v as OnboardingAuth)}>
                    <TabsList>
                      <TabsTrigger value="password">
                        <Lock className="h-4 w-4" /> Password
                      </TabsTrigger>
                      <TabsTrigger value="private_key">
                        <Key className="h-4 w-4" /> Private key
                      </TabsTrigger>
                    </TabsList>
                    <TabsContent value="password">
                      <Field label="Password" htmlFor="onboard-ssh-password">
                        <Input
                          id="onboard-ssh-password"
                          type="password"
                          placeholder="••••••••"
                          value={password}
                          onChange={(e) => setPassword(e.target.value)}
                        />
                      </Field>
                    </TabsContent>
                    <TabsContent value="private_key">
                      <div className="flex flex-col gap-1.5">
                        <div className="flex items-center justify-between">
                          <Label className="inline-flex items-center gap-1.5">
                            <Key className="h-3.5 w-3.5" />
                            Private key (PEM body)
                          </Label>
                          <FileUploadButton
                            accept=".pem,.key,.pub,text/plain"
                            label="Upload .pem"
                            onContent={(text) => setPrivateKey(text.trim())}
                          />
                        </div>
                        <textarea
                          className="flex min-h-[120px] w-full rounded-md border border-border-subtle bg-surface px-3 py-2 font-mono text-xs text-foreground focus-visible:outline-none focus-visible:border-border-strong focus-visible:ring-2 focus-visible:ring-brand-500/30 resize-y"
                          placeholder="-----BEGIN OPENSSH PRIVATE KEY-----…"
                          value={privateKey}
                          onChange={(e) => setPrivateKey(e.target.value)}
                          autoComplete="off"
                        />
                      </div>
                      <Field label="Passphrase (optional)" htmlFor="onboard-private-key-passphrase">
                        <Input
                          id="onboard-private-key-passphrase"
                          type="password"
                          placeholder="leave blank if key is unencrypted"
                          value={passphrase}
                          onChange={(e) => setPassphrase(e.target.value)}
                          autoComplete="new-password"
                        />
                      </Field>
                    </TabsContent>
                  </Tabs>
                ) : (
                  <Field label="Password" htmlFor="onboard-winrm-password">
                    <Input
                      id="onboard-winrm-password"
                      type="password"
                      placeholder="••••••••"
                      value={password}
                      onChange={(e) => {
                        setPassword(e.target.value);
                        setAuth('password');
                      }}
                    />
                  </Field>
                )}
                <p className="text-[0.65rem] text-text-muted">
                  Credentials are sent once, used to probe the machine, and never persisted. No keys hit the database.
                </p>
              </Panel>
            )}

            <div className="lg:col-span-2 flex items-center justify-end gap-2">
              <Button
                type="submit"
                variant="primary"
                size="lg"
                shimmer
                loading={test.isPending}
                disabled={!submittable || test.isPending}
              >
                {test.isPending ? 'Testing…' : 'Test connection'}
              </Button>
            </div>
          </form>

          {result && (
            <Panel
              padding="md"
              tone={result.ok ? 'glow' : 'default'}
              toneAccent={result.ok ? 'healthy' : 'critical'}
              eyebrow={result.ok ? 'STEP 1 RESULT · CONNECTION VERIFIED' : 'STEP 1 RESULT · CONNECTION FAILED'}
              title={
                <span className="inline-flex items-center gap-2">
                  {result.ok ? (
                    <CheckCircle2 className="h-5 w-5 text-state-healthy" />
                  ) : (
                    <XCircle className="h-5 w-5 text-state-critical" />
                  )}
                   {result.ok ? 'Machine is reachable' : 'Could not reach the machine'}
                </span>
              }
            >
              {result.ok && result.probe ? (
                <ProbeSummary probe={result.probe} />
              ) : (
                <div className="flex flex-col gap-3">
                  <p className="text-sm text-state-critical">
                    {result.error || 'Connection failed for an unknown reason.'}
                  </p>
                  <div className="flex flex-wrap items-center gap-2">
                    <Button type="button" variant="secondary" size="sm" onClick={() => setScenario('local')}>
                      Command install
                    </Button>
                    <Button type="button" variant="ghost" size="sm" asChild>
                      <Link to="/offline-bundle">Offline bundle</Link>
                    </Button>
                  </div>
                </div>
              )}
            </Panel>
          )}

          {result?.ok && protocol === 'rdp' && (
            <Panel
              padding="md"
              eyebrow="ENROLLMENT PATH"
              title="WinRM or command install required"
              toneAccent="accent"
            >
              <p className="text-sm text-text-secondary">
                RDP confirms reachability only. Agent enrollment needs WinRM, SSH, command install, or an offline bundle.
              </p>
              <div className="mt-3 flex flex-wrap items-center justify-end gap-2">
                <Button type="button" variant="secondary" size="sm" onClick={() => setProtocol('winrm')}>
                  Switch to WinRM
                </Button>
                <Button type="button" variant="ghost" size="sm" onClick={() => setScenario('local')}>
                  Command install
                </Button>
              </div>
            </Panel>
          )}

          {result?.ok && protocol !== 'rdp' && (
            <Panel
              padding="md"
              eyebrow="STEP 2 · GROUP & ENROL"
              title="Name the machine group and enrol"
              toneAccent="brand"
            >
              <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                <Field
                  label="Machine group"
                  htmlFor="onboard-machine-group"
                  icon={<Sparkles className="h-3.5 w-3.5 text-accent-400" />}
                >
                  <Input
                    id="onboard-machine-group"
                    placeholder="Group US"
                    value={groupName}
                    onChange={(e) => {
                      setGroupName(e.target.value);
                      setGroupTouched(true);
                    }}
                  />
                  <p className="text-[0.65rem] text-text-muted">
                    Auto-suggested from{' '}
                    {enrichQ.isLoading ? 'IP intelligence…' : enrichQ.data?.geo?.country_code
                      ? `IP origin (${enrichQ.data.geo.country_code}${enrichQ.data.geo?.country ? ' · ' + enrichQ.data.geo.country : ''})`
                      : 'a generic default'}{' '}
                    for rule and dashboard scoping.
                  </p>
                </Field>
                <div className="flex flex-col gap-1.5">
                  {creatingTenant ? (
                    <div className="flex flex-col gap-1.5">
                      <Label htmlFor="onboard-new-tenant">New tenant</Label>
                      <div className="flex items-center gap-2">
                        <Input
                          id="onboard-new-tenant"
                          autoFocus
                          placeholder="e.g. Production"
                          value={newTenantName}
                          onChange={(e) => setNewTenantName(e.target.value)}
                          onKeyDown={(e) => {
                            if (e.key === 'Enter') {
                              e.preventDefault();
                              if (newTenantName.trim() && !createTenant.isPending) {
                                createTenant.mutate();
                              }
                            } else if (e.key === 'Escape') {
                              e.preventDefault();
                              setCreatingTenant(false);
                              setNewTenantName('');
                            }
                          }}
                          disabled={createTenant.isPending}
                        />
                        <Button
                          type="button"
                          variant="primary"
                          size="sm"
                          loading={createTenant.isPending}
                          disabled={!newTenantName.trim() || createTenant.isPending}
                          onClick={() => createTenant.mutate()}
                        >
                          Create
                        </Button>
                        <Button
                          type="button"
                          variant="ghost"
                          size="sm"
                          disabled={createTenant.isPending}
                          onClick={() => {
                            setCreatingTenant(false);
                            setNewTenantName('');
                          }}
                        >
                          Cancel
                        </Button>
                      </div>
                    </div>
                  ) : (
                    <div className="flex items-end gap-2">
                      <SelectField
                        label="Tenant"
                        value={enrolTenantId ?? ''}
                        onChange={(e) => selectEnrollmentTenant(e.target.value || null)}
                        wrapperClassName="flex-1"
                      >
                        <option value="">Select tenant…</option>
                        {tenants.map((t) => (
                          <option key={t.id} value={t.id}>{t.name}</option>
                        ))}
                      </SelectField>
                      <Button
                        type="button"
                        variant="ghost"
                        size="sm"
                        onClick={() => setCreatingTenant(true)}
                      >
                        <Plus className="h-4 w-4" /> New tenant
                      </Button>
                    </div>
                  )}
                  <p className="text-[0.65rem] text-text-muted">
                    Active tenant selected by default. Enrollment is tenant-scoped.
                  </p>
                </div>
              </div>

              <div className="mt-2 flex flex-wrap items-center justify-end gap-2">
                <Button
                  variant="ghost"
                  size="md"
                  asChild
                >
                  <Link to="/fleet-enroll">Open bulk enrollment</Link>
                </Button>
                <Button
                  variant="primary"
                  size="lg"
                  shimmer
                  loading={enrol.isPending}
                  disabled={!canEnrol}
                  onClick={() => enrol.mutate()}
                >
                   Enrol machine <ArrowRight className="h-4 w-4" />
                </Button>
              </div>

              {jobId && (
                <div className="mt-3 rounded-md border border-state-healthy/40 bg-state-healthy/10 px-3 py-2 text-sm text-state-healthy">
                  <span className="font-mono text-[0.7rem] uppercase tracking-wider">Job started</span>{' '}
                  <span className="font-mono text-xs">{jobId}</span>{' '}
                  <Link
                    to={`/fleet-enroll?job_id=${jobId}`}
                    className="ml-2 underline hover:text-state-healthy/80"
                  >
                    Watch progress
                  </Link>
                </div>
              )}
            </Panel>
          )}

          {!result && !test.isPending && (
            <EmptyState
              title="Connection test required"
              description="Successful probes unlock enrollment."
              icon={<Terminal />}
            />
          )}
        </>
      )}

      {/* ── Repair existing agent ─────────────────────────────────────── */}
      {scenario === 'repair' && (
        <Panel padding="md" eyebrow="REPAIR AGENT" title="Preserve identity during reinstall" toneAccent="accent">
          <p className="text-sm text-text-secondary">
            One-shot token. Fresh binary. Existing node history preserved.
          </p>
          <ul className="mt-2 list-disc pl-5 text-xs text-text-muted">
            <li>Agent node key and historical data are preserved.</li>
            <li>A fresh agent binary is deployed via SSH or WinRM.</li>
            <li>No manual re-enrollment is needed after the repair completes.</li>
          </ul>
          <div className="mt-3 flex justify-end">
            <Button asChild variant="primary" size="md" shimmer>
              <Link to="/repair-agent">
                Repair agent <ArrowRight className="h-4 w-4" />
              </Link>
            </Button>
          </div>
        </Panel>
      )}
    </div>
  );
}

function ScenarioCard({
  icon,
  title,
  description,
  outcome,
  active,
  onClick,
  link,
}: {
  icon: ReactNode;
  title: string;
  description: string;
  outcome: string;
  active: boolean;
  onClick: () => void;
  link?: string;
}): JSX.Element {
  const cardClasses = [
    'flex flex-col gap-2 rounded-lg border px-4 py-3 text-left transition-colors cursor-pointer',
    active
      ? 'border-brand-400 bg-brand-400/10 ring-1 ring-brand-400/30'
      : 'border-border-subtle bg-surface hover:border-border-strong hover:bg-surface-hover',
  ].join(' ');

  const inner = (
    <>
      <div className="flex items-center gap-2">
        <span className={active ? 'text-brand-400' : 'text-text-secondary'}>{icon}</span>
        <span className="text-sm font-medium text-foreground">{title}</span>
      </div>
      <p className="text-xs text-text-secondary leading-relaxed">{description}</p>
      <p className="text-[0.65rem] text-text-muted">{outcome}</p>
    </>
  );

  if (link) {
    return (
      <Link to={link} className={cardClasses.replace('cursor-pointer', 'cursor-pointer')}>
        {inner}
      </Link>
    );
  }

  return (
    <button type="button" className={cardClasses} onClick={onClick}>
      {inner}
    </button>
  );
}

function Field({ label, htmlFor, icon, children }: { label: string; htmlFor?: string; icon?: ReactNode; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-1.5">
      <Label htmlFor={htmlFor} className="inline-flex items-center gap-1.5">
        {icon}
        {label}
      </Label>
      {children}
    </div>
  );
}

function ProbeSummary({ probe }: { probe: ConnectionProbe }) {
  const tone: StateTone = probe.reachable ? 'healthy' : 'critical';

  function formatMem(mb?: number): string {
    if (!mb) return '—';
    if (mb >= 1024) return `${(mb / 1024).toFixed(1)} GB`;
    return `${mb} MB`;
  }

  return (
    <div className="flex flex-col gap-3">
      {/* Primary stats row */}
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
        <Stat label="Reachable">
          <StatusTag tone={tone}>{probe.reachable ? 'YES' : 'NO'}</StatusTag>
        </Stat>
        <Stat label="Latency">{probe.latency_ms ? `${probe.latency_ms} ms` : '—'}</Stat>
        <Stat label="Hostname">{probe.hostname || '—'}</Stat>
        <Stat label="Architecture">{probe.architecture || '—'}</Stat>
      </div>

      {/* OS / distro row */}
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
        <Stat label="OS">{probe.os || '—'}</Stat>
        <Stat label="Distribution">{probe.distro || probe.os_version || '—'}</Stat>
        <Stat label="CPUs">{probe.cpu_count ? `${probe.cpu_count} cores` : '—'}</Stat>
        <Stat label="Memory">{formatMem(probe.memory_mb)}</Stat>
      </div>

      {/* Capabilities */}
      {probe.capabilities && probe.capabilities.length > 0 && (
        <div className="flex flex-wrap items-center gap-1.5">
          <Eyebrow>capabilities</Eyebrow>
          {probe.capabilities.map((c) => (
            <StatusTag key={c} tone="info">{c}</StatusTag>
          ))}
        </div>
      )}

      {/* SSH banner */}
      {probe.banner && (
        <div className="rounded-md border border-border-subtle bg-surface px-3 py-2 font-mono text-[0.7rem] text-text-secondary">
          <span className="text-text-muted">SSH banner: </span>{probe.banner}
        </div>
      )}

      {/* Full uname for power users */}
      {probe.os_version && probe.os_version !== probe.distro && (
        <div className="rounded-md border border-border-subtle bg-surface px-3 py-2 font-mono text-[0.7rem] text-text-secondary break-all">
          <span className="text-text-muted">kernel: </span>{probe.os_version}
        </div>
      )}
    </div>
  );
}

function Stat({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-0.5 rounded-md border border-border-subtle bg-surface px-3 py-2">
      <span className="font-mono text-[0.6rem] uppercase tracking-wider text-text-muted">{label}</span>
      <span className="font-mono text-sm text-foreground">{children}</span>
    </div>
  );
}

// toBase64 — turn a PEM string into the base64 form expected by the
// existing fleet-enroll endpoint. Browsers don't ship an obvious helper
// for arbitrary unicode; btoa works on PEM because it's already 7-bit
// printable, but we sanitize CR/LF first.
function toBase64(pem: string): string {
  const normalized = pem.replace(/\r\n/g, '\n');
  return btoa(normalized);
}
