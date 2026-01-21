import { beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import type { ReactNode } from 'react';
import App from './App';
import { getOnboardingStatus, lookupOrgByEmail } from './api/client';
import { useAuth } from './auth';

vi.mock('./api/client', async () => {
  const actual = await vi.importActual<typeof import('./api/client')>('./api/client');
  return {
    ...actual,
    getOnboardingStatus: vi.fn(),
    lookupOrgByEmail: vi.fn(),
  };
});

vi.mock('./auth', () => ({
  AuthProvider: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  useAuth: vi.fn(),
  LoginForm: () => <div>Login Form</div>,
}));

vi.mock('./components/Dashboard', () => ({
  Dashboard: ({ orgId }: { orgId: string }) => <div>Dashboard view {orgId}</div>,
}));

vi.mock('./components/OnboardingWizard', () => ({
  OnboardingWizard: () => <div>Onboarding Wizard</div>,
}));

const baseAuthState = {
  user: { email: 'user@example.com', username: 'user' },
  isLoading: false,
  isAuthenticated: true,
  authEnabled: true,
  login: vi.fn(),
  logout: vi.fn(),
  register: vi.fn(),
  confirmRegistration: vi.fn(),
  getAccessToken: vi.fn(),
};

describe('App onboarding flow', () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    vi.mocked(useAuth).mockReturnValue(baseAuthState);
  });

  it('renders the dashboard when onboarding is ready', async () => {
    localStorage.setItem('medspa_org_id', 'org_123');
    vi.mocked(getOnboardingStatus).mockResolvedValue({
      org_id: 'org_123',
      clinic_name: 'MedSpa',
      overall_progress: 100,
      ready_for_launch: true,
      steps: [],
    });

    render(<App />);

    expect(await screen.findByText('Dashboard view org_123')).toBeInTheDocument();
    expect(screen.queryByText('Onboarding Wizard')).not.toBeInTheDocument();
  });

  it('renders onboarding when not ready for launch', async () => {
    localStorage.setItem('medspa_org_id', 'org_123');
    vi.mocked(getOnboardingStatus).mockResolvedValue({
      org_id: 'org_123',
      clinic_name: 'MedSpa',
      overall_progress: 60,
      ready_for_launch: false,
      steps: [],
    });

    render(<App />);

    expect(await screen.findByText('Onboarding Wizard')).toBeInTheDocument();
    expect(screen.queryByText('Dashboard view org_123')).not.toBeInTheDocument();
  });

  it('renders the dashboard when setup is complete even if not ready', async () => {
    localStorage.setItem('medspa_org_id', 'org_123');
    localStorage.setItem('medspa_setup_complete:org_123', 'true');
    vi.mocked(getOnboardingStatus).mockResolvedValue({
      org_id: 'org_123',
      clinic_name: 'MedSpa',
      overall_progress: 60,
      ready_for_launch: false,
      steps: [],
    });

    render(<App />);

    expect(await screen.findByText('Dashboard view org_123')).toBeInTheDocument();
    expect(screen.queryByText('Onboarding Wizard')).not.toBeInTheDocument();
  });

  it('renders clinic setup when orgId is missing', async () => {
    vi.mocked(lookupOrgByEmail).mockRejectedValue(new Error('Not found'));

    render(<App />);

    expect(await screen.findByText('Set up your clinic')).toBeInTheDocument();
    expect(vi.mocked(getOnboardingStatus)).not.toHaveBeenCalled();
  });

  it('renders onboarding when onboarding status fails', async () => {
    localStorage.setItem('medspa_org_id', 'org_123');
    vi.mocked(getOnboardingStatus).mockRejectedValue(new Error('Failure'));
    const errorSpy = vi.spyOn(console, 'error').mockImplementation(() => {});

    render(<App />);

    expect(await screen.findByText('Onboarding Wizard')).toBeInTheDocument();
    errorSpy.mockRestore();
  });
});
