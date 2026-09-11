import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { WhistleblowerIntake } from './WhistleblowerIntake';

const apiMocks = vi.hoisted(() => ({
  fetchMisconductChallenge: vi.fn(),
  submitWhistleblowerReport: vi.fn(),
}));

vi.mock('../lib/api', () => ({
  fetchMisconductChallenge: apiMocks.fetchMisconductChallenge,
  submitWhistleblowerReport: apiMocks.submitWhistleblowerReport,
}));

describe('WhistleblowerIntake', () => {
  beforeEach(() => {
    apiMocks.fetchMisconductChallenge.mockResolvedValue({ challenge: 'challenge-', difficulty: 0 });
    apiMocks.submitWhistleblowerReport.mockResolvedValue({
      token: 'misconduct-token-123',
      message: 'Report received.',
    });
    vi.stubGlobal('crypto', {
      subtle: {
        digest: vi.fn().mockResolvedValue(new Uint8Array([0]).buffer),
      },
    });
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.resetAllMocks();
  });

  it('keeps the status link inside the console basename after submission', async () => {
    render(
      <MemoryRouter basename="/console" initialEntries={['/console/intake']}>
        <Routes>
          <Route path="/intake" element={<WhistleblowerIntake />} />
        </Routes>
      </MemoryRouter>,
    );

    await userEvent.type(
      await screen.findByLabelText('Description'),
      'A complete anonymous report with enough context.',
    );
    await waitFor(() => expect(screen.getByRole('button', { name: 'Submit report' })).toBeEnabled());
    await userEvent.click(screen.getByRole('button', { name: 'Submit report' }));

    expect(await screen.findByRole('heading', { name: 'Report submitted' })).toBeInTheDocument();
    expect(screen.getByText('misconduct-token-123')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Check status with your token' })).toHaveAttribute(
      'href',
      '/console/intake-status',
    );
  });
});
