import { OnboardingWizard } from './components/OnboardingWizard';
import { AuthProvider, useAuth, LoginForm } from './auth';

function AuthenticatedApp() {
  const { isLoading, isAuthenticated, authEnabled, user, logout } = useAuth();

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

  // Show onboarding wizard (with optional user header if authenticated)
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
      <OnboardingWizard />
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
