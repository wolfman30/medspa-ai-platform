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

// Admin users can view all orgs
const ADMIN_EMAILS = ['andrew@aiwolfsolutions.com', 'wolfpassion20@gmail.com'];

function isAdminUser(email: string | undefined): boolean {
  return !!email && ADMIN_EMAILS.includes(email.toLowerCase());
}

function getOrgIdFromUser(user: unknown): string | null {
  if (!user || typeof user !== 'object') return null;
  const record = user as { orgId?: string; org_id?: string };
  return record.orgId || record.org_id || null;
}

// Known orgs for admin quick access
const KNOWN_ORGS = [
  { id: 'bb507f20-7fcc-4941-9eac-9ed93b7834ed', name: 'Botox by Audrey (Dev)' },
];

function AuthenticatedApp() {
  const { isLoading, isAuthenticated, authEnabled, user, logout } = useAuth();
  const [decision, setDecision] = useState<OnboardingDecision>('idle');
  const [checkedOrgId, setCheckedOrgId] = useState<string | null>(null);
  const [statusRefresh, setStatusRefresh] = useState(0);
  const [view, setView] = useState<AppView>('dashboard');
  const [selectedConversationId, setSelectedConversationId] = useState<string | null>(null);
  const [adminOrgId, setAdminOrgId] = useState<string>(KNOWN_ORGS[0]?.id || '');

  const authReady = !isLoading && (!authEnabled || isAuthenticated);
  const userOrgId = getOrgIdFromUser(user);
  const isAdmin = isAdminUser(user?.email);
  // Admins can switch orgs; regular users use their assigned org
  const orgId = isAdmin ? adminOrgId : (userOrgId || getStoredOrgId());

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

  const handleAdminOrgChange = (newOrgId: string) => {
    setAdminOrgId(newOrgId);
    setSelectedConversationId(null);
    setDecision('idle');
    setCheckedOrgId(null);
  };

  // Show onboarding wizard or dashboard (with optional user header if authenticated)
  return (
    <div>
      {/* Admin header with org selector */}
      {authEnabled && user && isAdmin && (
        <OrgSelector currentOrgId={adminOrgId} onOrgChange={handleAdminOrgChange} />
      )}
      {authEnabled && user && (
        <div className="bg-indigo-600 text-white px-4 py-2 flex justify-between items-center">
          <div className="flex items-center gap-4">
            <span className="text-sm">
              {isAdmin ? '(Admin) ' : ''}Logged in as {user.email}
            </span>
            {(isAdmin || (decision === 'ready' && orgId)) && (
              <nav className="flex gap-2">
                {!isAdmin && (
                  <button
                    onClick={() => setView('dashboard')}
                    className={`text-sm px-2 py-1 rounded ${view === 'dashboard' ? 'bg-indigo-500' : 'hover:bg-indigo-500'}`}
                  >
                    Dashboard
                  </button>
                )}
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
      {/* Admin view - direct access to conversations */}
      {isAdmin && orgId ? (
        view === 'conversation-detail' && selectedConversationId ? (
          <ConversationDetail
            orgId={orgId}
            conversationId={selectedConversationId}
            onBack={() => { setView('conversations'); setSelectedConversationId(null); }}
          />
        ) : (
          <ConversationList
            orgId={orgId}
            onSelect={(id) => { setSelectedConversationId(id); setView('conversation-detail'); }}
          />
        )
      ) : showStatusLoading ? (
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

function OrgSelector({ currentOrgId, onOrgChange }: { currentOrgId: string; onOrgChange: (orgId: string) => void }) {
  const [customOrgId, setCustomOrgId] = useState('');

  const handleCustomSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (customOrgId.trim()) {
      onOrgChange(customOrgId.trim());
      setCustomOrgId('');
    }
  };

  return (
    <div className="bg-gray-800 text-white px-4 py-3">
      <div className="max-w-6xl mx-auto flex items-center gap-4 flex-wrap">
        <span className="text-sm font-medium">Admin View</span>
        <div className="flex items-center gap-2">
          <label className="text-xs text-gray-400">Org:</label>
          <select
            value={currentOrgId}
            onChange={(e) => onOrgChange(e.target.value)}
            className="bg-gray-700 text-white text-sm rounded px-2 py-1 border border-gray-600"
          >
            {KNOWN_ORGS.map((org) => (
              <option key={org.id} value={org.id}>
                {org.name}
              </option>
            ))}
            {!KNOWN_ORGS.find((o) => o.id === currentOrgId) && (
              <option value={currentOrgId}>{currentOrgId}</option>
            )}
          </select>
        </div>
        <form onSubmit={handleCustomSubmit} className="flex items-center gap-2">
          <input
            type="text"
            placeholder="Enter org ID..."
            value={customOrgId}
            onChange={(e) => setCustomOrgId(e.target.value)}
            className="bg-gray-700 text-white text-sm rounded px-2 py-1 border border-gray-600 w-64"
          />
          <button
            type="submit"
            className="bg-indigo-600 hover:bg-indigo-700 text-white text-sm px-3 py-1 rounded"
          >
            Go
          </button>
        </form>
      </div>
    </div>
  );
}

function ConversationsPreview() {
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const params = new URLSearchParams(window.location.search);
  const initialOrgId = params.get('orgId') || KNOWN_ORGS[0]?.id || 'preview-org';
  const [orgId, setOrgId] = useState(initialOrgId);

  const handleOrgChange = (newOrgId: string) => {
    setOrgId(newOrgId);
    setSelectedId(null);
    // Update URL without reload
    const newParams = new URLSearchParams(window.location.search);
    newParams.set('orgId', newOrgId);
    window.history.replaceState({}, '', `${window.location.pathname}?${newParams.toString()}`);
  };

  if (selectedId) {
    return (
      <div className="min-h-screen bg-gray-50">
        <OrgSelector currentOrgId={orgId} onOrgChange={handleOrgChange} />
        <ConversationDetail
          orgId={orgId}
          conversationId={selectedId}
          onBack={() => setSelectedId(null)}
        />
      </div>
    );
  }

  return (
    <div className="min-h-screen bg-gray-50">
      <OrgSelector currentOrgId={orgId} onOrgChange={handleOrgChange} />
      <ConversationList
        orgId={orgId}
        onSelect={setSelectedId}
      />
    </div>
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
