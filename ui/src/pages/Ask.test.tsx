import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { Ask } from './Ask';
import * as useApiClientModule from '../hooks/useApiClient';
import * as useTenantModule from '../providers/TenantProvider';
import type { APIClient } from '../lib/api';

describe('Ask', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('renders answer citations, confidence, and tool trace', async () => {
    const askAI = vi.fn().mockResolvedValue({
      answer: 'Timeline ready.',
      citations: ['tool:events_query:1'],
      source_citations: ['events:evt-1'],
      confidence: 'cited',
      tool_trace: [
        {
          name: 'events_query',
          citation_id: 'tool:events_query:1',
          ok: true,
          duration_ms: 14,
        },
      ],
    });
    vi.spyOn(useApiClientModule, 'useApiClient').mockReturnValue({ askAI } as unknown as APIClient);
    vi.spyOn(useTenantModule, 'useTenant').mockReturnValue({
      currentTenantId: 'tenant-1',
    } as ReturnType<typeof useTenantModule.useTenant>);

    const user = userEvent.setup();
    render(<Ask />);

    await user.type(screen.getByPlaceholderText(/ask about this fleet's posture/i), 'show timeline');
    await user.click(screen.getByRole('button', { name: /^ask$/i }));

    await waitFor(() => {
      expect(screen.getByText('Timeline ready.')).toBeInTheDocument();
    });
    expect(askAI).toHaveBeenCalledWith('tenant-1', 'show timeline');
    expect(screen.getAllByText('tool:events_query:1').length).toBeGreaterThan(0);
    expect(screen.getByText('Evidence: events:evt-1')).toBeInTheDocument();
    expect(screen.getByText('Events Query')).toBeInTheDocument();
    expect(screen.getByText(/Confidence: cited/i)).toBeInTheDocument();
  });
});
