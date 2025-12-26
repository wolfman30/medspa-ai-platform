import { useState } from 'react';
import { useForm, useFieldArray } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { z } from 'zod';

const serviceSchema = z.object({
  name: z.string().min(2, 'Service name required'),
  description: z.string().min(10, 'Description required (min 10 chars)'),
  durationMinutes: z.number().min(15, 'Duration must be at least 15 minutes'),
  priceRange: z.string().min(1, 'Price range required'),
});

const servicesFormSchema = z.object({
  services: z.array(serviceSchema).min(1, 'Add at least one service'),
});

type ServicesFormData = z.infer<typeof servicesFormSchema>;

interface Props {
  defaultValues?: Partial<ServicesFormData>;
  onSubmit: (data: ServicesFormData) => void;
  onBack: () => void;
}

const COMMON_SERVICES = [
  { name: 'Botox', description: 'Botulinum toxin injections for wrinkle reduction', duration: 30, price: '$300-$600' },
  { name: 'Dermal Fillers', description: 'Hyaluronic acid fillers for volume restoration', duration: 45, price: '$500-$1200' },
  { name: 'Chemical Peel', description: 'Exfoliating treatment for skin rejuvenation', duration: 45, price: '$150-$400' },
  { name: 'Microneedling', description: 'Collagen induction therapy for skin texture improvement', duration: 60, price: '$250-$500' },
  { name: 'Laser Hair Removal', description: 'Permanent hair reduction using laser technology', duration: 30, price: '$100-$400' },
  { name: 'HydraFacial', description: 'Multi-step facial treatment for deep cleansing and hydration', duration: 60, price: '$200-$350' },
];

export function ServicesForm({ defaultValues, onSubmit, onBack }: Props) {
  const [showSuggestions, setShowSuggestions] = useState(true);

  const {
    register,
    control,
    handleSubmit,
    formState: { errors, isSubmitting },
  } = useForm<ServicesFormData>({
    resolver: zodResolver(servicesFormSchema),
    defaultValues: defaultValues || { services: [] },
  });

  const { fields, append, remove } = useFieldArray({
    control,
    name: 'services',
  });

  const addSuggestedService = (service: typeof COMMON_SERVICES[0]) => {
    append({
      name: service.name,
      description: service.description,
      durationMinutes: service.duration,
      priceRange: service.price,
    });
  };

  return (
    <form onSubmit={handleSubmit(onSubmit)} className="space-y-6">
      <div>
        <h2 className="text-xl font-semibold text-gray-900">Services & Pricing</h2>
        <p className="mt-1 text-sm text-gray-600">
          Add the services you offer. The AI will use this to help clients book appointments.
        </p>
      </div>

      {showSuggestions && fields.length === 0 && (
        <div className="bg-indigo-50 rounded-lg p-4">
          <h3 className="text-sm font-medium text-indigo-900 mb-3">Quick Add Common Services</h3>
          <div className="flex flex-wrap gap-2">
            {COMMON_SERVICES.map((service) => (
              <button
                key={service.name}
                type="button"
                onClick={() => addSuggestedService(service)}
                className="inline-flex items-center rounded-full bg-white px-3 py-1 text-sm text-indigo-700 border border-indigo-200 hover:bg-indigo-100"
              >
                + {service.name}
              </button>
            ))}
          </div>
          <button
            type="button"
            onClick={() => setShowSuggestions(false)}
            className="mt-2 text-xs text-indigo-600 hover:underline"
          >
            Hide suggestions
          </button>
        </div>
      )}

      <div className="space-y-4">
        {fields.map((field, index) => (
          <div key={field.id} className="bg-gray-50 rounded-lg p-4 relative">
            <button
              type="button"
              onClick={() => remove(index)}
              className="absolute top-2 right-2 text-gray-400 hover:text-red-500"
              title="Remove service"
            >
              <svg className="h-5 w-5" viewBox="0 0 20 20" fill="currentColor">
                <path
                  fillRule="evenodd"
                  d="M4.293 4.293a1 1 0 011.414 0L10 8.586l4.293-4.293a1 1 0 111.414 1.414L11.414 10l4.293 4.293a1 1 0 01-1.414 1.414L10 11.414l-4.293 4.293a1 1 0 01-1.414-1.414L8.586 10 4.293 5.707a1 1 0 010-1.414z"
                  clipRule="evenodd"
                />
              </svg>
            </button>

            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
              <div>
                <label className="block text-sm font-medium text-gray-700">
                  Service Name
                </label>
                <input
                  type="text"
                  {...register(`services.${index}.name`)}
                  className="mt-1 block w-full rounded-md border-gray-300 shadow-sm focus:border-indigo-500 focus:ring-indigo-500 sm:text-sm border px-3 py-2"
                />
                {errors.services?.[index]?.name && (
                  <p className="mt-1 text-sm text-red-600">
                    {errors.services[index]?.name?.message}
                  </p>
                )}
              </div>

              <div>
                <label className="block text-sm font-medium text-gray-700">
                  Price Range
                </label>
                <input
                  type="text"
                  {...register(`services.${index}.priceRange`)}
                  className="mt-1 block w-full rounded-md border-gray-300 shadow-sm focus:border-indigo-500 focus:ring-indigo-500 sm:text-sm border px-3 py-2"
                  placeholder="$200-$400"
                />
                {errors.services?.[index]?.priceRange && (
                  <p className="mt-1 text-sm text-red-600">
                    {errors.services[index]?.priceRange?.message}
                  </p>
                )}
              </div>

              <div>
                <label className="block text-sm font-medium text-gray-700">
                  Duration (minutes)
                </label>
                <input
                  type="number"
                  {...register(`services.${index}.durationMinutes`, { valueAsNumber: true })}
                  className="mt-1 block w-full rounded-md border-gray-300 shadow-sm focus:border-indigo-500 focus:ring-indigo-500 sm:text-sm border px-3 py-2"
                  min={15}
                  step={15}
                />
                {errors.services?.[index]?.durationMinutes && (
                  <p className="mt-1 text-sm text-red-600">
                    {errors.services[index]?.durationMinutes?.message}
                  </p>
                )}
              </div>

              <div className="sm:col-span-2">
                <label className="block text-sm font-medium text-gray-700">
                  Description
                </label>
                <textarea
                  {...register(`services.${index}.description`)}
                  rows={2}
                  className="mt-1 block w-full rounded-md border-gray-300 shadow-sm focus:border-indigo-500 focus:ring-indigo-500 sm:text-sm border px-3 py-2"
                  placeholder="Brief description for clients and the AI assistant..."
                />
                {errors.services?.[index]?.description && (
                  <p className="mt-1 text-sm text-red-600">
                    {errors.services[index]?.description?.message}
                  </p>
                )}
              </div>
            </div>
          </div>
        ))}
      </div>

      <button
        type="button"
        onClick={() =>
          append({ name: '', description: '', durationMinutes: 30, priceRange: '' })
        }
        className="inline-flex items-center rounded-md border border-gray-300 bg-white px-4 py-2 text-sm font-medium text-gray-700 shadow-sm hover:bg-gray-50"
      >
        <svg className="h-5 w-5 mr-2 text-gray-400" viewBox="0 0 20 20" fill="currentColor">
          <path
            fillRule="evenodd"
            d="M10 3a1 1 0 011 1v5h5a1 1 0 110 2h-5v5a1 1 0 11-2 0v-5H4a1 1 0 110-2h5V4a1 1 0 011-1z"
            clipRule="evenodd"
          />
        </svg>
        Add Custom Service
      </button>

      {errors.services?.root && (
        <p className="text-sm text-red-600">{errors.services.root.message}</p>
      )}

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
          disabled={isSubmitting || fields.length === 0}
          className="rounded-md border border-transparent bg-indigo-600 py-2 px-4 text-sm font-medium text-white shadow-sm hover:bg-indigo-700 focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:ring-offset-2 disabled:opacity-50"
        >
          {isSubmitting ? 'Saving...' : 'Continue'}
        </button>
      </div>
    </form>
  );
}
