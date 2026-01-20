import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { OnboardingWizard } from './OnboardingWizard';
import { getClinicConfig, getOnboardingStatus, seedKnowledge, updateClinicConfig } from '../api/client';

vi.mock('../api/client', async () => {
  const actual = await vi.importActual<typeof import('../api/client')>('../api/client');
  return {
    ...actual,
    getClinicConfig: vi.fn(),
    getOnboardingStatus: vi.fn(),
    updateClinicConfig: vi.fn(),
    seedKnowledge: vi.fn(),
  };
});

vi.mock('./PaymentSetup', () => ({
  PaymentSetup: () => <div>Payment Setup</div>,
}));

describe('OnboardingWizard services step', () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
  });

  it('advances to payments even if knowledge seeding fails', async () => {
    vi.mocked(getOnboardingStatus).mockResolvedValue({
      org_id: 'org_123',
      clinic_name: 'Test Clinic',
      overall_progress: 25,
      ready_for_launch: false,
      steps: [
        {
          id: 'clinic_config',
          name: 'Clinic Configuration',
          description: '',
          completed: true,
          required: true,
        },
        {
          id: 'square_connected',
          name: 'Square Connected',
          description: '',
          completed: false,
          required: true,
        },
        {
          id: 'phone_configured',
          name: 'Phone Configured',
          description: '',
          completed: false,
          required: true,
        },
      ],
    });
    vi.mocked(getClinicConfig).mockResolvedValue({
      org_id: 'org_123',
      name: 'Test Clinic',
      timezone: 'America/New_York',
      clinic_info_confirmed: true,
      business_hours_confirmed: true,
      services_confirmed: false,
      contact_info_confirmed: false,
    });
    vi.mocked(updateClinicConfig).mockResolvedValue();
    vi.mocked(seedKnowledge).mockRejectedValue(new Error('Unknown error'));

    render(<OnboardingWizard orgId="org_123" />);

    expect(await screen.findByText('Services & Pricing')).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: /\+ Botox/i }));

    await waitFor(() =>
      expect(screen.getByRole('button', { name: /Continue/i })).not.toBeDisabled()
    );

    fireEvent.click(screen.getByRole('button', { name: /Continue/i }));

    expect(await screen.findByText('Payment Setup')).toBeInTheDocument();
    expect(screen.queryByText('Unknown error')).not.toBeInTheDocument();
    expect(updateClinicConfig).toHaveBeenCalledWith('org_123', {
      services: ['Botox'],
      services_confirmed: true,
    });
    expect(seedKnowledge).toHaveBeenCalled();
  });
});
