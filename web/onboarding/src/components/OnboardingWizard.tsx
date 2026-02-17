import { useState, useEffect, useCallback, useRef } from 'react';
import { StepIndicator } from './StepIndicator';
import { ClinicInfoForm } from './ClinicInfoForm';
import { BusinessHoursForm, type BusinessHoursFormData } from './BusinessHoursForm';
import { ServicesForm } from './ServicesForm';
import { PaymentSetup } from './PaymentSetup';
import { ContactInfoForm, type ContactInfoFormData } from './ContactInfoForm';
import { LOASetup } from './LOASetup';
import { CampaignRegistration } from './CampaignRegistration';
import {
  createClinic,
  getClinicConfig,
  getOnboardingStatus,
  prefillFromWebsite,
  seedKnowledge,
  updateClinicConfig,
  type BusinessHours,
  type DayHours,
  type NotificationSettings,
  type PrefillResult,
} from '../api/client';
import { getStoredOrgId, setStoredOrgId } from '../utils/orgStorage';

const STEPS = [
  { id: 'clinic', name: 'Clinic Info' },
  { id: 'hours', name: 'Hours' },
  { id: 'services', name: 'Services' },
  { id: 'payments', name: 'Payments' },
  { id: 'contact', name: 'Contact Info' },
  { id: 'sms', name: 'SMS Setup' },
  { id: 'campaign', name: '10DLC' },
];

interface OnboardingState {
  orgId: string | null;
  currentStep: number;
  clinicInfo: {
    name: string;
    website?: string;
    email: string;
    phone: string;
    address: string;
    city: string;
    state: string;
    zipCode: string;
    timezone: string;
  } | null;
  businessHours: BusinessHoursFormData | null;
  services: Array<{
    name: string;
    description: string;
    durationMinutes: number;
    priceRange: string;
  }>;
  squareConnected: boolean;
  merchantId?: string;
  contactInfo: ContactInfoFormData | null;
}

interface OnboardingWizardProps {
  orgId?: string | null;
  onComplete?: () => void;
}

const FALLBACK_HOURS = {
  monday: { open: '09:00', close: '18:00' },
  tuesday: { open: '09:00', close: '18:00' },
  wednesday: { open: '09:00', close: '18:00' },
  thursday: { open: '09:00', close: '18:00' },
  friday: { open: '09:00', close: '17:00' },
  saturday: { open: '10:00', close: '16:00' },
  sunday: { open: '10:00', close: '16:00' },
};

function toFormDayHours(value: DayHours | null | undefined, fallback: { open: string; close: string }) {
  if (!value) {
    return { open: fallback.open, close: fallback.close, closed: true };
  }
  return { open: value.open || fallback.open, close: value.close || fallback.close, closed: false };
}

function toFormBusinessHours(hours?: BusinessHours): BusinessHoursFormData {
  return {
    monday: toFormDayHours(hours?.monday, FALLBACK_HOURS.monday),
    tuesday: toFormDayHours(hours?.tuesday, FALLBACK_HOURS.tuesday),
    wednesday: toFormDayHours(hours?.wednesday, FALLBACK_HOURS.wednesday),
    thursday: toFormDayHours(hours?.thursday, FALLBACK_HOURS.thursday),
    friday: toFormDayHours(hours?.friday, FALLBACK_HOURS.friday),
    saturday: toFormDayHours(hours?.saturday, FALLBACK_HOURS.saturday),
    sunday: toFormDayHours(hours?.sunday, FALLBACK_HOURS.sunday),
  };
}

function hasAnyBusinessHours(hours?: BusinessHours): boolean {
  if (!hours) return false;
  return Boolean(
    hours.monday ||
      hours.tuesday ||
      hours.wednesday ||
      hours.thursday ||
      hours.friday ||
      hours.saturday ||
      hours.sunday
  );
}

function toApiBusinessHours(hours: BusinessHoursFormData): BusinessHours {
  const mapDay = (day: BusinessHoursFormData[keyof BusinessHoursFormData]) =>
    day.closed ? null : { open: day.open, close: day.close };
  return {
    monday: mapDay(hours.monday),
    tuesday: mapDay(hours.tuesday),
    wednesday: mapDay(hours.wednesday),
    thursday: mapDay(hours.thursday),
    friday: mapDay(hours.friday),
    saturday: mapDay(hours.saturday),
    sunday: mapDay(hours.sunday),
  };
}

function toContactInfo(notifications?: NotificationSettings): ContactInfoFormData {
  return {
    emailEnabled: notifications?.email_enabled ?? false,
    smsEnabled: notifications?.sms_enabled ?? false,
    emailRecipients: notifications?.email_recipients ?? [],
    smsRecipients: notifications?.sms_recipients ?? [],
    notifyOnPayment: notifications?.notify_on_payment ?? true,
    notifyOnNewLead: notifications?.notify_on_new_lead ?? false,
  };
}

function normalizePhone(phone: string): string {
  const digits = phone.replace(/\D/g, '');
  if (digits.length === 10) {
    return '+1' + digits;
  }
  if (digits.length === 11 && digits.startsWith('1')) {
    return '+' + digits;
  }
  return phone;
}

function mergeContactInfo(
  existing: ContactInfoFormData | null,
  email?: string,
  phone?: string
): ContactInfoFormData {
  const emailRecipients =
    existing?.emailRecipients?.length
      ? existing.emailRecipients
      : email
      ? [email]
      : [];
  const smsRecipients =
    existing?.smsRecipients?.length
      ? existing.smsRecipients
      : phone
      ? [normalizePhone(phone)]
      : [];

  return {
    emailEnabled: existing?.emailEnabled ?? emailRecipients.length > 0,
    smsEnabled: existing?.smsEnabled ?? smsRecipients.length > 0,
    emailRecipients,
    smsRecipients,
    notifyOnPayment: existing?.notifyOnPayment ?? true,
    notifyOnNewLead: existing?.notifyOnNewLead ?? false,
  };
}

function mapServiceNames(services?: string[]): OnboardingState['services'] {
  if (!services || services.length === 0) return [];
  return services.map((name) => ({
    name,
    description: `See website for details about ${name}.`,
    durationMinutes: 30,
    priceRange: 'Varies',
  }));
}

function mapPrefillServices(prefill: PrefillResult): OnboardingState['services'] {
  if (!prefill.services || prefill.services.length === 0) return [];
  return prefill.services.map((service) => ({
    name: service.name,
    description: service.description,
    durationMinutes: service.duration_minutes,
    priceRange: service.price_range,
  }));
}

export function OnboardingWizard({ orgId: orgIdProp, onComplete }: OnboardingWizardProps) {
  const prefillTriggeredRef = useRef<Record<string, boolean>>({});
  const [state, setState] = useState<OnboardingState>({
    orgId: null,
    currentStep: 0,
    clinicInfo: null,
    businessHours: null,
    services: [],
    squareConnected: false,
    contactInfo: null,
  });
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handlePrefill = useCallback(async (website: string, options?: { preferExisting?: boolean }) => {
    const result = await prefillFromWebsite(website);
    const preferExisting = options?.preferExisting ?? false;
    const pickValue = (incoming: string | undefined, existing: string | undefined) => {
      const existingValue = (existing || '').trim();
      if (preferExisting && existingValue) {
        return existingValue;
      }
      return (incoming || existingValue).trim();
    };

    setState((prev) => {
      const clinicInfo = {
        name: pickValue(result.clinic_info.name, prev.clinicInfo?.name),
        website: pickValue(result.clinic_info.website_url || website, prev.clinicInfo?.website),
        email: pickValue(result.clinic_info.email, prev.clinicInfo?.email),
        phone: pickValue(result.clinic_info.phone, prev.clinicInfo?.phone),
        address: pickValue(result.clinic_info.address, prev.clinicInfo?.address),
        city: pickValue(result.clinic_info.city, prev.clinicInfo?.city),
        state: pickValue(result.clinic_info.state, prev.clinicInfo?.state),
        zipCode: pickValue(result.clinic_info.zip_code, prev.clinicInfo?.zipCode),
        timezone: pickValue(result.clinic_info.timezone, prev.clinicInfo?.timezone || 'America/New_York') || 'America/New_York',
      };
      const prefillServices = mapPrefillServices(result);
      const shouldOverrideServices = !preferExisting || prev.services.length === 0;
      const shouldOverrideHours = !preferExisting || !prev.businessHours;

      return {
        ...prev,
        clinicInfo,
        services: shouldOverrideServices && prefillServices.length > 0 ? prefillServices : prev.services,
        businessHours:
          shouldOverrideHours && hasAnyBusinessHours(result.business_hours)
            ? toFormBusinessHours(result.business_hours)
            : prev.businessHours,
        contactInfo: mergeContactInfo(prev.contactInfo, clinicInfo.email, clinicInfo.phone),
      };
    });
  }, []);

  const loadOnboardingStatus = useCallback(async (orgId: string) => {
    try {
      setLoading(true);
      setError(null);
      const status = await getOnboardingStatus(orgId);
      let config = null;
      try {
        config = await getClinicConfig(orgId);
      } catch (err) {
        console.warn('Failed to load clinic config:', err);
      }
      setStoredOrgId(orgId);

      const squareStep = status.steps.find((s) => s.id === 'square_connected');
      const squareConnected = squareStep?.completed || false;

      const clinicInfoCompleted = config?.clinic_info_confirmed || (config?.name && config.name !== 'MedSpa') || false;
      const hoursCompleted = config?.business_hours_confirmed || false;
      const servicesCompleted = config?.services_confirmed || false;
      const contactCompleted = config?.contact_info_confirmed || false;

      let step = 0;
      if (clinicInfoCompleted) step = 1;
      if (clinicInfoCompleted && !hoursCompleted) step = 1;
      if (clinicInfoCompleted && hoursCompleted && !servicesCompleted) step = 2;
      if (clinicInfoCompleted && hoursCompleted && servicesCompleted && !squareConnected) step = 3;
      if (clinicInfoCompleted && hoursCompleted && servicesCompleted && squareConnected) step = 4;
      if (contactCompleted) step = 4;

      setState((prev) => ({
        ...prev,
        orgId,
        currentStep: step,
        squareConnected,
        clinicInfo: config
          ? {
              name: config.name || '',
              website: config.website_url || '',
              email: config.email || '',
              phone: config.phone || '',
              address: config.address || '',
              city: config.city || '',
              state: config.state || '',
              zipCode: config.zip_code || '',
              timezone: config.timezone || 'America/New_York',
            }
          : prev.clinicInfo,
        businessHours: config?.business_hours ? toFormBusinessHours(config.business_hours) : prev.businessHours,
        services: config?.services ? mapServiceNames(config.services) : prev.services,
        contactInfo: config?.notifications ? toContactInfo(config.notifications) : prev.contactInfo,
      }));
    } catch (err) {
      console.error('Failed to load status:', err);
      setError(err instanceof Error ? err.message : 'Failed to load onboarding status');
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

  useEffect(() => {
    const orgId = state.orgId;
    const website = state.clinicInfo?.website?.trim();
    if (!orgId || !website) return;
    const needsServices = state.services.length === 0;
    const needsHours = !state.businessHours;
    if (!needsServices && !needsHours) return;
    if (prefillTriggeredRef.current[orgId]) return;
    prefillTriggeredRef.current[orgId] = true;

    handlePrefill(website, { preferExisting: true }).catch((err) => {
      setError(err instanceof Error ? err.message : 'Failed to prefill from website');
    });
  }, [state.orgId, state.clinicInfo?.website, state.services.length, state.businessHours, handlePrefill]);

  async function handleClinicSubmit(data: NonNullable<OnboardingState['clinicInfo']>) {
    try {
      setLoading(true);
      setError(null);

      if (!state.orgId) {
        // Create new clinic
        const result = await createClinic({
          name: data.name,
          websiteUrl: data.website,
          email: data.email,
          phone: data.phone,
          address: data.address,
          city: data.city,
          state: data.state,
          zipCode: data.zipCode,
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
        await updateClinicConfig(state.orgId, {
          name: data.name,
          website_url: data.website,
          email: data.email,
          phone: data.phone,
          address: data.address,
          city: data.city,
          state: data.state,
          zip_code: data.zipCode,
          timezone: data.timezone,
          clinic_info_confirmed: true,
        });
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

  async function handleHoursSubmit(data: BusinessHoursFormData) {
    try {
      setLoading(true);
      setError(null);
      if (state.orgId) {
        await updateClinicConfig(state.orgId, {
          business_hours: toApiBusinessHours(data),
          business_hours_confirmed: true,
        });
      }
      setState((prev) => ({
        ...prev,
        businessHours: data,
        currentStep: 2,
      }));
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to save business hours');
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
        await updateClinicConfig(state.orgId, {
          services: serviceNames,
          services_confirmed: true,
        });

        // Convert services to knowledge documents for AI RAG
        const knowledgeDocs = data.services.map(s => ({
          title: `${s.name} - Service Info`,
          content: `${s.name}: ${s.description}. Duration: ${s.durationMinutes} minutes. Price: ${s.priceRange}.`,
        }));
        try {
          await seedKnowledge(state.orgId, knowledgeDocs);
        } catch (err) {
          console.warn('Failed to seed knowledge documents', err);
        }
      }

      setState(prev => ({
        ...prev,
        services: data.services,
        currentStep: 3,
      }));
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to save services');
    } finally {
      setLoading(false);
    }
  }

  async function handleContactSubmit(data: ContactInfoFormData) {
    try {
      setLoading(true);
      setError(null);
      if (state.orgId) {
        await updateClinicConfig(state.orgId, {
          notifications: {
            email_enabled: data.emailEnabled,
            email_recipients: data.emailRecipients,
            sms_enabled: data.smsEnabled,
            sms_recipients: data.smsRecipients,
            notify_on_payment: data.notifyOnPayment,
            notify_on_new_lead: data.notifyOnNewLead,
          },
          contact_info_confirmed: true,
        });
      }
      setState((prev) => ({
        ...prev,
        contactInfo: data,
        currentStep: 4,
      }));
      onComplete?.();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to save contact info');
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
      <div className="ui-page flex items-center justify-center">
        <div className="h-9 w-9 animate-spin rounded-full border-2 border-slate-200 border-t-violet-600" />
      </div>
    );
  }

  return (
    <div className="ui-page">
      <div className="ui-container max-w-3xl">
        <div className="text-center mb-8">
          <h1 className="text-3xl font-semibold tracking-tight text-slate-900">Welcome to Medspa Concierge</h1>
          <p className="ui-muted mt-2">
            Let's set up your AI receptionist in a few simple steps.
          </p>
        </div>

        <StepIndicator steps={STEPS} currentStep={state.currentStep} />

        {error && (
          <div className="mb-6 bg-red-50 border border-red-200 rounded-xl p-4">
            <p className="text-sm font-medium text-red-800">{error}</p>
          </div>
        )}

        <div className="ui-card ui-card-solid p-6 sm:p-8">
          {state.currentStep === 0 && (
            <ClinicInfoForm
              defaultValues={state.clinicInfo || undefined}
              onSubmit={handleClinicSubmit}
              onPrefill={handlePrefill}
            />
          )}

          {state.currentStep === 1 && (
            <BusinessHoursForm
              defaultValues={state.businessHours || undefined}
              onSubmit={handleHoursSubmit}
              onBack={goBack}
            />
          )}

          {state.currentStep === 2 && (
            <ServicesForm
              defaultValues={state.services.length > 0 ? { services: state.services } : undefined}
              onSubmit={handleServicesSubmit}
              onBack={goBack}
            />
          )}

          {state.currentStep === 3 && state.orgId && (
            <PaymentSetup
              orgId={state.orgId}
              isConnected={state.squareConnected}
              merchantId={state.merchantId}
              onBack={goBack}
              onContinue={goNext}
            />
          )}

          {state.currentStep === 4 && (
            <ContactInfoForm
              defaultValues={state.contactInfo || undefined}
              onSubmit={handleContactSubmit}
              onBack={goBack}
            />
          )}

          {state.currentStep === 5 && state.orgId && (
            <LOASetup
              orgId={state.orgId}
              clinicName={state.clinicInfo?.name || ''}
              contactName={state.clinicInfo?.name || ''}
              contactEmail={state.contactInfo?.emailRecipients?.[0] || state.clinicInfo?.email || ''}
              contactPhone={state.clinicInfo?.phone || ''}
              onBack={goBack}
              onComplete={goNext}
            />
          )}

          {state.currentStep === 6 && state.orgId && (
            <CampaignRegistration
              orgId={state.orgId}
              onBack={goBack}
              onComplete={() => {
                // Final step complete
                if (onComplete) onComplete();
              }}
            />
          )}
        </div>

        <p className="mt-6 text-center text-sm text-slate-500">
          Need help? Contact support@aiwolfsolutions.com
        </p>
      </div>
    </div>
  );
}
