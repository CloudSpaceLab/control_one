import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useEffect, useState } from 'react';
import { SetupWizard } from '../components/SetupWizard';
import { useAuth } from '../providers/AuthProvider';
import { useTenants } from '../hooks/useTenants';
export function Setup() {
    const { isAuthenticated } = useAuth();
    const { pagination: tenantPagination, loading: tenantsLoading } = useTenants({ limit: 1, offset: 0 });
    const [showWizard, setShowWizard] = useState(false);
    useEffect(() => {
        // Only show the wizard if user is authenticated and there are no tenants
        if (isAuthenticated && !tenantsLoading && tenantPagination.total === 0) {
            setShowWizard(true);
        }
        else if (tenantPagination.total > 0) {
            setShowWizard(false);
        }
    }, [isAuthenticated, tenantPagination.total, tenantsLoading]);
    if (!isAuthenticated) {
        return (_jsxs("section", { children: [_jsx("h2", { children: "Setup Required" }), _jsx("p", { children: "Please sign in to access the setup wizard." })] }));
    }
    if (tenantsLoading) {
        return (_jsxs("section", { children: [_jsx("h2", { children: "Loading..." }), _jsx("p", { children: "Checking your setup status..." })] }));
    }
    if (tenantPagination.total > 0) {
        return (_jsxs("section", { children: [_jsx("h2", { children: "Setup Complete" }), _jsxs("p", { children: ["Your Control One environment is already configured. You have ", tenantPagination.total, " tenant(s) set up."] }), _jsx("p", { children: "Use the navigation menu to manage your infrastructure." })] }));
    }
    if (!showWizard) {
        return (_jsxs("section", { children: [_jsx("h2", { children: "Initializing Setup..." }), _jsx("p", { children: "Preparing your setup wizard..." })] }));
    }
    return _jsx(SetupWizard, {});
}
