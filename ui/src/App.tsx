import { lazy, Suspense } from 'react';
import { Navigate, Route, Routes } from 'react-router-dom';
import { MainLayout } from './components/MainLayout';
import { Login } from './pages/Login';
import { AuthCallback } from './pages/AuthCallback';
import { useAuth } from './providers/AuthProvider';
import { Skeleton } from './components/ui/skeleton';

// Eager: every authenticated visit lands on the Control Room.
import { ControlRoom } from './pages/ControlRoom';
import { ControlRoomDrilldown } from './pages/ControlRoomDrilldown';

// Lazy — split each feature area into its own chunk.
const InvestigateHome = lazy(() => import('./pages/investigate').then((m) => ({ default: m.InvestigateHome })));
const SearchResults = lazy(() => import('./pages/investigate/SearchResults').then((m) => ({ default: m.SearchResults })));
const EntityDetail = lazy(() => import('./pages/investigate/EntityDetail').then((m) => ({ default: m.EntityDetail })));
const SavedSearches = lazy(() => import('./pages/investigate/Saved').then((m) => ({ default: m.SavedSearches })));
const KnowledgeGraph = lazy(() => import('./pages/investigate/KnowledgeGraph').then((m) => ({ default: m.KnowledgeGraph })));
const IpCompare = lazy(() => import('./pages/investigate/IpCompare').then((m) => ({ default: m.IpCompare })));
const Ask = lazy(() => import('./pages/Ask').then((m) => ({ default: m.Ask })));

const Tenants = lazy(() => import('./pages/Tenants').then((m) => ({ default: m.Tenants })));
const Nodes = lazy(() => import('./pages/Nodes').then((m) => ({ default: m.Nodes })));
const NodeDetail = lazy(() => import('./pages/NodeDetail').then((m) => ({ default: m.NodeDetail })));
const FleetEnroll = lazy(() => import('./pages/FleetEnroll').then((m) => ({ default: m.FleetEnroll })));
const Hypervisors = lazy(() => import('./pages/Hypervisors').then((m) => ({ default: m.Hypervisors })));
const Jobs = lazy(() => import('./pages/Jobs').then((m) => ({ default: m.Jobs })));
const Templates = lazy(() => import('./pages/Templates').then((m) => ({ default: m.Templates })));
const Compliance = lazy(() => import('./pages/Compliance').then((m) => ({ default: m.Compliance })));
const Rules = lazy(() => import('./pages/Rules').then((m) => ({ default: m.Rules })));
const Alerts = lazy(() => import('./pages/Alerts').then((m) => ({ default: m.Alerts })));
const Access = lazy(() => import('./pages/Access').then((m) => ({ default: m.Access })));
const Sessions = lazy(() => import('./pages/Sessions').then((m) => ({ default: m.Sessions })));
const NetworkSecurity = lazy(() => import('./pages/NetworkSecurity').then((m) => ({ default: m.NetworkSecurity })));
const WebserverAutoControl = lazy(() => import('./pages/WebserverAutoControl').then((m) => ({ default: m.WebserverAutoControl })));
const PatchManagement = lazy(() => import('./pages/PatchManagement').then((m) => ({ default: m.PatchManagement })));
const Roles = lazy(() => import('./pages/Roles').then((m) => ({ default: m.Roles })));
const Audit = lazy(() => import('./pages/Audit').then((m) => ({ default: m.Audit })));
const Users = lazy(() => import('./pages/Users').then((m) => ({ default: m.Users })));
const Telemetry = lazy(() => import('./pages/Telemetry').then((m) => ({ default: m.Telemetry })));
const Secrets = lazy(() => import('./pages/Secrets').then((m) => ({ default: m.Secrets })));
const OfflineBundle = lazy(() => import('./pages/OfflineBundle').then((m) => ({ default: m.OfflineBundle })));
const Settings = lazy(() => import('./pages/Settings').then((m) => ({ default: m.Settings })));
const Onboard = lazy(() => import('./pages/Onboard').then((m) => ({ default: m.Onboard })));
const DataSecurity = lazy(() => import('./pages/DataSecurity').then((m) => ({ default: m.DataSecurity })));
const TrustCenter = lazy(() => import('./pages/TrustCenter').then((m) => ({ default: m.TrustCenter })));
const WhistleblowerIntake = lazy(() => import('./pages/WhistleblowerIntake').then((m) => ({ default: m.WhistleblowerIntake })));
const WhistleblowerStatus = lazy(() => import('./pages/WhistleblowerStatus').then((m) => ({ default: m.WhistleblowerStatus })));
const Misconduct = lazy(() => import('./pages/Misconduct').then((m) => ({ default: m.Misconduct })));
const FinacleProfiles = lazy(() => import('./pages/FinacleProfiles').then((m) => ({ default: m.FinacleProfiles })));

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
      {/* Public Trust Center - no authentication required */}
      <Route
        path="/trust/:tenantSlug"
        element={
          <Suspense fallback={<PageFallback />}>
            <TrustCenter />
          </Suspense>
        }
      />
      {/* Public misconduct intake (UC7) - no authentication required.
          Mirrors the trust-center pattern; rate limits + PoW are enforced
          server-side. */}
      <Route
        path="/intake"
        element={
          <Suspense fallback={<PageFallback />}>
            <WhistleblowerIntake />
          </Suspense>
        }
      />
      <Route
        path="/intake-status"
        element={
          <Suspense fallback={<PageFallback />}>
            <WhistleblowerStatus />
          </Suspense>
        }
      />
      <Route
        path="/"
        element={isAuthenticated ? <MainLayout /> : <Navigate to="/login" replace />}
      >
        <Route index element={<ControlRoom />} />
        <Route
          path="*"
          element={
            <Suspense fallback={<PageFallback />}>
              <Routes>
                <Route path="onboard" element={<Onboard />} />
                <Route path="control-room" element={<ControlRoom />} />
                <Route path="control-room/:laneId" element={<ControlRoomDrilldown />} />
                <Route path="search" element={<SearchResults />} />
                <Route path="investigate" element={<InvestigateHome />} />
                <Route path="investigate/saved" element={<SavedSearches />} />
                <Route path="investigate/knowledge-graph" element={<KnowledgeGraph />} />
                <Route path="investigate/:type/:id" element={<EntityDetail />} />
                <Route path="investigate/ip/:id/compare" element={<IpCompare />} />
                <Route path="ask" element={<Ask />} />
                <Route path="tenants" element={<Tenants />} />
                <Route path="nodes" element={<Nodes />} />
                <Route path="nodes/:id" element={<NodeDetail />} />
                <Route path="fleet-enroll" element={<FleetEnroll />} />
                <Route path="hypervisors" element={<Hypervisors />} />
                <Route path="jobs" element={<Jobs />} />
                <Route path="templates" element={<Templates />} />
                <Route path="compliance" element={<Compliance />} />
                <Route path="rules" element={<Rules />} />
                <Route path="rules/builder" element={<Navigate to="/rules" replace />} />
                <Route path="alerts" element={<Alerts />} />
                <Route path="access" element={<Access />} />
                <Route path="recommendations" element={<Navigate to="/rules?tab=drafts" replace />} />
                <Route path="reports" element={<Navigate to="/compliance?tab=reports" replace />} />
                {/* Network Security (PR 3) — consolidated tab surface. */}
                <Route path="security/network" element={<NetworkSecurity />} />
                <Route path="security/webservers" element={<WebserverAutoControl />} />
                {/* Patch Management (PR 4) */}
                <Route path="infrastructure/patch" element={<PatchManagement />} />
                {/* Legacy routes redirect to the consolidated page, mapping their
                    landing tab. Query params from the old URL drop here. */}
                <Route path="threat-feeds" element={<Navigate to="/security/network?tab=threats" replace />} />
                <Route path="connections" element={<Navigate to="/security/network?tab=connections" replace />} />
                <Route path="sessions" element={<Sessions />} />
                <Route path="dashboards" element={<Navigate to="/control-room" replace />} />
                <Route path="roles" element={<Roles />} />
                <Route path="audit" element={<Audit />} />
                <Route path="users" element={<Users />} />
                <Route path="telemetry" element={<Telemetry />} />
                <Route path="secrets" element={<Secrets />} />
                <Route path="offline-bundle" element={<OfflineBundle />} />
                <Route path="settings" element={<Settings />} />
                <Route path="behavioral" element={<Navigate to="/security/network?tab=ip-behavior" replace />} />
                <Route path="data-security" element={<DataSecurity />} />
                <Route path="compliance-evidence" element={<Navigate to="/compliance?tab=evidence" replace />} />
                <Route path="audit-reports" element={<Navigate to="/audit?tab=reports" replace />} />
                <Route path="frameworks" element={<Navigate to="/compliance?tab=frameworks" replace />} />
                <Route path="misconduct" element={<Misconduct />} />
                <Route path="access/finacle" element={<FinacleProfiles />} />
              </Routes>
            </Suspense>
          }
        />
      </Route>
      <Route path="*" element={<Navigate to={isAuthenticated ? '/' : '/login'} replace />} />
    </Routes>
  );
}
