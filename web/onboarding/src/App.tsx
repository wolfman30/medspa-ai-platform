import { useEffect, useState } from 'react';
import { OnboardingWizard } from './components/OnboardingWizard';
import { Dashboard } from './components/Dashboard';
import { getOnboardingStatus } from './api/client';
import { AuthProvider, useAuth, LoginForm } from './auth';
import { getStoredOrgId, setStoredOrgId } from './utils/orgStorage';

type OnboardingDecision = 'idle' | 'loading' | 'ready' | 'not_ready';

function getOrgIdFromUser(user: unknown): string | null {
  if (!user || typeof user !== 'object') return null;
  const record = user as { orgId?: string; org_id?: string };
  return record.orgId || record.org_id || null;
}

function AuthenticatedApp() {
  const { isLoading, isAuthenticated, authEnabled, user, logout } = useAuth();
  const [decision, setDecision] = useState<OnboardingDecision>('idle');
  const [checkedOrgId, setCheckedOrgId] = useState<string | null>(null);
  const [statusRefresh, setStatusRefresh] = useState(0);

  const authReady = !isLoading && (!authEnabled || isAuthenticated);
  const userOrgId = getOrgIdFromUser(user);
  const orgId = userOrgId || getStoredOrgId();

  useEffect(() => {
    if (!authReady || !userOrgId) return;
    setStoredOrgId(userOrgId);
  }, [authReady, userOrgId]);

  useEffect(() => {
    if (!authReady || !orgId) return;
    let isActive = true;
    getOnboardingStatus(orgId)
      .then(status => {
        if (!isActive) return;
        setDecision(status.ready_for_launch ? 'ready' : 'not_ready');
        setCheckedOrgId(orgId);
      })
      .catch(err => {
        if (!isActive) return;
        if (import.meta.env.DEV) {
          console.error('Failed to get onboarding status', err);
        }
        setDecision('not_ready');
        setCheckedOrgId(orgId);
      });
    return () => {
      isActive = false;
    };
  }, [authReady, orgId, statusRefresh]);

  // Show loading state
  if (isLoading) {
    return (
      <div className="min-h-screen flex items-center justify-center">
        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-indigo-600" />
      </div>
    );
  }

  // If auth is enabled but user is not authenticated, show login
  if (authEnabled && !isAuthenticated) {
    return <LoginForm />;
  }

  const showStatusLoading = authReady && orgId && checkedOrgId !== orgId;

  // Show onboarding wizard or dashboard (with optional user header if authenticated)
  return (
    <div>
      {authEnabled && user && (
        <div className="bg-indigo-600 text-white px-4 py-2 flex justify-between items-center">
          <span className="text-sm">Logged in as {user.email}</span>
          <button
            onClick={logout}
            className="text-sm underline hover:no-underline"
          >
            Sign out
          </button>
        </div>
      )}
      {showStatusLoading ? (
        <div className="min-h-screen flex items-center justify-center">
          <span className="text-sm text-gray-600">Checking onboarding status...</span>
        </div>
      ) : decision === 'ready' && orgId ? (
        <Dashboard orgId={orgId} />
      ) : (
        <OnboardingWizard
          orgId={orgId}
          onComplete={() => setStatusRefresh(prev => prev + 1)}
        />
      )}
    </div>
  );
}

function App() {
  return (
    <AuthProvider>
      <AuthenticatedApp />
    </AuthProvider>
  );
}

export default App;
