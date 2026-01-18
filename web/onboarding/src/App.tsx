import { useEffect, useState } from 'react';
import { OnboardingWizard } from './components/OnboardingWizard';
import { Dashboard } from './components/Dashboard';
import { CampaignRegistration } from './components/CampaignRegistration';
import { ConversationList } from './components/ConversationList';
import { ConversationDetail } from './components/ConversationDetail';
import { getOnboardingStatus } from './api/client';
import { AuthProvider, useAuth, LoginForm } from './auth';
import { getStoredOrgId, setStoredOrgId } from './utils/orgStorage';

type OnboardingDecision = 'idle' | 'loading' | 'ready' | 'not_ready';
type AppView = 'dashboard' | 'conversations' | 'conversation-detail';

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
  const [view, setView] = useState<AppView>('dashboard');
  const [selectedConversationId, setSelectedConversationId] = useState<string | null>(null);

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
          <div className="flex items-center gap-4">
            <span className="text-sm">Logged in as {user.email}</span>
            {decision === 'ready' && orgId && (
              <nav className="flex gap-2">
                <button
                  onClick={() => setView('dashboard')}
                  className={`text-sm px-2 py-1 rounded ${view === 'dashboard' ? 'bg-indigo-500' : 'hover:bg-indigo-500'}`}
                >
                  Dashboard
                </button>
                <button
                  onClick={() => { setView('conversations'); setSelectedConversationId(null); }}
                  className={`text-sm px-2 py-1 rounded ${view === 'conversations' || view === 'conversation-detail' ? 'bg-indigo-500' : 'hover:bg-indigo-500'}`}
                >
                  Conversations
                </button>
              </nav>
            )}
          </div>
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
        view === 'conversation-detail' && selectedConversationId ? (
          <ConversationDetail
            orgId={orgId}
            conversationId={selectedConversationId}
            onBack={() => { setView('conversations'); setSelectedConversationId(null); }}
          />
        ) : view === 'conversations' ? (
          <ConversationList
            orgId={orgId}
            onSelect={(id) => { setSelectedConversationId(id); setView('conversation-detail'); }}
          />
        ) : (
          <Dashboard orgId={orgId} />
        )
      ) : (
        <OnboardingWizard
          orgId={orgId}
          onComplete={() => setStatusRefresh(prev => prev + 1)}
        />
      )}
    </div>
  );
}

function ConversationsPreview() {
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const orgId = new URLSearchParams(window.location.search).get('orgId') || 'preview-org';

  if (selectedId) {
    return (
      <ConversationDetail
        orgId={orgId}
        conversationId={selectedId}
        onBack={() => setSelectedId(null)}
      />
    );
  }

  return (
    <ConversationList
      orgId={orgId}
      onSelect={setSelectedId}
    />
  );
}

function App() {
  // Preview mode for component development - add ?preview=campaign or ?preview=conversations to URL
  const params = new URLSearchParams(window.location.search);
  const preview = params.get('preview');

  if (preview === 'campaign') {
    return (
      <div className="min-h-screen bg-gray-50 py-8">
        <div className="max-w-2xl mx-auto px-4">
          <div className="bg-white rounded-lg shadow p-6">
            <CampaignRegistration
              orgId="preview-org"
              brandId="preview-brand-id"
              onBack={() => alert('Back clicked')}
              onComplete={(id) => alert(`Campaign created: ${id}`)}
            />
          </div>
        </div>
      </div>
    );
  }

  if (preview === 'conversations') {
    return <ConversationsPreview />;
  }

  return (
    <AuthProvider>
      <AuthenticatedApp />
    </AuthProvider>
  );
}

export default App;
