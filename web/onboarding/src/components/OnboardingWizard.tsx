import { useState, useEffect, useCallback } from 'react';
import { StepIndicator } from './StepIndicator';
import { ClinicInfoForm } from './ClinicInfoForm';
import { ServicesForm } from './ServicesForm';
import { PaymentSetup } from './PaymentSetup';
import { SMSSetup } from './SMSSetup';
import { createClinic, getOnboardingStatus, updateClinicConfig, seedKnowledge } from '../api/client';
import { getStoredOrgId, setStoredOrgId } from '../utils/orgStorage';

const STEPS = [
  { id: 'clinic', name: 'Clinic Info' },
  { id: 'services', name: 'Services' },
  { id: 'payments', name: 'Payments' },
  { id: 'sms', name: 'SMS' },
];

interface OnboardingState {
  orgId: string | null;
  currentStep: number;
  clinicInfo: {
    name: string;
    email: string;
    phone: string;
    address: string;
    city: string;
    state: string;
    zipCode: string;
    timezone: string;
  } | null;
  services: Array<{
    name: string;
    description: string;
    durationMinutes: number;
    priceRange: string;
  }>;
  squareConnected: boolean;
  merchantId?: string;
  smsStatus: 'not_started' | 'pending' | 'verified' | 'active';
  phoneNumber?: string;
}

interface OnboardingWizardProps {
  orgId?: string | null;
  onComplete?: () => void;
}

export function OnboardingWizard({ orgId: orgIdProp, onComplete }: OnboardingWizardProps) {
  const [state, setState] = useState<OnboardingState>({
    orgId: null,
    currentStep: 0,
    clinicInfo: null,
    services: [],
    squareConnected: false,
    smsStatus: 'not_started',
  });
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const loadOnboardingStatus = useCallback(async (orgId: string) => {
    try {
      setLoading(true);
      const status = await getOnboardingStatus(orgId);
      setStoredOrgId(orgId);

      // Determine current step based on what's completed
      let step = 0;
      const squareStep = status.steps.find(s => s.id === 'square_connected');
      const phoneStep = status.steps.find(s => s.id === 'phone_configured');

      if (status.steps.find(s => s.id === 'clinic_config')?.completed) step = 1;
      if (squareStep?.completed) step = 2;
      if (phoneStep?.completed) step = 3;

      setState(prev => ({
        ...prev,
        orgId,
        currentStep: step,
        squareConnected: squareStep?.completed || false,
        smsStatus: phoneStep?.completed ? 'active' : 'not_started',
      }));
    } catch (err) {
      console.error('Failed to load status:', err);
    } finally {
      setLoading(false);
    }
  }, []);

  // Check URL for orgId (returning from Square OAuth)
  useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    const orgIdFromUrl = params.get('org_id');
    const resolvedOrgId = orgIdFromUrl || orgIdProp || getStoredOrgId();
    if (resolvedOrgId) {
      loadOnboardingStatus(resolvedOrgId);
    }
  }, [orgIdProp, loadOnboardingStatus]);

  async function handleClinicSubmit(data: NonNullable<OnboardingState['clinicInfo']>) {
    try {
      setLoading(true);
      setError(null);

      if (!state.orgId) {
        // Create new clinic
        const result = await createClinic({
          name: data.name,
          email: data.email,
          phone: data.phone,
          timezone: data.timezone,
        });

        setStoredOrgId(result.org_id);
        setState(prev => ({
          ...prev,
          orgId: result.org_id,
          clinicInfo: data,
          currentStep: 1,
        }));
      } else {
        // Update existing
        await updateClinicConfig(state.orgId, data);
        setStoredOrgId(state.orgId);
        setState(prev => ({
          ...prev,
          clinicInfo: data,
          currentStep: 1,
        }));
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to save clinic info');
    } finally {
      setLoading(false);
    }
  }

  async function handleServicesSubmit(data: { services: OnboardingState['services'] }) {
    try {
      setLoading(true);
      setError(null);

      if (state.orgId && data.services && data.services.length > 0) {
        // Save service names to clinic config
        const serviceNames = data.services.map(s => s.name);
        await updateClinicConfig(state.orgId, { services: serviceNames });

        // Convert services to knowledge documents for AI RAG
        const knowledgeDocs = data.services.map(s => ({
          title: `${s.name} - Service Info`,
          content: `${s.name}: ${s.description}. Duration: ${s.durationMinutes} minutes. Price: ${s.priceRange}.`,
        }));
        await seedKnowledge(state.orgId, knowledgeDocs);
      }

      setState(prev => ({
        ...prev,
        services: data.services,
        currentStep: 2,
      }));
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to save services');
    } finally {
      setLoading(false);
    }
  }

  function goBack() {
    setState(prev => ({ ...prev, currentStep: Math.max(0, prev.currentStep - 1) }));
  }

  function goNext() {
    setState(prev => ({ ...prev, currentStep: prev.currentStep + 1 }));
  }

  if (loading && !state.orgId) {
    return (
      <div className="min-h-screen flex items-center justify-center">
        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-indigo-600" />
      </div>
    );
  }

  return (
    <div className="min-h-screen bg-gray-50 py-12">
      <div className="max-w-3xl mx-auto px-4 sm:px-6 lg:px-8">
        <div className="text-center mb-8">
          <h1 className="text-3xl font-bold text-gray-900">Welcome to MedSpa AI</h1>
          <p className="mt-2 text-gray-600">
            Let's set up your AI receptionist in a few simple steps.
          </p>
        </div>

        <StepIndicator steps={STEPS} currentStep={state.currentStep} />

        {error && (
          <div className="mb-6 bg-red-50 border border-red-200 rounded-lg p-4">
            <p className="text-sm text-red-700">{error}</p>
          </div>
        )}

        <div className="bg-white shadow rounded-lg p-6 sm:p-8">
          {state.currentStep === 0 && (
            <ClinicInfoForm
              defaultValues={state.clinicInfo || undefined}
              onSubmit={handleClinicSubmit}
            />
          )}

          {state.currentStep === 1 && (
            <ServicesForm
              defaultValues={state.services.length > 0 ? { services: state.services } : undefined}
              onSubmit={handleServicesSubmit}
              onBack={goBack}
            />
          )}

          {state.currentStep === 2 && state.orgId && (
            <PaymentSetup
              orgId={state.orgId}
              isConnected={state.squareConnected}
              merchantId={state.merchantId}
              onBack={goBack}
              onContinue={goNext}
            />
          )}

          {state.currentStep === 3 && state.orgId && (
            <SMSSetup
              orgId={state.orgId}
              phoneNumber={state.phoneNumber}
              status={state.smsStatus}
              onBack={goBack}
              onComplete={() => {
                // Show completion screen or redirect
                alert('Onboarding complete! Your AI receptionist will be ready once SMS is activated.');
                onComplete?.();
              }}
              onPhoneActivated={(phone) => {
                setState(prev => ({
                  ...prev,
                  phoneNumber: phone,
                  smsStatus: 'active',
                }));
              }}
            />
          )}
        </div>

        <p className="mt-6 text-center text-sm text-gray-500">
          Need help? Contact support@aiwolfsolutions.com
        </p>
      </div>
    </div>
  );
}
