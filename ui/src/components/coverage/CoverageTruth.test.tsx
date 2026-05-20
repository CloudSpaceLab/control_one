import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { CoverageTruthBadge, summarizeCoverageRows } from './CoverageTruth';

describe('coverage truth presentation', () => {
  it('does not count unsupported or not-applicable rows as passing', () => {
    const summary = summarizeCoverageRows([
      { domain: 'compliance', name: 'Password rotation', state: 'supported' },
      { domain: 'compliance', name: 'Privileged account review', state: 'unsupported' },
      { domain: 'parser', name: 'Oracle audit parser', state: 'not_applicable' },
    ]);

    expect(summary.supported).toBe(1);
    expect(summary.unsupported).toBe(1);
    expect(summary.notApplicable).toBe(1);
    expect(summary.applicable).toBe(2);
    expect(summary.passingPercent).toBe(50);
  });

  it('renders unsupported coverage as an explicit badge', () => {
    render(<CoverageTruthBadge state="unsupported" />);

    expect(screen.getByText('Unsupported')).toBeInTheDocument();
  });
});
