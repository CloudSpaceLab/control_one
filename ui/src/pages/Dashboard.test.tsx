import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import { Dashboard } from './Dashboard';

describe('Dashboard', () => {
  it('renders welcome copy', () => {
    render(<Dashboard />);
    expect(screen.getByRole('heading', { name: /dashboard/i })).toBeInTheDocument();
    expect(
      screen.getByText(/select a module from the navigation to get started/i),
    ).toBeInTheDocument();
  });
});
