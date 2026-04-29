import { lazy, Suspense } from 'react';
import { Navigate, Route, Routes } from 'react-router-dom';
import { MainLayout } from './components/MainLayout';
import { Login } from './pages/Login';
import { AuthCallback } from './pages/AuthCallback';
import { useAuth } from './providers/AuthProvider';
import { Skeleton } from './components/ui/skeleton';

// Eager — every authenticated visit lands on the dashboard.
import { DashboardRouter } from './pages/role-dashboards';

// Lazy — split each feature area into its own chunk.
const InvestigateHome = lazy(() => import('./pages/investigate').then((m) => ({ default: m.InvestigateHome })));
const SearchResults = lazy(() => import('./pages/investigate/SearchResults').then((m) => ({ default: m.SearchResults })));
const EntityDetail = lazy(() => import('./pages/investigate/EntityDetail').then((m) => ({ default: m.EntityDetail })));
const SavedSearches = lazy(() => import('./pages/investigate/Saved').then((m) => ({ default: m.SavedSearches })));

const Tenants = lazy(() => import('./pages/Tenants').then((m) => ({ default: m.Tenants })));
const Nodes = lazy(() => import('./pages/Nodes').then((m) => ({ default: m.Nodes })));
const FleetEnroll = lazy(() => import('./pages/FleetEnroll').then((m) => ({ default: m.FleetEnroll })));
const Hypervisors = lazy(() => import('./pages/Hypervisors').then((m) => ({ default: m.Hypervisors })));
const Jobs = lazy(() => import('./pages/Jobs').then((m) => ({ default: m.Jobs })));
const Templates = lazy(() => import('./pages/Templates').then((m) => ({ default: m.Templates })));
const Compliance = lazy(() => import('./pages/Compliance').then((m) => ({ default: m.Compliance })));
const Rules = lazy(() => import('./pages/Rules').then((m) => ({ default: m.Rules })));
const Alerts = lazy(() => import('./pages/Alerts').then((m) => ({ default: m.Alerts })));
const Access = lazy(() => import('./pages/Access').then((m) => ({ default: m.Access })));
const Recommendations = lazy(() => import('./pages/Recommendations').then((m) => ({ default: m.Recommendations })));
const Reports = lazy(() => import('./pages/Reports').then((m) => ({ default: m.Reports })));
const ThreatFeeds = lazy(() => import('./pages/ThreatFeeds').then((m) => ({ default: m.ThreatFeeds })));
const Sessions = lazy(() => import('./pages/Sessions').then((m) => ({ default: m.Sessions })));
const Connections = lazy(() => import('./pages/Connections').then((m) => ({ default: m.Connections })));
const Dashboards = lazy(() => import('./pages/Dashboards').then((m) => ({ default: m.Dashboards })));
const Roles = lazy(() => import('./pages/Roles').then((m) => ({ default: m.Roles })));
const Audit = lazy(() => import('./pages/Audit').then((m) => ({ default: m.Audit })));
const Users = lazy(() => import('./pages/Users').then((m) => ({ default: m.Users })));
const Telemetry = lazy(() => import('./pages/Telemetry').then((m) => ({ default: m.Telemetry })));
const Secrets = lazy(() => import('./pages/Secrets').then((m) => ({ default: m.Secrets })));
const OfflineBundle = lazy(() => import('./pages/OfflineBundle').then((m) => ({ default: m.OfflineBundle })));
const Settings = lazy(() => import('./pages/Settings').then((m) => ({ default: m.Settings })));
const Onboard = lazy(() => import('./pages/Onboard').then((m) => ({ default: m.Onboard })));
const Behavioral = lazy(() => import('./pages/Behavioral').then((m) => ({ default: m.Behavioral })));
const DataSecurity = lazy(() => import('./pages/DataSecurity').then((m) => ({ default: m.DataSecurity })));
const ComplianceEvidence = lazy(() => import('./pages/ComplianceEvidence').then((m) => ({ default: m.ComplianceEvidence })));
const AuditReports = lazy(() => import('./pages/AuditReports').then((m) => ({ default: m.AuditReports })));
const Frameworks = lazy(() => import('./pages/Frameworks').then((m) => ({ default: m.Frameworks })));

function PageFallback(): JSX.Element {
  return (
    <div className="flex flex-col gap-3 p-6">
      <Skeleton className="h-8 w-48" />
      <Skeleton className="h-4 w-96" />
      <div className="grid grid-cols-12 gap-4">
        <Skeleton className="col-span-3 h-24" />
        <Skeleton className="col-span-3 h-24" />
        <Skeleton className="col-span-3 h-24" />
        <Skeleton className="col-span-3 h-24" />
        <Skeleton className="col-span-12 h-64" />
      </div>
    </div>
  );
}

export function App(): JSX.Element {
  const { isAuthenticated } = useAuth();

  return (
    <Routes>
      <Route path="/login" element={<Login />} />
      <Route path="/auth/callback" element={<AuthCallback />} />
      <Route
        path="/"
        element={isAuthenticated ? <MainLayout /> : <Navigate to="/login" replace />}
      >
        <Route index element={<DashboardRouter />} />
        <Route
          path="*"
          element={
            <Suspense fallback={<PageFallback />}>
              <Routes>
                <Route path="onboard" element={<Onboard />} />
                <Route path="search" element={<SearchResults />} />
                <Route path="investigate" element={<InvestigateHome />} />
                <Route path="investigate/saved" element={<SavedSearches />} />
                <Route path="investigate/:type/:id" element={<EntityDetail />} />
                <Route path="tenants" element={<Tenants />} />
                <Route path="nodes" element={<Nodes />} />
                <Route path="fleet-enroll" element={<FleetEnroll />} />
                <Route path="hypervisors" element={<Hypervisors />} />
                <Route path="jobs" element={<Jobs />} />
                <Route path="templates" element={<Templates />} />
                <Route path="compliance" element={<Compliance />} />
                <Route path="rules" element={<Rules />} />
                <Route path="rules/builder" element={<Navigate to="/rules" replace />} />
                <Route path="alerts" element={<Alerts />} />
                <Route path="access" element={<Access />} />
                <Route path="recommendations" element={<Recommendations />} />
                <Route path="reports" element={<Reports />} />
                <Route path="threat-feeds" element={<ThreatFeeds />} />
                <Route path="sessions" element={<Sessions />} />
                <Route path="connections" element={<Connections />} />
                <Route path="dashboards" element={<Dashboards />} />
                <Route path="roles" element={<Roles />} />
                <Route path="audit" element={<Audit />} />
                <Route path="users" element={<Users />} />
                <Route path="telemetry" element={<Telemetry />} />
                <Route path="secrets" element={<Secrets />} />
                <Route path="offline-bundle" element={<OfflineBundle />} />
                <Route path="settings" element={<Settings />} />
                <Route path="behavioral" element={<Behavioral />} />
                <Route path="data-security" element={<DataSecurity />} />
                <Route path="compliance-evidence" element={<ComplianceEvidence />} />
                <Route path="audit-reports" element={<AuditReports />} />
                <Route path="frameworks" element={<Frameworks />} />
              </Routes>
            </Suspense>
          }
        />
      </Route>
      <Route path="*" element={<Navigate to={isAuthenticated ? '/' : '/login'} replace />} />
    </Routes>
  );
}
