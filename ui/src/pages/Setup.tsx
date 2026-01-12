import { useEffect, useState } from 'react';
import { SetupWizard } from '../components/SetupWizard';
import { useAuth } from '../providers/AuthProvider';
import { useTenants } from '../hooks/useTenants';

export function Setup(): JSX.Element {
  const { isAuthenticated } = useAuth();
  const { pagination: tenantPagination, loading: tenantsLoading } = useTenants({ limit: 1, offset: 0 });
  const [showWizard, setShowWizard] = useState(false);

  useEffect(() => {
    // Only show the wizard if user is authenticated and there are no tenants
    if (isAuthenticated && !tenantsLoading && tenantPagination.total === 0) {
      setShowWizard(true);
    } else if (tenantPagination.total > 0) {
      setShowWizard(false);
    }
  }, [isAuthenticated, tenantPagination.total, tenantsLoading]);

  if (!isAuthenticated) {
    return (
      <section>
        <h2>Setup Required</h2>
        <p>Please sign in to access the setup wizard.</p>
      </section>
    );
  }

  if (tenantsLoading) {
    return (
      <section>
        <h2>Loading...</h2>
        <p>Checking your setup status...</p>
      </section>
    );
  }

  if (tenantPagination.total > 0) {
    return (
      <section>
        <h2>Setup Complete</h2>
        <p>Your Control One environment is already configured. You have {tenantPagination.total} tenant(s) set up.</p>
        <p>Use the navigation menu to manage your infrastructure.</p>
      </section>
    );
  }

  if (!showWizard) {
    return (
      <section>
        <h2>Initializing Setup...</h2>
        <p>Preparing your setup wizard...</p>
      </section>
    );
  }

  return <SetupWizard />;
}
