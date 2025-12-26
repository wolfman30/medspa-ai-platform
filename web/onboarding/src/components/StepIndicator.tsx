interface Step {
  id: string;
  name: string;
}

interface Props {
  steps: Step[];
  currentStep: number;
}

export function StepIndicator({ steps, currentStep }: Props) {
  return (
    <nav aria-label="Progress" className="mb-8">
      <ol className="flex items-center">
        {steps.map((step, index) => (
          <li
            key={step.id}
            className={`relative ${index !== steps.length - 1 ? 'pr-8 sm:pr-20 flex-1' : ''}`}
          >
            <div className="flex items-center">
              <div
                className={`relative flex h-8 w-8 items-center justify-center rounded-full ${
                  index < currentStep
                    ? 'bg-indigo-600'
                    : index === currentStep
                    ? 'border-2 border-indigo-600 bg-white'
                    : 'border-2 border-gray-300 bg-white'
                }`}
              >
                {index < currentStep ? (
                  <svg
                    className="h-5 w-5 text-white"
                    viewBox="0 0 20 20"
                    fill="currentColor"
                  >
                    <path
                      fillRule="evenodd"
                      d="M16.707 5.293a1 1 0 010 1.414l-8 8a1 1 0 01-1.414 0l-4-4a1 1 0 011.414-1.414L8 12.586l7.293-7.293a1 1 0 011.414 0z"
                      clipRule="evenodd"
                    />
                  </svg>
                ) : (
                  <span
                    className={`text-sm font-medium ${
                      index === currentStep ? 'text-indigo-600' : 'text-gray-500'
                    }`}
                  >
                    {index + 1}
                  </span>
                )}
              </div>
              {index !== steps.length - 1 && (
                <div
                  className={`absolute top-4 left-8 -ml-px h-0.5 w-full sm:w-20 ${
                    index < currentStep ? 'bg-indigo-600' : 'bg-gray-300'
                  }`}
                />
              )}
            </div>
            <span className="mt-2 block text-xs font-medium text-gray-600 sm:text-sm">
              {step.name}
            </span>
          </li>
        ))}
      </ol>
    </nav>
  );
}
