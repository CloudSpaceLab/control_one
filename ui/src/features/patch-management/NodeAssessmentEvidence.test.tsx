import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import type { NodeAssessment } from '@/lib/api';
import { NodeAssessmentEvidence } from './NodeAssessmentEvidence';

const record: NodeAssessment = {
  id: 'attempt-1', tenant_id: 'tenant-1', node_id: 'node-1', started_at: '2026-10-28T09:00:00Z', completed_at: '2026-10-28T09:00:01Z',
  status: 'partial', current_status: 'expired', current_reasons: ['current_feed_expired'], inventory_generation: 'inventory-a',
  evidence: { coverage: 'not_established', inventory_completeness: 'unknown', package_count: 109, match_count: 0, feed_generation: 'feed-a', feeds: [], reasons: ['complete_product_coverage_not_established'], exclusions: {}, version_schemes: {}, findings_updated: true },
};

describe('Saved node assessment evidence', () => {
  it('keeps zero matches distinct from a clean assessment and separates past outcome from current applicability', () => {
    render(<NodeAssessmentEvidence assessment={record} />);
    expect(screen.getByText('Assessment: expired')).toBeInTheDocument();
    expect(screen.getByText(/109 package inputs · 0 known matches/)).toHaveTextContent('completeness: unknown');
    expect(screen.getByText(/Saved outcome: partial. Current applicability: expired/)).toBeInTheDocument();
    expect(screen.getByText('current feed expired')).toBeInTheDocument();
    expect(screen.getByText(/missing from incomplete inventory/)).toHaveTextContent('does not prove removal');
  });
  it('does not present cached evidence as current after a loading failure', () => {
    render(<NodeAssessmentEvidence assessment={record} loadFailed />);
    expect(screen.getByRole('alert')).toHaveTextContent('could not be loaded');
    expect(screen.queryByText('Assessment: expired')).not.toBeInTheDocument();
  });
  it('explains missing evidence for a node with no saved attempt', () => {
    render(<NodeAssessmentEvidence />);
    expect(screen.getByRole('alert')).toHaveTextContent('No saved assessment attempt');
  });
  it('does not turn a failed attempt into zero assessed findings', () => {
    render(<NodeAssessmentEvidence assessment={{ ...record, evidence: { ...record.evidence, findings_updated: false } }} />);
    expect(screen.getByText(/match result unavailable/)).toBeInTheDocument();
    expect(screen.queryByText(/0 known matches/)).not.toBeInTheDocument();
  });
});
