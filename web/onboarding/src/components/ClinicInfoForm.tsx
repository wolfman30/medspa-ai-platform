import { useEffect, useState } from 'react';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { z } from 'zod';

const clinicSchema = z.object({
  name: z.string().min(2, 'Clinic name is required'),
  legalName: z.string().optional(),
  ein: z.string().optional(),
  website: z.string().optional(),
  email: z.string().email('Valid email required'),
  phone: z.string().min(10, 'Phone number required'),
  address: z.string().min(5, 'Address required'),
  city: z.string().min(2, 'City required'),
  state: z.string().min(2, 'State required'),
  zipCode: z.string().min(5, 'Zip code required'),
  timezone: z.string().min(1, 'Timezone required'),
});

type ClinicFormData = z.infer<typeof clinicSchema>;

interface Props {
  defaultValues?: Partial<ClinicFormData>;
  onSubmit: (data: ClinicFormData) => void;
  onBack?: () => void;
  onPrefill?: (website: string) => Promise<void>;
}

const US_TIMEZONES = [
  { value: 'America/New_York', label: 'Eastern Time' },
  { value: 'America/Chicago', label: 'Central Time' },
  { value: 'America/Denver', label: 'Mountain Time' },
  { value: 'America/Los_Angeles', label: 'Pacific Time' },
  { value: 'America/Phoenix', label: 'Arizona Time' },
  { value: 'Pacific/Honolulu', label: 'Hawaii Time' },
  { value: 'America/Anchorage', label: 'Alaska Time' },
];

export function ClinicInfoForm({ defaultValues, onSubmit, onBack, onPrefill }: Props) {
  const {
    register,
    handleSubmit,
    reset,
    watch,
    formState: { errors, isSubmitting },
  } = useForm<ClinicFormData>({
    resolver: zodResolver(clinicSchema),
    defaultValues: {
      timezone: 'America/New_York',
      ...defaultValues,
    },
  });
  const [prefillError, setPrefillError] = useState<string | null>(null);
  const [prefillLoading, setPrefillLoading] = useState(false);
  const websiteValue = watch('website');

  useEffect(() => {
    reset({
      timezone: 'America/New_York',
      ...defaultValues,
    });
  }, [defaultValues, reset]);

  const handlePrefill = async () => {
    if (!onPrefill) return;
    const website = (websiteValue || '').trim();
    if (!website) {
      setPrefillError('Enter a website to prefill');
      return;
    }
    setPrefillLoading(true);
    setPrefillError(null);
    try {
      await onPrefill(website);
    } catch (err) {
      setPrefillError(err instanceof Error ? err.message : 'Failed to prefill from website');
    } finally {
      setPrefillLoading(false);
    }
  };

  return (
    <form onSubmit={handleSubmit(onSubmit)} className="space-y-6">
      <div>
        <h2 className="text-xl font-semibold tracking-tight text-slate-900">Clinic Information</h2>
        <p className="ui-muted mt-1">
          Tell us about your medspa. This information will be used for client communications.
        </p>
      </div>

      <div className="grid grid-cols-1 gap-6 sm:grid-cols-2">
        <div className="sm:col-span-2">
          <label htmlFor="name" className="ui-label">
            Clinic Name *
          </label>
          <input
            type="text"
            id="name"
            {...register('name')}
            className="ui-input mt-2"
            placeholder="Glow MedSpa"
          />
          {errors.name && (
            <p className="mt-2 text-sm font-medium text-red-700">{errors.name.message}</p>
          )}
        </div>

        <div className="sm:col-span-2">
          <label htmlFor="legalName" className="ui-label">
            Legal Business Name
            <span className="ml-1 text-xs font-normal text-gray-500">(as it appears on IRS filings — needed for SMS registration)</span>
          </label>
          <input
            type="text"
            id="legalName"
            {...register('legalName')}
            className="ui-input mt-2"
            placeholder="Same as clinic name if identical"
          />
          {errors.legalName && (
            <p className="mt-2 text-sm font-medium text-red-700">{errors.legalName.message}</p>
          )}
        </div>

        <div>
          <label htmlFor="ein" className="ui-label">
            EIN (Employer Identification Number)
            <span className="ml-1 text-xs font-normal text-gray-500">(needed for SMS registration)</span>
          </label>
          <input
            type="text"
            id="ein"
            {...register('ein')}
            className="ui-input mt-2"
            placeholder="XX-XXXXXXX"
          />
          {errors.ein && (
            <p className="mt-2 text-sm font-medium text-red-700">{errors.ein.message}</p>
          )}
        </div>

        <div className="sm:col-span-2">
          <div className="flex items-center justify-between">
            <label htmlFor="website" className="ui-label">
              Website
            </label>
            {onPrefill && (
              <button
                type="button"
                onClick={handlePrefill}
                disabled={prefillLoading}
                className="ui-link text-xs font-semibold disabled:opacity-50"
              >
                {prefillLoading ? 'Prefilling...' : 'Prefill from website'}
              </button>
            )}
          </div>
          <input
            type="url"
            id="website"
            {...register('website')}
            className="ui-input mt-2"
            placeholder="https://www.examplemedspa.com"
          />
          {prefillError && (
            <p className="mt-2 text-sm font-medium text-red-700">{prefillError}</p>
          )}
        </div>

        <div>
          <label htmlFor="email" className="ui-label">
            Business Email *
          </label>
          <input
            type="email"
            id="email"
            {...register('email')}
            className="ui-input mt-2"
            placeholder="hello@glowmedspa.com"
          />
          {errors.email && (
            <p className="mt-2 text-sm font-medium text-red-700">{errors.email.message}</p>
          )}
        </div>

        <div>
          <label htmlFor="phone" className="ui-label">
            Business Phone *
          </label>
          <input
            type="tel"
            id="phone"
            {...register('phone')}
            className="ui-input mt-2"
            placeholder="(555) 123-4567"
          />
          {errors.phone && (
            <p className="mt-2 text-sm font-medium text-red-700">{errors.phone.message}</p>
          )}
        </div>

        <div className="sm:col-span-2">
          <label htmlFor="address" className="ui-label">
            Street Address *
          </label>
          <input
            type="text"
            id="address"
            {...register('address')}
            className="ui-input mt-2"
            placeholder="123 Main St, Suite 100"
          />
          {errors.address && (
            <p className="mt-2 text-sm font-medium text-red-700">{errors.address.message}</p>
          )}
        </div>

        <div>
          <label htmlFor="city" className="ui-label">
            City *
          </label>
          <input
            type="text"
            id="city"
            {...register('city')}
            className="ui-input mt-2"
          />
          {errors.city && (
            <p className="mt-2 text-sm font-medium text-red-700">{errors.city.message}</p>
          )}
        </div>

        <div className="grid grid-cols-2 gap-4">
          <div>
            <label htmlFor="state" className="ui-label">
              State *
            </label>
            <input
              type="text"
              id="state"
              {...register('state')}
              className="ui-input mt-2"
              maxLength={2}
              placeholder="CA"
            />
            {errors.state && (
              <p className="mt-2 text-sm font-medium text-red-700">{errors.state.message}</p>
            )}
          </div>

          <div>
            <label htmlFor="zipCode" className="ui-label">
              Zip *
            </label>
            <input
              type="text"
              id="zipCode"
              {...register('zipCode')}
              className="ui-input mt-2"
              maxLength={10}
            />
            {errors.zipCode && (
              <p className="mt-2 text-sm font-medium text-red-700">{errors.zipCode.message}</p>
            )}
          </div>
        </div>

        <div>
          <label htmlFor="timezone" className="ui-label">
            Timezone *
          </label>
          <select
            id="timezone"
            {...register('timezone')}
            className="ui-select mt-2"
          >
            {US_TIMEZONES.map((tz) => (
              <option key={tz.value} value={tz.value}>
                {tz.label}
              </option>
            ))}
          </select>
          {errors.timezone && (
            <p className="mt-2 text-sm font-medium text-red-700">{errors.timezone.message}</p>
          )}
        </div>
      </div>

      <div className="flex justify-between pt-4">
        {onBack ? (
          <button
            type="button"
            onClick={onBack}
            className="ui-btn ui-btn-ghost"
          >
            Back
          </button>
        ) : (
          <div />
        )}
        <button
          type="submit"
          disabled={isSubmitting}
          className="ui-btn ui-btn-primary"
        >
          {isSubmitting ? 'Saving...' : 'Continue'}
        </button>
      </div>
    </form>
  );
}
