import { useState, useMemo } from 'react';
import { useAuth } from './AuthContext';
import { registerClinic, lookupOrgByEmail } from '../api/client';
import { setStoredOrgId } from '../utils/orgStorage';

type AuthMode = 'login' | 'register' | 'confirm' | 'setup-clinic';

interface PasswordRequirement {
  label: string;
  test: (password: string) => boolean;
}

const PASSWORD_REQUIREMENTS: PasswordRequirement[] = [
  { label: 'At least 10 characters', test: (p) => p.length >= 10 },
  { label: 'At least one letter', test: (p) => /[a-zA-Z]/.test(p) },
  { label: 'At least one number', test: (p) => /[0-9]/.test(p) },
  { label: 'At least one special character (!@#$%^&*)', test: (p) => /[!@#$%^&*()_+\-=[\]{};':"\\|,.<>/?]/.test(p) },
];

function validatePassword(password: string): { valid: boolean; errors: string[] } {
  const errors = PASSWORD_REQUIREMENTS
    .filter((req) => !req.test(password))
    .map((req) => req.label);
  return { valid: errors.length === 0, errors };
}

export function LoginForm() {
  const { login, loginWithGoogle, register, confirmRegistration } = useAuth();
  const [mode, setMode] = useState<AuthMode>('login');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [confirmCode, setConfirmCode] = useState('');
  const [clinicName, setClinicName] = useState('');
  const [clinicPhone, setClinicPhone] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  const passwordValidation = useMemo(() => validatePassword(password), [password]);
  const passwordsMatch = password === confirmPassword;

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError(null);

    if (mode === 'register') {
      if (!passwordValidation.valid) {
        setError('Password does not meet requirements');
        return;
      }
      if (!passwordsMatch) {
        setError('Passwords do not match');
        return;
      }
    }

    setLoading(true);

    try {
      if (mode === 'login') {
        await login(email, password);
      } else if (mode === 'register') {
        const result = await register(email, password);
        if (result.needsConfirmation) {
          setMode('confirm');
        }
      } else if (mode === 'confirm') {
        await confirmRegistration(email, confirmCode);
        // Check if user already has an org
        try {
          const orgResult = await lookupOrgByEmail(email);
          if (orgResult.org_id) {
            setStoredOrgId(orgResult.org_id);
            await login(email, password);
          } else {
            // No org yet, go to setup-clinic
            setMode('setup-clinic');
            setLoading(false);
            return;
          }
        } catch {
          // No org found, go to setup-clinic
          setMode('setup-clinic');
          setLoading(false);
          return;
        }
      } else if (mode === 'setup-clinic') {
        // Register the clinic
        const result = await registerClinic({
          clinic_name: clinicName,
          owner_email: email,
          owner_phone: clinicPhone || undefined,
        });
        setStoredOrgId(result.org_id);
        await login(email, password);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'An error occurred');
    } finally {
      setLoading(false);
    }
  }

  function switchMode(newMode: AuthMode) {
    setMode(newMode);
    setError(null);
    setPassword('');
    setConfirmPassword('');
  }

  return (
    <div className="ui-page flex items-center justify-center px-4">
      <div className="w-full max-w-md">
        <div className="ui-card ui-card-solid p-8 space-y-6">
          <div className="flex items-start gap-3">
            <div className="ui-brandmark" aria-hidden="true" />
            <div className="min-w-0">
              <h2 className="text-xl sm:text-2xl font-semibold tracking-tight text-slate-900">
            {mode === 'login' && 'Sign in to your account'}
            {mode === 'register' && 'Create your account'}
            {mode === 'confirm' && 'Confirm your email'}
            {mode === 'setup-clinic' && 'Set up your clinic'}
          </h2>
              <p className="ui-muted mt-1">Medspa Concierge Portal</p>
            </div>
          </div>

        {mode === 'login' && (
          <div className="mt-4">
            <button
              type="button"
              onClick={loginWithGoogle}
              className="ui-btn ui-btn-ghost w-full py-3 justify-center"
            >
              <svg className="h-5 w-5" viewBox="0 0 24 24">
                <path
                  fill="#4285F4"
                  d="M22.56 12.25c0-.78-.07-1.53-.2-2.25H12v4.26h5.92c-.26 1.37-1.04 2.53-2.21 3.31v2.77h3.57c2.08-1.92 3.28-4.74 3.28-8.09z"
                />
                <path
                  fill="#34A853"
                  d="M12 23c2.97 0 5.46-.98 7.28-2.66l-3.57-2.77c-.98.66-2.23 1.06-3.71 1.06-2.86 0-5.29-1.93-6.16-4.53H2.18v2.84C3.99 20.53 7.7 23 12 23z"
                />
                <path
                  fill="#FBBC05"
                  d="M5.84 14.09c-.22-.66-.35-1.36-.35-2.09s.13-1.43.35-2.09V7.07H2.18C1.43 8.55 1 10.22 1 12s.43 3.45 1.18 4.93l2.85-2.22.81-.62z"
                />
                <path
                  fill="#EA4335"
                  d="M12 5.38c1.62 0 3.06.56 4.21 1.64l3.15-3.15C17.45 2.09 14.97 1 12 1 7.7 1 3.99 3.47 2.18 7.07l3.66 2.84c.87-2.6 3.3-4.53 6.16-4.53z"
                />
              </svg>
              Sign in with Google
            </button>

            <div className="mt-6 relative">
              <div className="absolute inset-0 flex items-center">
                <div className="w-full border-t border-slate-200" />
              </div>
              <div className="relative flex justify-center text-sm">
                <span className="px-2 bg-white text-slate-500">Or continue with email</span>
              </div>
            </div>
          </div>
        )}

        <form className="mt-6 space-y-6" onSubmit={handleSubmit}>
          {error && (
            <div className="bg-red-50 border border-red-200 rounded-xl p-4">
              <p className="text-sm font-medium text-red-800">{error}</p>
            </div>
          )}

          {(mode === 'login' || mode === 'register') ? (
            <div className="space-y-4">
              <div>
                <label htmlFor="email" className="ui-label">
                  Email address
                </label>
                <input
                  id="email"
                  name="email"
                  type="email"
                  autoComplete="email"
                  required
                  className="ui-input mt-2"
                  placeholder="you@example.com"
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                />
              </div>

              <div>
                <label htmlFor="password" className="ui-label">
                  Password
                </label>
                <input
                  id="password"
                  name="password"
                  type="password"
                  autoComplete={mode === 'login' ? 'current-password' : 'new-password'}
                  required
                  className="ui-input mt-2"
                  placeholder="Enter password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                />
              </div>

              {mode === 'register' && (
                <>
                  <div>
                    <label htmlFor="confirmPassword" className="ui-label">
                      Confirm Password
                    </label>
                    <input
                      id="confirmPassword"
                      name="confirmPassword"
                      type="password"
                      autoComplete="new-password"
                      required
                      className={`ui-input mt-2 ${
                        confirmPassword && !passwordsMatch
                          ? 'border-red-300 bg-red-50'
                          : confirmPassword && passwordsMatch
                          ? 'border-green-300 bg-green-50'
                          : ''
                      }`}
                      placeholder="Confirm password"
                      value={confirmPassword}
                      onChange={(e) => setConfirmPassword(e.target.value)}
                    />
                    {confirmPassword && !passwordsMatch && (
                      <p className="mt-2 text-sm font-medium text-red-700">Passwords do not match</p>
                    )}
                    {confirmPassword && passwordsMatch && (
                      <p className="mt-2 text-sm font-medium text-emerald-700">Passwords match</p>
                    )}
                  </div>

                  <div className="rounded-2xl border border-slate-200/70 bg-slate-50/60 p-4">
                    <p className="text-sm font-semibold text-slate-800 mb-2">Password requirements</p>
                    <ul className="space-y-1">
                      {PASSWORD_REQUIREMENTS.map((req, index) => {
                        const met = req.test(password);
                        return (
                          <li key={index} className="flex items-center text-sm">
                            {met ? (
                              <svg className="h-4 w-4 text-green-500 mr-2" fill="currentColor" viewBox="0 0 20 20">
                                <path fillRule="evenodd" d="M16.707 5.293a1 1 0 010 1.414l-8 8a1 1 0 01-1.414 0l-4-4a1 1 0 011.414-1.414L8 12.586l7.293-7.293a1 1 0 011.414 0z" clipRule="evenodd" />
                              </svg>
                            ) : (
                              <svg className="h-4 w-4 text-gray-300 mr-2" fill="currentColor" viewBox="0 0 20 20">
                                <path fillRule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zm1-11a1 1 0 10-2 0v2H7a1 1 0 100 2h2v2a1 1 0 102 0v-2h2a1 1 0 100-2h-2V7z" clipRule="evenodd" />
                              </svg>
                            )}
                            <span className={met ? 'text-emerald-800' : 'text-slate-500'}>
                              {req.label}
                            </span>
                          </li>
                        );
                      })}
                    </ul>
                  </div>
                </>
              )}
            </div>
          ) : mode === 'confirm' ? (
            <div>
              <label htmlFor="code" className="ui-label">
                Confirmation code
              </label>
              <p className="ui-muted mt-2 mb-3">
                Check your email for a verification code
              </p>
              <input
                id="code"
                name="code"
                type="text"
                required
                className="ui-input"
                placeholder="Enter 6-digit code"
                value={confirmCode}
                onChange={(e) => setConfirmCode(e.target.value)}
              />
            </div>
          ) : (
            <div className="space-y-4">
              <p className="ui-muted">
                Welcome! Let's set up your clinic profile to get started.
              </p>
              <div>
                <label htmlFor="clinicName" className="ui-label">
                  Clinic Name
                </label>
                <input
                  id="clinicName"
                  name="clinicName"
                  type="text"
                  required
                  className="ui-input mt-2"
                  placeholder="Your MedSpa Name"
                  value={clinicName}
                  onChange={(e) => setClinicName(e.target.value)}
                />
              </div>
              <div>
                <label htmlFor="clinicPhone" className="ui-label">
                  Business Phone (optional)
                </label>
                <input
                  id="clinicPhone"
                  name="clinicPhone"
                  type="tel"
                  className="ui-input mt-2"
                  placeholder="+1 (555) 123-4567"
                  value={clinicPhone}
                  onChange={(e) => setClinicPhone(e.target.value)}
                />
              </div>
            </div>
          )}

          <div>
            <button
              type="submit"
              disabled={loading || (mode === 'register' && (!passwordValidation.valid || !passwordsMatch)) || (mode === 'setup-clinic' && !clinicName.trim())}
              className="ui-btn ui-btn-primary w-full py-3"
            >
              {loading ? (
                <span className="flex items-center">
                  <svg className="animate-spin -ml-1 mr-3 h-5 w-5 text-white" fill="none" viewBox="0 0 24 24">
                    <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
                    <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z" />
                  </svg>
                  Processing...
                </span>
              ) : (
                <>
                  {mode === 'login' && 'Sign in'}
                  {mode === 'register' && 'Create account'}
                  {mode === 'confirm' && 'Confirm & Sign in'}
                  {mode === 'setup-clinic' && 'Complete Setup'}
                </>
              )}
            </button>
          </div>

          {(mode === 'login' || mode === 'register') && (
            <div className="text-center">
              <button
                type="button"
                className="ui-link text-sm font-semibold"
                onClick={() => switchMode(mode === 'login' ? 'register' : 'login')}
              >
                {mode === 'login' ? "Don't have an account? Sign up" : 'Already have an account? Sign in'}
              </button>
            </div>
          )}
        </form>
        </div>
      </div>
    </div>
  );
}
