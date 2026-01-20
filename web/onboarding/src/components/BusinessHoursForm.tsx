import { useEffect } from 'react';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { z } from 'zod';

const daySchema = z.object({
  open: z.string().min(1, 'Open time required'),
  close: z.string().min(1, 'Close time required'),
  closed: z.boolean(),
});

const businessHoursSchema = z.object({
  monday: daySchema,
  tuesday: daySchema,
  wednesday: daySchema,
  thursday: daySchema,
  friday: daySchema,
  saturday: daySchema,
  sunday: daySchema,
});

export type BusinessHoursFormData = z.infer<typeof businessHoursSchema>;

interface Props {
  defaultValues?: BusinessHoursFormData;
  onSubmit: (data: BusinessHoursFormData) => void;
  onBack: () => void;
}

const DEFAULT_HOURS: BusinessHoursFormData = {
  monday: { open: '09:00', close: '18:00', closed: false },
  tuesday: { open: '09:00', close: '18:00', closed: false },
  wednesday: { open: '09:00', close: '18:00', closed: false },
  thursday: { open: '09:00', close: '18:00', closed: false },
  friday: { open: '09:00', close: '17:00', closed: false },
  saturday: { open: '10:00', close: '16:00', closed: true },
  sunday: { open: '10:00', close: '16:00', closed: true },
};

const DAY_LABELS: Array<{ key: keyof BusinessHoursFormData; label: string }> = [
  { key: 'monday', label: 'Monday' },
  { key: 'tuesday', label: 'Tuesday' },
  { key: 'wednesday', label: 'Wednesday' },
  { key: 'thursday', label: 'Thursday' },
  { key: 'friday', label: 'Friday' },
  { key: 'saturday', label: 'Saturday' },
  { key: 'sunday', label: 'Sunday' },
];

export function BusinessHoursForm({ defaultValues, onSubmit, onBack }: Props) {
  const {
    register,
    handleSubmit,
    reset,
    watch,
    formState: { errors, isSubmitting },
  } = useForm<BusinessHoursFormData>({
    resolver: zodResolver(businessHoursSchema),
    defaultValues: defaultValues || DEFAULT_HOURS,
  });

  useEffect(() => {
    if (!defaultValues) return;
    reset(defaultValues);
  }, [defaultValues, reset]);

  const values = watch();

  return (
    <form onSubmit={handleSubmit(onSubmit)} className="space-y-6">
      <div>
        <h2 className="text-xl font-semibold text-gray-900">Business Hours</h2>
        <p className="mt-1 text-sm text-gray-600">
          Confirm when your clinic is open so the AI can set the right expectations.
        </p>
      </div>

      <div className="space-y-4">
        {DAY_LABELS.map(({ key, label }) => {
          const dayValues = values?.[key];
          const isClosed = dayValues?.closed ?? false;
          const dayErrors = errors?.[key] as
            | { open?: { message?: string }; close?: { message?: string } }
            | undefined;

          return (
            <div key={key} className="flex flex-col gap-3 rounded-lg border border-gray-200 p-4 sm:flex-row sm:items-center">
              <div className="sm:w-32">
                <span className="text-sm font-medium text-gray-900">{label}</span>
              </div>
              <div className="flex flex-1 flex-col gap-2 sm:flex-row sm:items-center">
                <input
                  type="time"
                  {...register(`${key}.open`)}
                  disabled={isClosed}
                  className="w-full rounded-md border border-gray-300 px-3 py-2 text-sm shadow-sm focus:border-indigo-500 focus:ring-indigo-500 disabled:bg-gray-100 sm:w-40"
                />
                <span className="text-sm text-gray-500">to</span>
                <input
                  type="time"
                  {...register(`${key}.close`)}
                  disabled={isClosed}
                  className="w-full rounded-md border border-gray-300 px-3 py-2 text-sm shadow-sm focus:border-indigo-500 focus:ring-indigo-500 disabled:bg-gray-100 sm:w-40"
                />
              </div>
              <label className="flex items-center gap-2 text-sm text-gray-600">
                <input type="checkbox" {...register(`${key}.closed`)} />
                Closed
              </label>
              {(dayErrors?.open || dayErrors?.close) && (
                <p className="text-sm text-red-600">
                  {dayErrors?.open?.message || dayErrors?.close?.message}
                </p>
              )}
            </div>
          );
        })}
      </div>

      <div className="flex justify-between pt-4">
        <button
          type="button"
          onClick={onBack}
          className="rounded-md border border-gray-300 bg-white py-2 px-4 text-sm font-medium text-gray-700 shadow-sm hover:bg-gray-50"
        >
          Back
        </button>
        <button
          type="submit"
          disabled={isSubmitting}
          className="rounded-md border border-transparent bg-indigo-600 py-2 px-4 text-sm font-medium text-white shadow-sm hover:bg-indigo-700 focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:ring-offset-2 disabled:opacity-50"
        >
          {isSubmitting ? 'Saving...' : 'Continue'}
        </button>
      </div>
    </form>
  );
}
