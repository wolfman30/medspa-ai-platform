import { useEffect, useState } from 'react';
import { OnboardingWizard } from './components/OnboardingWizard';
import { Dashboard } from './components/Dashboard';
import { CampaignRegistration } from './components/CampaignRegistration';
import { ConversationList } from './components/ConversationList';
import { ConversationDetail } from './components/ConversationDetail';
import { DepositList } from './components/DepositList';
import { DepositDetail } from './components/DepositDetail';
import { SettingsPage } from './components/SettingsPage';
import { KnowledgeSettings } from './components/KnowledgeSettings';
import { getOnboardingStatus, lookupOrgByEmail, registerClinic, listOrgs, type ApiScope, type OrgListItem } from './api/client';
import { AuthProvider, useAuth, LoginForm } from './auth';
import {
  getStoredOrgId,
  getStoredSetupComplete,
  setStoredOrgId,
  setStoredSetupComplete,
} from './utils/orgStorage';

type OnboardingDecision = 'idle' | 'loading' | 'ready' | 'not_ready';
type AppView =
  | 'dashboard'
  | 'conversations'
  | 'conversation-detail'
  | 'deposits'
  | 'deposit-detail'
  | 'settings'
  | 'knowledge';

// Admin users can view all orgs
const ADMIN_EMAILS = ['andrew@aiwolfsolutions.com', 'wolfpassion20@gmail.com', 'aiwolftwin@gmail.com'];

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
  { id: 'b1b73611-420c-4f30-b4cc-e576a79fabf8', name: 'Glow Medspa' },
  { id: 'bb507f20-7fcc-4941-9eac-9ed93b7834ed', name: 'AI Wolf Solutions' },
];

// Clinic setup form for returning users without an org
interface ClinicSetupPromptProps {
  email: string;
  onComplete: (orgId: string) => void;
  onLogout: () => void;
}

function ClinicSetupPrompt({ email, onComplete, onLogout }: ClinicSetupPromptProps) {
  const [clinicName, setClinicName] = useState('');
  const [clinicPhone, setClinicPhone] = useState('');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!clinicName.trim()) {
      setError('Clinic name is required');
      return;
    }

    setLoading(true);
    setError(null);

    try {
      const result = await registerClinic({
        clinic_name: clinicName.trim(),
        owner_email: email,
        owner_phone: clinicPhone.trim() || undefined,
      });
      onComplete(result.org_id);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create clinic');
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="min-h-screen flex items-center justify-center bg-gray-50 py-12 px-4 sm:px-6 lg:px-8">
      <div className="max-w-md w-full space-y-8">
        <div>
          <h2 className="mt-6 text-center text-3xl font-extrabold text-gray-900">
            Set up your clinic
          </h2>
          <p className="mt-2 text-center text-sm text-gray-600">
            Welcome! Let's set up your clinic profile to get started.
          </p>
          <p className="mt-1 text-center text-xs text-gray-500">
            Signed in as {email}
          </p>
        </div>

        <form className="mt-8 space-y-6" onSubmit={handleSubmit}>
          {error && (
            <div className="bg-red-50 border border-red-200 rounded-lg p-4">
              <p className="text-sm text-red-700">{error}</p>
            </div>
          )}

          <div className="space-y-4">
            <div>
              <label htmlFor="clinicName" className="block text-sm font-medium text-gray-700">
                Clinic Name
              </label>
              <input
                id="clinicName"
                name="clinicName"
                type="text"
                required
                className="mt-1 appearance-none relative block w-full px-3 py-2 border border-gray-300 placeholder-gray-500 text-gray-900 rounded-md focus:outline-none focus:ring-indigo-500 focus:border-indigo-500 sm:text-sm"
                placeholder="Your MedSpa Name"
                value={clinicName}
                onChange={(e) => setClinicName(e.target.value)}
              />
            </div>
            <div>
              <label htmlFor="clinicPhone" className="block text-sm font-medium text-gray-700">
                Business Phone (optional)
              </label>
              <input
                id="clinicPhone"
                name="clinicPhone"
                type="tel"
                className="mt-1 appearance-none relative block w-full px-3 py-2 border border-gray-300 placeholder-gray-500 text-gray-900 rounded-md focus:outline-none focus:ring-indigo-500 focus:border-indigo-500 sm:text-sm"
                placeholder="+1 (555) 123-4567"
                value={clinicPhone}
                onChange={(e) => setClinicPhone(e.target.value)}
              />
            </div>
          </div>

          <div>
            <button
              type="submit"
              disabled={loading || !clinicName.trim()}
              className="group relative w-full flex justify-center py-2 px-4 border border-transparent text-sm font-medium rounded-md text-white bg-indigo-600 hover:bg-indigo-700 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-indigo-500 disabled:opacity-50 disabled:cursor-not-allowed"
            >
              {loading ? 'Creating...' : 'Complete Setup'}
            </button>
          </div>

          <div className="text-center">
            <button
              type="button"
              className="text-sm text-gray-500 hover:text-gray-700"
              onClick={onLogout}
            >
              Sign out and use a different account
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}

function AuthenticatedApp() {
  const { isLoading, isAuthenticated, authEnabled, user, logout } = useAuth();
  const [decision, setDecision] = useState<OnboardingDecision>('idle');
  const [checkedOrgId, setCheckedOrgId] = useState<string | null>(null);
  const [statusRefresh, setStatusRefresh] = useState(0);
  const [view, setView] = useState<AppView>('dashboard');
  const [selectedConversationId, setSelectedConversationId] = useState<string | null>(null);
  const [selectedDepositId, setSelectedDepositId] = useState<string | null>(null);
  const [adminOrgId, setAdminOrgId] = useState<string>(KNOWN_ORGS[0]?.id || '');
  const [orgLookupDone, setOrgLookupDone] = useState(false);
  const [needsClinicSetup, setNeedsClinicSetup] = useState(false);
  const [setupComplete, setSetupComplete] = useState(false);
  const [adminOrgs, setAdminOrgs] = useState<OrgListItem[]>([]);
  const [adminOrgsLoading, setAdminOrgsLoading] = useState(false);

  const authReady = !isLoading && (!authEnabled || isAuthenticated);
  const userOrgId = getOrgIdFromUser(user);
  const isAdmin = isAdminUser(user?.email);
  const dataScope: ApiScope = isAdmin ? 'admin' : 'portal';
  // Admins can switch orgs; regular users use their assigned org
  const orgId = isAdmin ? adminOrgId : (userOrgId || getStoredOrgId());

  // Fetch organizations list for admin users
  useEffect(() => {
    if (!authReady || !isAdmin || adminOrgs.length > 0) return;
    setAdminOrgsLoading(true);
    listOrgs()
      .then((res) => {
        setAdminOrgs(res.organizations);
        // If we have orgs and no current selection, select the first one
        if (res.organizations.length > 0 && !adminOrgId) {
          setAdminOrgId(res.organizations[0].id);
        }
      })
      .catch((err) => {
        console.error('Failed to fetch organizations:', err);
        // Fall back to KNOWN_ORGS
      })
      .finally(() => setAdminOrgsLoading(false));
  }, [authReady, isAdmin, adminOrgs.length, adminOrgId]);

  // Look up org for returning users who don't have an org in localStorage
  useEffect(() => {
    if (!authReady || !user?.email || isAdmin || orgLookupDone) return;

    // If we already have an org, skip lookup
    const storedOrg = getStoredOrgId();
    if (storedOrg) {
      setOrgLookupDone(true);
      return;
    }

    // Look up org by email
    lookupOrgByEmail(user.email)
      .then((result) => {
        if (result.org_id) {
          setStoredOrgId(result.org_id);
        } else {
          setNeedsClinicSetup(true);
        }
        setOrgLookupDone(true);
      })
      .catch(() => {
        // No org found - user needs to set up clinic
        setNeedsClinicSetup(true);
        setOrgLookupDone(true);
      });
  }, [authReady, user?.email, isAdmin, orgLookupDone]);

  useEffect(() => {
    if (!authReady || !userOrgId) return;
    setStoredOrgId(userOrgId);
  }, [authReady, userOrgId]);

  useEffect(() => {
    if (!authReady || !orgId) return;
    const storedSetupComplete = getStoredSetupComplete(orgId);
    setSetupComplete(storedSetupComplete);
    let isActive = true;
    getOnboardingStatus(orgId)
      .then(status => {
        if (!isActive) return;
        const serverSetupComplete = status.setup_complete || false;
        if (serverSetupComplete && !storedSetupComplete) {
          setStoredSetupComplete(orgId);
        }
        setSetupComplete(storedSetupComplete || serverSetupComplete);
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

  // If user is authenticated but needs to set up their clinic
  if (needsClinicSetup && user?.email) {
    return (
      <ClinicSetupPrompt
        email={user.email}
        onComplete={(newOrgId) => {
          setStoredOrgId(newOrgId);
          setNeedsClinicSetup(false);
          setStatusRefresh((prev) => prev + 1);
        }}
        onLogout={logout}
      />
    );
  }

  // Wait for org lookup to complete for non-admin users
  if (authEnabled && isAuthenticated && !isAdmin && !orgLookupDone) {
    return (
      <div className="min-h-screen flex items-center justify-center">
        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-indigo-600" />
      </div>
    );
  }

  const showStatusLoading = authReady && orgId && checkedOrgId !== orgId;
  const canAccessPortal = decision === 'ready' || setupComplete;

  const handleAdminOrgChange = (newOrgId: string) => {
    setAdminOrgId(newOrgId);
    setSelectedConversationId(null);
    setSelectedDepositId(null);
    setDecision('idle');
    setCheckedOrgId(null);
  };

  // Show onboarding wizard or dashboard (with optional user header if authenticated)
  return (
    <div>
      {/* Admin header with org selector */}
      {authEnabled && user && isAdmin && (
        <OrgSelector
          currentOrgId={adminOrgId}
          onOrgChange={handleAdminOrgChange}
          orgs={adminOrgs}
          loading={adminOrgsLoading}
        />
      )}
      {authEnabled && user && (
        <div className="bg-indigo-600 text-white px-3 py-2 sm:px-4">
          <div className="flex flex-col gap-2 sm:flex-row sm:justify-between sm:items-center">
            <div className="flex justify-between items-center">
              <span className="text-xs sm:text-sm truncate max-w-[200px] sm:max-w-none">
                {isAdmin ? '(Admin) ' : ''}{user.email}
              </span>
              <button
                onClick={logout}
                className="text-xs sm:text-sm underline hover:no-underline sm:hidden ml-2 whitespace-nowrap"
              >
                Sign out
              </button>
            </div>
            {(isAdmin || (canAccessPortal && orgId)) && (
              <nav className="flex flex-wrap gap-1 sm:gap-2">
                {!isAdmin && (
                  <button
                    onClick={() => setView('dashboard')}
                    className={`text-xs sm:text-sm px-2 py-1 rounded ${view === 'dashboard' ? 'bg-indigo-500' : 'hover:bg-indigo-500'}`}
                  >
                    Dashboard
                  </button>
                )}
                <button
                  onClick={() => { setView('conversations'); setSelectedConversationId(null); }}
                  className={`text-xs sm:text-sm px-2 py-1 rounded ${view === 'conversations' || view === 'conversation-detail' ? 'bg-indigo-500' : 'hover:bg-indigo-500'}`}
                >
                  Conversations
                </button>
                <button
                  onClick={() => { setView('deposits'); setSelectedDepositId(null); }}
                  className={`text-xs sm:text-sm px-2 py-1 rounded ${view === 'deposits' || view === 'deposit-detail' ? 'bg-indigo-500' : 'hover:bg-indigo-500'}`}
                >
                  Deposits
                </button>
                <button
                  onClick={() => setView('settings')}
                  className={`text-xs sm:text-sm px-2 py-1 rounded ${view === 'settings' ? 'bg-indigo-500' : 'hover:bg-indigo-500'}`}
                >
                  Settings
                </button>
                <button
                  onClick={() => setView('knowledge')}
                  className={`text-xs sm:text-sm px-2 py-1 rounded ${view === 'knowledge' ? 'bg-indigo-500' : 'hover:bg-indigo-500'}`}
                >
                  Knowledge
                </button>
              </nav>
            )}
            <button
              onClick={logout}
              className="hidden sm:block text-sm underline hover:no-underline whitespace-nowrap"
            >
              Sign out
            </button>
          </div>
        </div>
      )}
      {/* Admin view - direct access to conversations and deposits */}
      {isAdmin && orgId ? (
        view === 'conversation-detail' && selectedConversationId ? (
          <ConversationDetail
            orgId={orgId}
            conversationId={selectedConversationId}
            onBack={() => { setView('conversations'); setSelectedConversationId(null); }}
            scope={dataScope}
          />
        ) : view === 'deposit-detail' && selectedDepositId ? (
          <DepositDetail
            orgId={orgId}
            depositId={selectedDepositId}
            onBack={() => { setView('deposits'); setSelectedDepositId(null); }}
            onViewConversation={(convId) => { setSelectedConversationId(convId); setView('conversation-detail'); }}
            scope={dataScope}
          />
        ) : view === 'settings' ? (
          <SettingsPage
            orgId={orgId}
            onBack={() => setView('conversations')}
          />
        ) : view === 'knowledge' ? (
          <KnowledgeSettings
            orgId={orgId}
            onBack={() => setView('conversations')}
          />
        ) : view === 'deposits' ? (
          <DepositList
            orgId={orgId}
            onSelect={(id) => { setSelectedDepositId(id); setView('deposit-detail'); }}
            scope={dataScope}
          />
        ) : (
          <ConversationList
            orgId={orgId}
            onSelect={(id) => { setSelectedConversationId(id); setView('conversation-detail'); }}
            scope={dataScope}
          />
        )
      ) : showStatusLoading ? (
        <div className="min-h-screen flex items-center justify-center">
          <span className="text-sm text-gray-600">Checking onboarding status...</span>
        </div>
      ) : canAccessPortal && orgId ? (
        view === 'conversation-detail' && selectedConversationId ? (
          <ConversationDetail
            orgId={orgId}
            conversationId={selectedConversationId}
            onBack={() => { setView('conversations'); setSelectedConversationId(null); }}
            scope={dataScope}
          />
        ) : view === 'deposit-detail' && selectedDepositId ? (
          <DepositDetail
            orgId={orgId}
            depositId={selectedDepositId}
            onBack={() => { setView('deposits'); setSelectedDepositId(null); }}
            onViewConversation={(convId) => { setSelectedConversationId(convId); setView('conversation-detail'); }}
            scope={dataScope}
          />
        ) : view === 'settings' ? (
          <SettingsPage
            orgId={orgId}
            onBack={() => setView('dashboard')}
          />
        ) : view === 'knowledge' ? (
          <KnowledgeSettings
            orgId={orgId}
            onBack={() => setView('dashboard')}
          />
        ) : view === 'deposits' ? (
          <DepositList
            orgId={orgId}
            onSelect={(id) => { setSelectedDepositId(id); setView('deposit-detail'); }}
            scope={dataScope}
          />
        ) : view === 'conversations' ? (
          <ConversationList
            orgId={orgId}
            onSelect={(id) => { setSelectedConversationId(id); setView('conversation-detail'); }}
            scope={dataScope}
          />
        ) : (
          <Dashboard orgId={orgId} />
        )
      ) : (
        <OnboardingWizard
          orgId={orgId}
          onComplete={() => {
            const resolvedOrgId = orgId || getStoredOrgId();
            if (resolvedOrgId) {
              setStoredSetupComplete(resolvedOrgId);
              setSetupComplete(true);
            }
            setView('dashboard');
            setStatusRefresh(prev => prev + 1);
          }}
        />
      )}
    </div>
  );
}

interface OrgSelectorProps {
  currentOrgId: string;
  onOrgChange: (orgId: string) => void;
  orgs?: Array<{ id: string; name: string }>;
  loading?: boolean;
}

function OrgSelector({ currentOrgId, onOrgChange, orgs, loading }: OrgSelectorProps) {
  const [customOrgId, setCustomOrgId] = useState('');
  // Use provided orgs or fall back to KNOWN_ORGS
  const orgList = orgs && orgs.length > 0 ? orgs : KNOWN_ORGS;

  const handleCustomSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (customOrgId.trim()) {
      onOrgChange(customOrgId.trim());
      setCustomOrgId('');
    }
  };

  return (
    <div className="bg-gray-800 text-white px-3 py-2 sm:px-4 sm:py-3">
      <div className="max-w-6xl mx-auto flex flex-col gap-2 sm:flex-row sm:items-center sm:gap-4 sm:flex-wrap">
        <span className="text-sm font-medium">Admin View</span>
        <div className="flex items-center gap-2">
          <label className="text-xs text-gray-400">Org:</label>
          <select
            value={currentOrgId}
            onChange={(e) => onOrgChange(e.target.value)}
            className="bg-gray-700 text-white text-sm rounded px-2 py-1 border border-gray-600 flex-1 sm:flex-none"
            disabled={loading}
          >
            {loading && <option value="">Loading...</option>}
            {orgList.map((org) => (
              <option key={org.id} value={org.id}>
                {org.name}
              </option>
            ))}
            {!orgList.find((o) => o.id === currentOrgId) && currentOrgId && (
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
            className="bg-gray-700 text-white text-sm rounded px-2 py-1 border border-gray-600 flex-1 sm:w-64"
          />
          <button
            type="submit"
            className="bg-indigo-600 hover:bg-indigo-700 text-white text-sm px-3 py-1 rounded whitespace-nowrap"
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
