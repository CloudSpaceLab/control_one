import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { AssigneePicker } from './AssigneePicker';

class ResizeObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
}

vi.stubGlobal('ResizeObserver', ResizeObserver);
HTMLElement.prototype.scrollIntoView = vi.fn();

const users = [
  { id: 'ada', name: 'Ada Lovelace', email: 'ada@example.com' },
  { id: 'grace', name: 'Grace Hopper', email: 'grace@example.com' },
];

describe('AssigneePicker', () => {
  it('filters team members, selects an assignee, and clears the selection', async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();

    render(<AssigneePicker value={null} options={users} onChange={onChange} />);

    expect(screen.getByRole('combobox', { name: 'Assignee' })).toHaveTextContent('Unassigned');

    await user.click(screen.getByRole('combobox', { name: 'Assignee' }));
    expect(screen.getByRole('dialog', { name: 'Assignee picker' })).toBeInTheDocument();
    expect(screen.getByRole('combobox', { name: 'Search assignees' })).toBeInTheDocument();
    await user.type(screen.getByRole('combobox', { name: 'Search assignees' }), 'grace');

    expect(screen.getByRole('option', { name: /grace hopper/i })).toBeInTheDocument();
    expect(screen.queryByRole('option', { name: /ada lovelace/i })).not.toBeInTheDocument();

    await user.click(screen.getByRole('option', { name: /grace hopper/i }));
    expect(onChange).toHaveBeenCalledWith(users[1]);

    render(<AssigneePicker value={users[1]} options={users} onChange={onChange} />);
    await user.click(screen.getByRole('button', { name: 'Clear assignee' }));

    expect(onChange).toHaveBeenLastCalledWith(null);
  });
});
