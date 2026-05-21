import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { describe, expect, it } from 'vitest';
import { Observability } from './Observability';

describe('Observability', () => {
  it('translates connector states into operator actions', () => {
    render(
      <MemoryRouter>
        <Observability />
      </MemoryRouter>,
    );

    expect(screen.getByRole('heading', { name: 'Guided setup' })).toBeInTheDocument();
    expect(screen.getByText('PostgreSQL core')).toBeInTheDocument();
    expect(screen.getAllByText('Needs access').length).toBeGreaterThan(0);
    expect(screen.getByText(/Create PostgreSQL read-only audit user/i)).toBeInTheDocument();
    expect(screen.getAllByText('Unsupported').length).toBeGreaterThan(0);
    expect(screen.getByText(/cannot count as healthy coverage/i)).toBeInTheDocument();
  });

  it('blocks debug start until safety fields are explicit and opens Knowledge Tree citations', async () => {
    const user = userEvent.setup();
    render(
      <MemoryRouter>
        <Observability />
      </MemoryRouter>,
    );

    await user.click(screen.getByRole('button', { name: /debug session/i }));
    const dialog = screen.getByRole('dialog');
    expect(within(dialog).getByRole('button', { name: /start preview/i })).toBeDisabled();

    await user.type(within(dialog).getByLabelText(/scope/i), 'payments-api');
    await user.type(within(dialog).getByLabelText(/ttl minutes/i), '30');
    await user.type(within(dialog).getByLabelText(/reason/i), 'Latency investigation');
    await user.type(within(dialog).getByLabelText(/quota/i), '100 MB');
    await user.type(within(dialog).getByLabelText(/rollback plan/i), 'Restore baseline log level');
    await user.click(within(dialog).getByLabelText(/approval state reviewed/i));
    expect(within(dialog).getByRole('button', { name: /start preview/i })).toBeEnabled();

    await user.click(within(dialog).getByRole('button', { name: /close/i }));
    await user.click(screen.getByRole('button', { name: /Raw-only parser state/i }));
    expect(screen.getByText(/skipped summary generation/i)).toBeInTheDocument();
    expect(screen.getByText('raw_logs:celery:task-retry')).toBeInTheDocument();
  });
});
