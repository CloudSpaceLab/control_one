import { useEffect, useRef, useState, type FormEvent } from 'react';
import { KeyRound, Loader2, Sparkles, Terminal, Upload } from 'lucide-react';
import { Alert, Eyebrow, Loader } from '@/components/kit';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { useApiClient } from '@/hooks/useApiClient';
import { useToast } from '@/providers/ToastProvider';
import { formatTs } from '@/lib/format';
import type { EnrollmentToken, FleetEnrollStatus, Node } from '@/lib/api';

type Mode = 'ssh' | 'manual';
type AuthKind = 'private_key' | 'password';

export interface RepairAgentDialogProps {
  open: boolean;
  node: Node;
  onOpenChange: (open: boolean) => void;
}

// RepairAgentDialog ships agent-repair as a one-click flow when controlplane
// can SSH into the host with operator-supplied credentials, and falls back
// to a copy-paste curl one-liner when SSH fails or the operator can't share
// keys with the controlplane.
export function RepairAgentDialog({ open, node, onOpenChange }: RepairAgentDialogProps): JSX.Element {
  const [mode, setMode] = useState<Mode>('ssh');

  // Reset to SSH-first whenever the dialog reopens.
  useEffect(() => {
    if (open) setMode('ssh');
  }, [open]);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>Repair / re-enroll {node.hostname || 'this node'}</DialogTitle>
        </DialogHeader>
        <p className="text-sm text-text-secondary">
          Re-runs agent installation on the host. Default path: SSH from
          controlplane using credentials you provide here (one-shot, never
          stored). Fallback: copy-paste a curl one-liner an operator runs
          manually if the SSH path can&apos;t reach the host.
        </p>
        <Tabs value={mode} onValueChange={(v) => setMode(v as Mode)} className="mt-2">
          <TabsList>
            <TabsTrigger value="ssh">
              <Sparkles className="h-4 w-4" /> Run via SSH
            </TabsTrigger>
            <TabsTrigger value="manual">
              <Terminal className="h-4 w-4" /> Copy command
            </TabsTrigger>
          </TabsList>
          <TabsContent value="ssh" className="mt-4">
            <SSHRepair
              node={node}
              onClose={() => onOpenChange(false)}
              onFallback={() => setMode('manual')}
            />
          </TabsContent>
          <TabsContent value="manual" className="mt-4">
            <ManualRepair node={node} onClose={() => onOpenChange(false)} />
          </TabsContent>
        </Tabs>
      </DialogContent>
    </Dialog>
  );
}

// ── SSH-driven path ────────────────────────────────────────────────────────

function SSHRepair({
  node,
  onClose,
  onFallback,
}: {
  node: Node;
  onClose: () => void;
  onFallback: () => void;
}) {
  const client = useApiClient();
  const { showToast } = useToast();

  const [authKind, setAuthKind] = useState<AuthKind>('private_key');
  const [host, setHost] = useState(node.public_ip ?? node.hostname ?? '');
  const [port, setPort] = useState(22);
  const [user, setUser] = useState('root');
  const [keyB64, setKeyB64] = useState('');
  const [keyFilename, setKeyFilename] = useState('');
  const [password, setPassword] = useState('');

  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [jobId, setJobId] = useState<string | null>(null);
  const [jobStatus, setJobStatus] = useState<{
    status: string;
    success?: boolean;
    error?: string;
    output?: string;
  } | null>(null);

  // Reset state on node change.
  useEffect(() => {
    setHost(node.public_ip ?? node.hostname ?? '');
    setError(null);
    setJobId(null);
    setJobStatus(null);
  }, [node.id, node.public_ip, node.hostname]);

  // Poll the fleet-enroll job status while a job is in flight.
  useEffect(() => {
    if (!jobId) return;
    let cancelled = false;
    const tick = async () => {
      try {
        const resp: FleetEnrollStatus = await client.getFleetEnrollStatus(jobId, node.tenant_id);
        if (cancelled) return;
        const result = resp.results?.[0];
        const terminal = resp.status === 'succeeded' || resp.status === 'failed';
        setJobStatus({
          status: resp.status,
          success: result?.success,
          error: result?.error_message,
          output: result?.ssh_output,
        });
        if (terminal) {
          setBusy(false);
          if (resp.status === 'succeeded' && result?.success) {
            showToast('Agent re-enrolled. Telemetry should resume on next heartbeat.', 'success');
          }
          return;
        }
      } catch (err) {
        if (cancelled) return;
        setError(err instanceof Error ? err.message : 'status poll failed');
        setBusy(false);
        return;
      }
      if (!cancelled) setTimeout(tick, 3000);
    };
    setBusy(true);
    tick();
    return () => {
      cancelled = true;
    };
  }, [client, jobId, showToast, node.tenant_id]);

  const fileInputRef = useRef<HTMLInputElement>(null);
  const onPickKey = (file: File | null) => {
    if (!file) return;
    setKeyFilename(file.name);
    const reader = new FileReader();
    reader.onload = () => {
      const text = String(reader.result ?? '');
      // Strip any wrapping whitespace before encoding.
      setKeyB64(btoa(text.trim()));
    };
    reader.readAsText(file);
  };

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setError(null);
    setJobStatus(null);
    if (authKind === 'private_key' && !keyB64) {
      setError('Pick or paste a private key, or switch to password auth.');
      return;
    }
    if (authKind === 'password' && !password) {
      setError('Enter the password.');
      return;
    }
    setBusy(true);
    try {
      const resp = await client.repairNodeViaSSH(node.id, {
        ssh_user: user,
        ssh_key: authKind === 'private_key' ? keyB64 : undefined,
        ssh_password: authKind === 'password' ? password : undefined,
        host_override: host,
        port,
      });
      setJobId(resp.job_id);
      // Wipe the password from memory once we hand it off.
      setPassword('');
    } catch (err) {
      setBusy(false);
      setError(err instanceof Error ? err.message : 'dispatch failed');
    }
  };

  const finished = jobStatus && (jobStatus.status === 'succeeded' || jobStatus.status === 'failed');
  const ok = finished && jobStatus?.success;
  const failed = finished && !jobStatus?.success;

  if (jobId) {
    return (
      <div className="flex flex-col gap-3">
        {!finished && (
          <Alert variant="info">
            <span className="inline-flex items-center gap-2">
              <Loader2 className="h-4 w-4 animate-spin" /> Repair in flight on {host}:{port} (job{' '}
              <span className="font-mono text-xs">{jobId.slice(0, 8)}</span>) — status:{' '}
              {jobStatus?.status ?? 'queued'}
            </span>
          </Alert>
        )}
        {ok && (
          <Alert variant="success" title="Agent re-enrolled">
            Agent installation completed. The node will check in within ~60s and
            telemetry / knowledge-graph / recommendations will populate
            automatically.
          </Alert>
        )}
        {failed && (
          <Alert
            variant="critical"
            title="Repair via SSH failed"
            actions={
              <Button variant="secondary" size="sm" onClick={onFallback}>
                Switch to copy-paste
              </Button>
            }
          >
            {jobStatus?.error || 'Provisioner returned failure.'}
          </Alert>
        )}
        {jobStatus?.output && (
          <details className="rounded-md border border-border-subtle bg-surface p-2">
            <summary className="cursor-pointer text-xs text-text-muted">SSH output</summary>
            <pre className="mt-2 overflow-x-auto whitespace-pre-wrap font-mono text-[0.7rem] leading-relaxed text-text-secondary">
              {jobStatus.output}
            </pre>
          </details>
        )}
        <DialogFooter>
          {finished && !ok && (
            <Button
              variant="ghost"
              onClick={() => {
                setJobId(null);
                setJobStatus(null);
                setBusy(false);
              }}
            >
              Try again
            </Button>
          )}
          <Button variant="primary" onClick={onClose}>
            Done
          </Button>
        </DialogFooter>
      </div>
    );
  }

  return (
    <form onSubmit={submit} className="flex flex-col gap-3">
      <p className="text-xs text-text-muted">
        Credentials are sent once to the controlplane to drive agent install
        and are wiped from memory when the job finishes. They are never written
        to disk.
      </p>
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-[2fr_1fr]">
        <Field label="Host">
          <Input value={host} onChange={(e) => setHost(e.target.value)} required />
        </Field>
        <Field label="Port">
          <Input
            type="number"
            value={port}
            onChange={(e) => setPort(Number(e.target.value) || 22)}
            min={1}
            max={65535}
          />
        </Field>
      </div>
      <Field label="SSH user">
        <Input value={user} onChange={(e) => setUser(e.target.value)} required />
      </Field>
      <Tabs value={authKind} onValueChange={(v) => setAuthKind(v as AuthKind)}>
        <TabsList>
          <TabsTrigger value="private_key">
            <KeyRound className="h-4 w-4" /> Private key
          </TabsTrigger>
          <TabsTrigger value="password">Password</TabsTrigger>
        </TabsList>
        <TabsContent value="private_key" className="mt-3 flex flex-col gap-2">
          <input
            ref={fileInputRef}
            type="file"
            accept=".pem,.key,application/x-pem-file,text/plain"
            className="hidden"
            onChange={(e) => onPickKey(e.target.files?.[0] ?? null)}
          />
          <div className="flex flex-wrap items-center gap-2">
            <Button
              type="button"
              variant="secondary"
              size="sm"
              onClick={() => fileInputRef.current?.click()}
            >
              <Upload className="h-4 w-4" /> Pick .pem file
            </Button>
            {keyFilename && (
              <Eyebrow>
                {keyFilename} · {(atob(keyB64).length / 1024).toFixed(1)} KB loaded
              </Eyebrow>
            )}
          </div>
          <p className="text-xs text-text-muted">
            Or paste the PEM contents (starts with -----BEGIN ...).
          </p>
          <textarea
            rows={4}
            className="rounded-md border border-border-subtle bg-surface px-3 py-2 font-mono text-[0.7rem] text-foreground focus:border-brand-500 focus:outline-none"
            placeholder="-----BEGIN OPENSSH PRIVATE KEY-----&#10;..."
            onChange={(e) => {
              const text = e.target.value.trim();
              if (!text) {
                setKeyB64('');
                setKeyFilename('');
                return;
              }
              setKeyB64(btoa(text));
              setKeyFilename('(pasted key)');
            }}
          />
        </TabsContent>
        <TabsContent value="password" className="mt-3">
          <Field label="Password">
            <Input
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              autoComplete="off"
            />
          </Field>
        </TabsContent>
      </Tabs>

      {error && <Alert variant="critical">{error}</Alert>}

      <DialogFooter>
        <Button type="button" variant="ghost" onClick={onClose}>
          Cancel
        </Button>
        <Button type="submit" variant="primary" disabled={busy}>
          {busy ? <Loader size="xs" /> : <Sparkles className="h-4 w-4" />}
          {busy ? 'Dispatching…' : 'Repair via SSH'}
        </Button>
      </DialogFooter>
    </form>
  );
}

// ── Manual / fallback path ─────────────────────────────────────────────────

function ManualRepair({ node, onClose }: { node: Node; onClose: () => void }) {
  const client = useApiClient();
  const { showToast } = useToast();
  const [busy, setBusy] = useState(false);
  const [token, setToken] = useState<EnrollmentToken | null>(null);

  const generate = async () => {
    setBusy(true);
    try {
      const issued = await client.createEnrollmentToken({
        name: `re-enroll · ${node.hostname}`,
        tenant_id: node.tenant_id,
        max_nodes: 1,
        ttl: '24h',
        labels: { node_id: node.id, hostname: node.hostname, source: 'manual-repair' },
      });
      setToken(issued);
    } catch (err) {
      showToast(err instanceof Error ? err.message : 'token generation failed', 'error');
    } finally {
      setBusy(false);
    }
  };

  const origin = typeof window !== 'undefined' ? window.location.origin : '';
  const installCmd = token?.token
    ? `curl -fsSL '${origin}/api/v1/agent/install-script?token=${encodeURIComponent(token.token)}&platform=linux' | sudo bash`
    : '';

  const copy = async (label: string, value: string) => {
    if (!value) return;
    await navigator.clipboard.writeText(value);
    showToast(`${label} copied`, 'success');
  };

  if (!token) {
    return (
      <div className="flex flex-col gap-3">
        <p className="text-sm text-text-secondary">
          Use this when the controlplane can&apos;t reach the host over SSH (firewalled,
          air-gapped, missing credentials). Generates a one-shot 24h enrollment
          token + a curl one-liner to paste on the host as root.
        </p>
        <DialogFooter>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="primary" disabled={busy} onClick={generate}>
            {busy ? 'Generating…' : 'Generate install command'}
          </Button>
        </DialogFooter>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-col gap-2 rounded-md border border-border-subtle bg-surface p-3">
        <div className="flex items-center justify-between">
          <Eyebrow>Run this on the host</Eyebrow>
          <Button variant="secondary" size="sm" onClick={() => copy('Install command', installCmd)}>
            Copy command
          </Button>
        </div>
        <pre className="overflow-x-auto rounded-md border border-border-subtle bg-surface-2 p-2 font-mono text-[0.7rem] leading-relaxed text-text-secondary">
          <code>{installCmd}</code>
        </pre>
        <p className="text-xs text-text-muted">
          Idempotent — safe to re-run. The installer reuses the same node id
          via the labeled token.
        </p>
      </div>
      <div className="flex flex-col gap-2 rounded-md border border-border-subtle bg-surface p-3">
        <div className="flex items-center justify-between">
          <Eyebrow>Bootstrap token</Eyebrow>
          <Button variant="ghost" size="sm" onClick={() => copy('Token', token.token ?? '')}>
            Copy token
          </Button>
        </div>
        <code className="break-all font-mono text-[0.7rem] text-text-muted">
          {token.token ?? '(redacted by server)'}
        </code>
        <p className="text-xs text-text-muted">Expires {formatTs(token.expires_at)}.</p>
      </div>
      <DialogFooter>
        <Button variant="primary" onClick={onClose}>
          Done
        </Button>
      </DialogFooter>
    </div>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-1">
      <Label className="text-xs text-text-muted">{label}</Label>
      {children}
    </div>
  );
}
