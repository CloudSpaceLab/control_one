import { useCallback, useEffect, useRef, useState } from 'react';
import { Upload, Trash2, Download, RefreshCw } from 'lucide-react';
import { Button } from '../components/ui/button';
import {
  DataTable,
  EmptyState,
  Panel,
  SectionHeader,
  StatusTag,
  type StateTone,
} from '../components/kit';
import { ConfirmModal } from '../components/ConfirmModal';
import { useApiClient } from '../hooks/useApiClient';
import { useTenants } from '../hooks/useTenants';
import type { ComplianceEvidence as CEType, ComplianceReview } from '../lib/api';
import type { ColumnDef } from '@tanstack/react-table';

const EVIDENCE_TYPES = [
  'training_completion',
  'meeting',
  'review',
  'attestation',
  'policy',
  'audit_log_export',
  'other',
];

const FRAMEWORKS = ['SOC2', 'ISO27001', 'HIPAA', 'PCI-DSS', 'GDPR', 'other'];

const TABS = ['Evidence', 'Training records', 'Reviews'] as const;
type Tab = (typeof TABS)[number];

function frameworkTone(fw: string | undefined): StateTone {
  switch (fw) {
    case 'SOC2':
      return 'healthy';
    case 'ISO27001':
      return 'info';
    case 'HIPAA':
      return 'warning';
    case 'PCI-DSS':
      return 'degraded';
    case 'GDPR':
      return 'critical';
    default:
      return 'unknown';
  }
}

function formatDate(v?: string | null): string {
  if (!v) return '—';
  const d = new Date(v);
  return isNaN(d.getTime()) ? v : d.toLocaleDateString();
}

export function ComplianceEvidence(): JSX.Element {
  const client = useApiClient();
  const { data: tenantList } = useTenants();

  const [activeTab, setActiveTab] = useState<Tab>('Evidence');
  const [selectedTenant, setSelectedTenant] = useState<string>('');
  const [frameworkFilter, setFrameworkFilter] = useState<string>('');
  const [items, setItems] = useState<CEType[]>([]);
  const [loading, setLoading] = useState(false);
  const [deleteId, setDeleteId] = useState<string | null>(null);

  // Upload form state
  const [uploading, setUploading] = useState(false);
  const [uploadError, setUploadError] = useState<string | null>(null);
  const fileRef = useRef<HTMLInputElement>(null);
  const [form, setForm] = useState({
    title: '',
    evidence_type: EVIDENCE_TYPES[0],
    framework: '',
    control_ref: '',
    description: '',
  });

  // Reviews state
  const [reviews, setReviews] = useState<ComplianceReview[]>([]);
  const [reviewsLoading, setReviewsLoading] = useState(false);
  const [reviewForm, setReviewForm] = useState({
    review_type: 'quarterly_access_review',
    scheduled_for: '',
    recurrence: '',
    notes: '',
  });
  const [creatingReview, setCreatingReview] = useState(false);
  const [completeReviewId, setCompleteReviewId] = useState<string | null>(null);
  const [completeNotes, setCompleteNotes] = useState('');
  const [deleteReviewId, setDeleteReviewId] = useState<string | null>(null);

  const load = useCallback(async () => {
    if (!selectedTenant) return;
    setLoading(true);
    try {
      const evidenceType =
        activeTab === 'Training records' ? 'training_completion' : '';
      const res = await client.listComplianceEvidence({
        tenantId: selectedTenant,
        framework: frameworkFilter || undefined,
        evidenceType: evidenceType || undefined,
        limit: 100,
      });
      setItems(res.data);
    } catch {
      setItems([]);
    } finally {
      setLoading(false);
    }
  }, [client, selectedTenant, frameworkFilter, activeTab]);

  const loadReviews = useCallback(async () => {
    if (!selectedTenant) return;
    setReviewsLoading(true);
    try {
      const res = await client.listComplianceReviews({
        tenantId: selectedTenant,
        limit: 100,
      });
      setReviews(res.data);
    } catch {
      setReviews([]);
    } finally {
      setReviewsLoading(false);
    }
  }, [client, selectedTenant]);

  useEffect(() => {
    void load();
  }, [load]);

  useEffect(() => {
    if (activeTab === 'Reviews') {
      void loadReviews();
    }
  }, [loadReviews, activeTab]);

  // Auto-select first tenant
  useEffect(() => {
    if (!selectedTenant && tenantList.length > 0) {
      setSelectedTenant(tenantList[0].id);
    }
  }, [tenantList, selectedTenant]);

  const handleUpload = async () => {
    if (!selectedTenant || !form.title) {
      setUploadError('Tenant and title are required.');
      return;
    }
    const file = fileRef.current?.files?.[0];
    setUploading(true);
    setUploadError(null);

    const fd = new FormData();
    fd.append('tenant_id', selectedTenant);
    fd.append('title', form.title);
    fd.append('evidence_type', form.evidence_type);
    if (form.framework) fd.append('framework', form.framework);
    if (form.control_ref) fd.append('control_ref', form.control_ref);
    if (form.description) fd.append('description', form.description);
    if (file) fd.append('file', file);

    try {
      await client.uploadComplianceEvidence(fd);
      setForm({ title: '', evidence_type: EVIDENCE_TYPES[0], framework: '', control_ref: '', description: '' });
      if (fileRef.current) fileRef.current.value = '';
      void load();
    } catch (err: unknown) {
      setUploadError(err instanceof Error ? err.message : 'Upload failed');
    } finally {
      setUploading(false);
    }
  };

  const handleDelete = async () => {
    if (!deleteId || !selectedTenant) return;
    try {
      await client.deleteComplianceEvidence(deleteId, selectedTenant);
      void load();
    } finally {
      setDeleteId(null);
    }
  };

  const columns: ColumnDef<CEType, unknown>[] = [
    { accessorKey: 'title', header: 'Title' },
    {
      accessorKey: 'framework',
      header: 'Framework',
      cell: ({ getValue }) => {
        const v = getValue() as string | undefined;
        if (!v) return <span className="text-muted-foreground">—</span>;
        return <StatusTag tone={frameworkTone(v)}>{v}</StatusTag>;
      },
    },
    {
      accessorKey: 'control_ref',
      header: 'Control Ref',
      cell: ({ getValue }) => {
        const v = getValue() as string | undefined;
        return <span>{v ?? '—'}</span>;
      },
    },
    { accessorKey: 'evidence_type', header: 'Type' },
    {
      accessorKey: 'uploaded_at',
      header: 'Uploaded',
      cell: ({ getValue }) => <span>{formatDate(getValue() as string)}</span>,
    },
    {
      id: 'actions',
      header: '',
      cell: ({ row }) => (
        <div className="flex gap-1 justify-end">
          <Button
            variant="ghost"
            size="sm"
            onClick={() => {
              if (!selectedTenant) return;
              window.open(client.buildEvidenceDownloadUrl(row.original.id, selectedTenant), '_blank');
            }}
          >
            <Download className="w-3.5 h-3.5" />
          </Button>
          <Button variant="ghost" size="sm" onClick={() => setDeleteId(row.original.id)}>
            <Trash2 className="w-3.5 h-3.5 text-destructive" />
          </Button>
        </div>
      ),
    },
  ];

  return (
    <div className="flex flex-col gap-4">
      <ConfirmModal
        open={deleteId !== null}
        title="Delete evidence?"
        body="This will permanently remove the evidence record and any attached file."
        variant="danger"
        confirmLabel="Delete"
        onConfirm={() => void handleDelete()}
        onCancel={() => setDeleteId(null)}
      />

      <SectionHeader title="Compliance Evidence" description="Upload and manage evidence for compliance controls." />

      <div className="flex flex-wrap gap-3 items-center">
        <select
          className="border rounded px-3 py-1.5 text-sm bg-background"
          value={selectedTenant}
          onChange={(e) => setSelectedTenant(e.target.value)}
        >
          <option value="">Select tenant...</option>
          {tenantList.map((t) => (
            <option key={t.id} value={t.id}>
              {t.name}
            </option>
          ))}
        </select>
        <select
          className="border rounded px-3 py-1.5 text-sm bg-background"
          value={frameworkFilter}
          onChange={(e) => setFrameworkFilter(e.target.value)}
        >
          <option value="">All frameworks</option>
          {FRAMEWORKS.map((f) => (
            <option key={f} value={f}>
              {f}
            </option>
          ))}
        </select>
        <Button variant="outline" size="sm" onClick={() => void load()}>
          <RefreshCw className="w-3.5 h-3.5 mr-1.5" />
          Refresh
        </Button>
      </div>

      {/* Tabs */}
      <div className="flex gap-1 border-b">
        {TABS.map((tab) => (
          <button
            key={tab}
            className={`px-4 py-2 text-sm font-medium border-b-2 transition-colors ${
              activeTab === tab
                ? 'border-primary text-foreground'
                : 'border-transparent text-muted-foreground hover:text-foreground'
            }`}
            onClick={() => setActiveTab(tab)}
          >
            {tab}
          </button>
        ))}
      </div>

      {activeTab !== 'Reviews' && (
        <>
          {loading ? (
            <div className="text-muted-foreground text-sm py-4">Loading...</div>
          ) : items.length === 0 ? (
            <EmptyState
              title="No evidence found"
              description="Upload evidence to get started."
            />
          ) : (
            <DataTable columns={columns} rows={items} />
          )}

          {/* Upload form */}
          <Panel>
            <div className="p-4 flex flex-col gap-3">
              <h3 className="font-semibold text-sm">Upload evidence</h3>

              {uploadError && (
                <p className="text-sm text-destructive">{uploadError}</p>
              )}

              <div className="grid grid-cols-2 gap-3">
                <div className="flex flex-col gap-1">
                  <label className="text-xs font-medium">Title *</label>
                  <input
                    className="border rounded px-3 py-1.5 text-sm bg-background"
                    value={form.title}
                    onChange={(e) => setForm((p) => ({ ...p, title: e.target.value }))}
                    placeholder="e.g. Q1 Security Training"
                  />
                </div>
                <div className="flex flex-col gap-1">
                  <label className="text-xs font-medium">Evidence type *</label>
                  <select
                    className="border rounded px-3 py-1.5 text-sm bg-background"
                    value={form.evidence_type}
                    onChange={(e) => setForm((p) => ({ ...p, evidence_type: e.target.value }))}
                  >
                    {EVIDENCE_TYPES.map((t) => (
                      <option key={t} value={t}>
                        {t}
                      </option>
                    ))}
                  </select>
                </div>
                <div className="flex flex-col gap-1">
                  <label className="text-xs font-medium">Framework</label>
                  <select
                    className="border rounded px-3 py-1.5 text-sm bg-background"
                    value={form.framework}
                    onChange={(e) => setForm((p) => ({ ...p, framework: e.target.value }))}
                  >
                    <option value="">None</option>
                    {FRAMEWORKS.map((f) => (
                      <option key={f} value={f}>
                        {f}
                      </option>
                    ))}
                  </select>
                </div>
                <div className="flex flex-col gap-1">
                  <label className="text-xs font-medium">Control Ref</label>
                  <input
                    className="border rounded px-3 py-1.5 text-sm bg-background"
                    value={form.control_ref}
                    onChange={(e) => setForm((p) => ({ ...p, control_ref: e.target.value }))}
                    placeholder="e.g. CC6.1"
                  />
                </div>
              </div>

              <div className="flex flex-col gap-1">
                <label className="text-xs font-medium">Description</label>
                <textarea
                  className="border rounded px-3 py-1.5 text-sm bg-background resize-none"
                  rows={2}
                  value={form.description}
                  onChange={(e) => setForm((p) => ({ ...p, description: e.target.value }))}
                  placeholder="Optional description..."
                />
              </div>

              <div className="flex flex-col gap-1">
                <label className="text-xs font-medium">File</label>
                <input
                  ref={fileRef}
                  type="file"
                  className="text-sm"
                  accept=".pdf,.png,.jpg,.jpeg,.docx,.zip"
                />
              </div>

              <div>
                <Button
                  size="sm"
                  onClick={() => void handleUpload()}
                  disabled={uploading || !selectedTenant}
                >
                  <Upload className="w-3.5 h-3.5 mr-1.5" />
                  {uploading ? 'Uploading...' : 'Upload evidence'}
                </Button>
              </div>
            </div>
          </Panel>
        </>
      )}

      {activeTab === 'Reviews' && (
        <>
          <Panel>
            <SectionHeader title="Scheduled Reviews" className="mb-4" />
            {reviewsLoading ? (
              <div className="p-6 text-center text-muted-foreground">Loading...</div>
            ) : reviews.length === 0 ? (
              <EmptyState title="No reviews scheduled" description="Create one below to track compliance reviews." />
            ) : (
              <DataTable<ComplianceReview>
                rows={reviews}
                columns={[
                  { accessorKey: 'review_type', header: 'Review Type' },
                  {
                    accessorKey: 'scheduled_for',
                    header: 'Scheduled For',
                    cell: ({ getValue }) => <span>{formatDate(getValue() as string)}</span>,
                  },
                  {
                    accessorKey: 'status',
                    header: 'Status',
                    cell: ({ getValue }) => {
                      const status = getValue() as string;
                      const tone: StateTone =
                        status === 'completed' ? 'healthy' : status === 'overdue' ? 'critical' : 'warning';
                      return <StatusTag tone={tone}>{status}</StatusTag>;
                    },
                  },
                  { accessorKey: 'recurrence', header: 'Recurrence' },
                  {
                    id: 'actions',
                    header: 'Actions',
                    cell: ({ row }) => (
                      <div className="flex gap-2">
                        {row.original.status !== 'completed' && (
                          <Button
                            size="sm"
                            variant="ghost"
                            onClick={() => setCompleteReviewId(row.original.id)}
                          >
                            Complete
                          </Button>
                        )}
                        <Button
                          size="sm"
                          variant="ghost"
                          className="text-destructive"
                          onClick={() => setDeleteReviewId(row.original.id)}
                        >
                          <Trash2 className="w-4 h-4" />
                        </Button>
                      </div>
                    ),
                  },
                ]}
              />
            )}
          </Panel>

          <Panel>
            <SectionHeader title="Schedule New Review" className="mb-4" />
            <div className="flex flex-col gap-4 max-w-xl">
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                <div className="flex flex-col gap-1">
                  <label className="text-xs font-medium">Review Type *</label>
                  <select
                    className="border rounded px-3 py-1.5 text-sm bg-background"
                    value={reviewForm.review_type}
                    onChange={(e) => setReviewForm((p) => ({ ...p, review_type: e.target.value }))}
                  >
                    <option value="quarterly_access_review">Quarterly Access Review</option>
                    <option value="annual_policy_review">Annual Policy Review</option>
                    <option value="security_assessment">Security Assessment</option>
                    <option value="compliance_audit">Compliance Audit</option>
                    <option value="vendor_review">Vendor Review</option>
                    <option value="other">Other</option>
                  </select>
                </div>
                <div className="flex flex-col gap-1">
                  <label className="text-xs font-medium">Scheduled For *</label>
                  <input
                    type="date"
                    className="border rounded px-3 py-1.5 text-sm bg-background"
                    value={reviewForm.scheduled_for}
                    onChange={(e) => setReviewForm((p) => ({ ...p, scheduled_for: e.target.value }))}
                  />
                </div>
              </div>
              <div className="flex flex-col gap-1">
                <label className="text-xs font-medium">Recurrence</label>
                <select
                  className="border rounded px-3 py-1.5 text-sm bg-background"
                  value={reviewForm.recurrence}
                  onChange={(e) => setReviewForm((p) => ({ ...p, recurrence: e.target.value }))}
                >
                  <option value="">One-time</option>
                  <option value="weekly">Weekly</option>
                  <option value="monthly">Monthly</option>
                  <option value="quarterly">Quarterly</option>
                  <option value="annual">Annual</option>
                </select>
              </div>
              <div className="flex flex-col gap-1">
                <label className="text-xs font-medium">Notes</label>
                <textarea
                  className="border rounded px-3 py-1.5 text-sm bg-background resize-none"
                  rows={2}
                  value={reviewForm.notes}
                  onChange={(e) => setReviewForm((p) => ({ ...p, notes: e.target.value }))}
                  placeholder="Optional notes..."
                />
              </div>
              <div>
                <Button
                  size="sm"
                  onClick={async () => {
                    if (!selectedTenant || !reviewForm.scheduled_for) return;
                    setCreatingReview(true);
                    try {
                      await client.createComplianceReview({
                        tenant_id: selectedTenant,
                        review_type: reviewForm.review_type,
                        scheduled_for: reviewForm.scheduled_for,
                        recurrence: reviewForm.recurrence || undefined,
                        notes: reviewForm.notes || undefined,
                      });
                      setReviewForm({
                        review_type: 'quarterly_access_review',
                        scheduled_for: '',
                        recurrence: '',
                        notes: '',
                      });
                      void loadReviews();
                    } catch (err) {
                      console.error('Failed to create review:', err);
                    } finally {
                      setCreatingReview(false);
                    }
                  }}
                  disabled={creatingReview || !selectedTenant || !reviewForm.scheduled_for}
                >
                  {creatingReview ? 'Creating...' : 'Schedule Review'}
                </Button>
              </div>
            </div>
          </Panel>

          <ConfirmModal
            open={!!completeReviewId}
            onCancel={() => {
              setCompleteReviewId(null);
              setCompleteNotes('');
            }}
            onConfirm={async () => {
              if (!completeReviewId) return;
              try {
                await client.completeComplianceReview(completeReviewId, completeNotes);
                setCompleteReviewId(null);
                setCompleteNotes('');
                void loadReviews();
              } catch (err) {
                console.error('Failed to complete review:', err);
              }
            }}
            title="Complete Review"
            body={completeNotes}
          />

          <ConfirmModal
            open={!!deleteReviewId}
            onCancel={() => setDeleteReviewId(null)}
            onConfirm={async () => {
              if (!deleteReviewId) return;
              try {
                await client.deleteComplianceReview(deleteReviewId);
                setDeleteReviewId(null);
                void loadReviews();
              } catch (err) {
                console.error('Failed to delete review:', err);
              }
            }}
            title="Delete Review"
            body="Are you sure you want to delete this review? This action cannot be undone."
            variant="danger"
          />
        </>
      )}
    </div>
  );
}
