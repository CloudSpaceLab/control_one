import { useMutation, useQuery } from '@tanstack/react-query';
import {
  ArrowRight,
  Boxes,
  CheckCircle2,
  Globe,
  Key,
  Lock,
  Network,
  Server,
  ShieldCheck,
  Sparkles,
  Terminal,
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
import { toast } from 'sonner';
import type {
  ConnectionProbe,
  IpEnrichment,
  OnboardingAuth,
  OnboardingProtocol,
  TestConnectionPayload,
  TestConnectionResult,
} from '../lib/api';

const PROTO_HINT: Record<OnboardingProtocol, string> = {
  ssh: 'Linux, macOS, or any host with sshd. Default port 22.',
  winrm: 'Windows Server with WinRM enabled. Default port 5985 (HTTP) / 5986 (HTTPS).',
  rdp: 'TCP reachability check only. Pair with a WinRM credential to enrol the host.',
};

type Mode = 'single' | 'hypervisor';

export function Onboard(): JSX.Element {
  const client = useApiClient();
  const { currentTenantId, tenants } = useTenant();
  const [mode, setMode] = useState<Mode>('single');

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

  useEffect(() => {
    if (!enrolTenantId && currentTenantId) setEnrolTenantId(currentTenantId);
  }, [currentTenantId, enrolTenantId]);

  // Auto country lookup once we know the host is reachable. Uses the
  // existing ipintel pipeline (akyriako/ipquery + AbuseIPDB fallback).
  const enrichQ = useQuery<IpEnrichment | null>({
    queryKey: ['onboard.enrich', host, result?.ok],
    queryFn: async () => {
      try {
        return await client.enrichIp(host);
      } catch {
        return null;
      }
    },
    enabled: !!result?.ok && /^[\d.:a-fA-F]+$/.test(host),
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
      // queued-job + per-host result tracking the bulk enrol page already
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
        title="Add servers to Control One"
        description="Single host or pull a fleet from a hypervisor — both reuse the existing fleet-enrol job pipeline so onboarding stays consistent across paths."
      />

      <Tabs value={mode} onValueChange={(v) => setMode(v as Mode)}>
        <TabsList>
          <TabsTrigger value="single">
            <Server className="h-4 w-4" /> Single server
          </TabsTrigger>
          <TabsTrigger value="hypervisor">
            <Boxes className="h-4 w-4" /> Hypervisor / cloud account
          </TabsTrigger>
        </TabsList>
      </Tabs>

      {mode === 'hypervisor' ? <HypervisorPathCard /> : null}

      {mode === 'single' && (
        <>
          <Panel padding="md" eyebrow="STEP 1 · PROTOCOL" title="Pick how to reach the host" toneAccent="brand">
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
            <Panel padding="md" eyebrow="TARGET" title="Where is the server?">
              <Field label="Host" icon={<Globe className="h-3.5 w-3.5" />}>
                <Input
                  placeholder="10.0.0.42 or server.example.com"
                  value={host}
                  onChange={(e) => setHost(e.target.value)}
                  required
                />
              </Field>
              <Field label="Port (optional)">
                <Input
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
              <Panel padding="md" eyebrow="CREDENTIALS" title="How should we authenticate?" toneAccent="accent">
                <Field label="Username" icon={<ShieldCheck className="h-3.5 w-3.5" />}>
                  <Input
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
                      <Field label="Password">
                        <Input
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
                      <Field label="Passphrase (optional)">
                        <Input
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
                  <Field label="Password">
                    <Input
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
                  Credentials are sent once, used to probe the host, and never persisted. No keys hit the database.
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
                  {result.ok ? 'Server is reachable' : 'Could not reach the server'}
                </span>
              }
            >
              {result.ok && result.probe ? (
                <ProbeSummary probe={result.probe} />
              ) : (
                <p className="text-sm text-state-critical">
                  {result.error || 'Connection failed for an unknown reason.'}
                </p>
              )}
            </Panel>
          )}

          {result?.ok && protocol !== 'rdp' && (
            <Panel
              padding="md"
              eyebrow="STEP 2 · GROUP & ENROL"
              title="Name the server group and enrol"
              toneAccent="brand"
            >
              <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                <Field
                  label="Server group"
                  icon={<Sparkles className="h-3.5 w-3.5 text-accent-400" />}
                >
                  <Input
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
                    — override anytime. Group is stored as a label so existing nodes pages, rules and dashboards filter by it automatically.
                  </p>
                </Field>
                <div className="flex flex-col gap-1.5">
                  <SelectField
                    label="Tenant"
                    value={enrolTenantId ?? ''}
                    onChange={(e) => setEnrolTenantId(e.target.value || null)}
                  >
                    <option value="">Select tenant…</option>
                    {tenants.map((t) => (
                      <option key={t.id} value={t.id}>{t.name}</option>
                    ))}
                  </SelectField>
                  <p className="text-[0.65rem] text-text-muted">
                    Defaults to the active tenant. Single-server enrolment scopes the new node here.
                  </p>
                </div>
              </div>

              <div className="mt-2 flex flex-wrap items-center justify-end gap-2">
                <Button
                  variant="ghost"
                  size="md"
                  asChild
                >
                  <Link to="/fleet-enroll">Open bulk enrol →</Link>
                </Button>
                <Button
                  variant="primary"
                  size="lg"
                  shimmer
                  loading={enrol.isPending}
                  disabled={!canEnrol}
                  onClick={() => enrol.mutate()}
                >
                  Enrol server <ArrowRight className="h-4 w-4" />
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
                    Watch progress →
                  </Link>
                </div>
              )}
            </Panel>
          )}

          {!result && !test.isPending && (
            <EmptyState
              title="Run a test"
              description="Fill the form above and click Test connection. Successful probes unlock the enrolment step."
              icon={<Terminal />}
            />
          )}
        </>
      )}
    </div>
  );
}

function HypervisorPathCard(): JSX.Element {
  return (
    <Panel
      padding="md"
      eyebrow="HYPERVISOR / CLOUD ACCOUNT"
      title="Bulk-enrol from a hypervisor or cloud account"
      toneAccent="accent"
    >
      <p className="text-sm text-text-secondary">
        Register a vCenter, libvirt, AWS, or Azure account once — Control One enumerates running
        VMs and enrols them as a fleet. Existing tooling lives at{' '}
        <Link to="/hypervisors" className="text-brand-400 underline">
          /hypervisors
        </Link>
        . The single-server wizard reuses the same fleet-enrol pipeline, so groups, labels and
        agents stay consistent across both paths.
      </p>
      <ul className="mt-2 grid gap-2 sm:grid-cols-2">
        {[
          { name: 'AWS', hint: 'access key + region; enumerates EC2 + SSM-managed hosts' },
          { name: 'Azure', hint: 'service principal; enumerates VMs and Arc-connected machines' },
          { name: 'VMware vCenter', hint: 'API + read-only datastore role; pulls VMs across clusters' },
          { name: 'libvirt / KVM', hint: 'libvirt-uri credentials; enumerates running domains' },
        ].map((p) => (
          <li
            key={p.name}
            className="flex items-start gap-2 rounded-md border border-border-subtle bg-surface px-3 py-2"
          >
            <Boxes className="mt-0.5 h-4 w-4 text-accent-400" />
            <div className="flex flex-col">
              <span className="text-sm font-medium text-foreground">{p.name}</span>
              <span className="text-xs text-text-muted">{p.hint}</span>
            </div>
          </li>
        ))}
      </ul>
      <div className="mt-3 flex justify-end">
        <Button asChild variant="primary" size="md" shimmer>
          <Link to="/hypervisors">
            Go to /hypervisors <ArrowRight className="h-4 w-4" />
          </Link>
        </Button>
      </div>
    </Panel>
  );
}

function Field({ label, icon, children }: { label: string; icon?: ReactNode; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-1.5">
      <Label className="inline-flex items-center gap-1.5">
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

