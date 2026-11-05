import {render, screen} from '@testing-library/react';
import {MemoryRouter} from 'react-router-dom';
import {expect, it, vi} from 'vitest';
import {Sidebar} from './Sidebar';

vi.mock('./AlertStatusBadge', () => ({AlertStatusBadge: () => null}));
vi.mock('./NodeStatusBadge', () => ({NodeStatusBadge: () => null}));

it.each(['ciso', 'viewer'])('makes patch review discoverable for %s without global role management', role => {
  render(<MemoryRouter><Sidebar userRoles={[role]} variant="sheet" /></MemoryRouter>);
  expect(screen.getByRole('link', {name: 'Patch posture'})).toHaveAttribute('href', '/infrastructure/patch');
  expect(screen.queryByRole('link', {name: 'Roles & permissions'})).not.toBeInTheDocument();
});
