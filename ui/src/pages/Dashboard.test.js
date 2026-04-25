import { jsx as _jsx } from "react/jsx-runtime";
import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { Dashboard } from './Dashboard';
import * as useApiClientModule from '../hooks/useApiClient';
import * as useTenantsModule from '../hooks/useTenants';
const overview = {
    generated_at: new Date().toISOString(),
    node_counts: { total: 3, healthy: 2, offline: 1 },
    security_event_counts: { critical: 1, high: 2, medium: 0, low: 0, total: 3 },
    health_incident_counts: { critical: 0, high: 1, medium: 0, low: 0, total: 1 },
    compliance_summary: { total: 10, passed: 8, failed: 2 },
    rule_trigger_counts_24h: { port: 4, log: 1 },
    remediations_applied_24h: 2,
};
function stubClient() {
    return {
        getDashboardOverview: vi.fn().mockResolvedValue(overview),
        streamEvents: vi.fn().mockReturnValue(() => undefined),
    };
}
describe('Dashboard', () => {
    beforeEach(() => {
        vi.spyOn(useApiClientModule, 'useApiClient').mockReturnValue(stubClient());
        vi.spyOn(useTenantsModule, 'useTenants').mockReturnValue({
            data: [{ id: 'tenant-1', name: 't', created_at: '2024-01-01', updated_at: '2024-01-01' }],
            pagination: { total: 1, count: 1, limit: 1, offset: 0, nextOffset: null, prevOffset: null },
            loading: false,
            error: null,
            refresh: vi.fn(),
        });
    });
    afterEach(() => {
        vi.restoreAllMocks();
    });
    it('renders the 5-section overview with totals from /dashboard/overview', async () => {
        render(_jsx(MemoryRouter, { children: _jsx(Dashboard, {}) }));
        await waitFor(() => {
            expect(screen.getByText(/Security events/)).toBeInTheDocument();
        });
        expect(screen.getByText(/Open health incidents/)).toBeInTheDocument();
        expect(screen.getByText(/Compliance alerts/)).toBeInTheDocument();
        expect(screen.getByText(/Rule triggers/)).toBeInTheDocument();
        expect(screen.getByText(/Auto-remediations/)).toBeInTheDocument();
    });
});
