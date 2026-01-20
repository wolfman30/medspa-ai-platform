import { useEffect, useState, type FormEvent } from 'react';

export interface ContactInfoFormData {
  emailEnabled: boolean;
  smsEnabled: boolean;
  emailRecipients: string[];
  smsRecipients: string[];
  notifyOnPayment: boolean;
  notifyOnNewLead: boolean;
}

interface Props {
  defaultValues?: ContactInfoFormData;
  onSubmit: (data: ContactInfoFormData) => void;
  onBack: () => void;
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

function formatPhone(phone: string): string {
  const digits = phone.replace(/\D/g, '');
  if (digits.length === 10) {
    return `(${digits.slice(0, 3)}) ${digits.slice(3, 6)}-${digits.slice(6)}`;
  }
  if (digits.length === 11 && digits.startsWith('1')) {
    return `+1 (${digits.slice(1, 4)}) ${digits.slice(4, 7)}-${digits.slice(7)}`;
  }
  return phone;
}

function isValidEmail(email: string): boolean {
  return /\S+@\S+\.\S+/.test(email);
}

const DEFAULT_CONTACT_INFO: ContactInfoFormData = {
  emailEnabled: false,
  smsEnabled: false,
  emailRecipients: [],
  smsRecipients: [],
  notifyOnPayment: true,
  notifyOnNewLead: false,
};

export function ContactInfoForm({ defaultValues, onSubmit, onBack }: Props) {
  const [emailEnabled, setEmailEnabled] = useState(DEFAULT_CONTACT_INFO.emailEnabled);
  const [smsEnabled, setSmsEnabled] = useState(DEFAULT_CONTACT_INFO.smsEnabled);
  const [emailRecipients, setEmailRecipients] = useState<string[]>(DEFAULT_CONTACT_INFO.emailRecipients);
  const [smsRecipients, setSmsRecipients] = useState<string[]>(DEFAULT_CONTACT_INFO.smsRecipients);
  const [notifyOnPayment, setNotifyOnPayment] = useState(DEFAULT_CONTACT_INFO.notifyOnPayment);
  const [notifyOnNewLead, setNotifyOnNewLead] = useState(DEFAULT_CONTACT_INFO.notifyOnNewLead);
  const [newEmail, setNewEmail] = useState('');
  const [newPhone, setNewPhone] = useState('');
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!defaultValues) return;
    setEmailEnabled(defaultValues.emailEnabled);
    setSmsEnabled(defaultValues.smsEnabled);
    setEmailRecipients(defaultValues.emailRecipients);
    setSmsRecipients(defaultValues.smsRecipients);
    setNotifyOnPayment(defaultValues.notifyOnPayment);
    setNotifyOnNewLead(defaultValues.notifyOnNewLead);
  }, [defaultValues]);

  const addEmail = () => {
    const value = newEmail.trim().toLowerCase();
    if (!value) return;
    if (!isValidEmail(value)) {
      setError('Enter a valid email address');
      return;
    }
    if (emailRecipients.includes(value)) {
      setError('This email is already added');
      return;
    }
    setEmailRecipients((prev) => [...prev, value]);
    setNewEmail('');
    setError(null);
  };

  const addPhone = () => {
    const value = newPhone.trim();
    if (!value) return;
    const normalized = normalizePhone(value);
    if (smsRecipients.includes(normalized)) {
      setError('This phone number is already added');
      return;
    }
    setSmsRecipients((prev) => [...prev, normalized]);
    setNewPhone('');
    setError(null);
  };

  const removeEmail = (email: string) => {
    setEmailRecipients((prev) => prev.filter((item) => item !== email));
  };

  const removePhone = (phone: string) => {
    setSmsRecipients((prev) => prev.filter((item) => item !== phone));
  };

  const handleSubmit = (e: FormEvent) => {
    e.preventDefault();
    if (emailEnabled && emailRecipients.length === 0) {
      setError('Add at least one email recipient or disable email notifications');
      return;
    }
    if (smsEnabled && smsRecipients.length === 0) {
      setError('Add at least one phone recipient or disable SMS notifications');
      return;
    }
    setError(null);
    onSubmit({
      emailEnabled,
      smsEnabled,
      emailRecipients,
      smsRecipients,
      notifyOnPayment,
      notifyOnNewLead,
    });
  };

  return (
    <form onSubmit={handleSubmit} className="space-y-6">
      <div>
        <h2 className="text-xl font-semibold text-gray-900">Contact Info</h2>
        <p className="mt-1 text-sm text-gray-600">
          Choose who should receive payment notifications and lead alerts.
        </p>
      </div>

      {error && (
        <div className="rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700">
          {error}
        </div>
      )}

      <div className="rounded-lg border border-gray-200 p-4">
        <h3 className="text-sm font-medium text-gray-900 mb-3">Notification Events</h3>
        <div className="space-y-3">
          <label className="flex items-center justify-between">
            <div>
              <span className="text-sm font-medium text-gray-700">Deposit Paid</span>
              <p className="text-xs text-gray-500">
                Send the patient name, service, and requested times.
              </p>
            </div>
            <button
              type="button"
              onClick={() => setNotifyOnPayment((value) => !value)}
              className={`relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors ${
                notifyOnPayment ? 'bg-indigo-600' : 'bg-gray-200'
              }`}
            >
              <span
                className={`pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow transition ${
                  notifyOnPayment ? 'translate-x-5' : 'translate-x-0'
                }`}
              />
            </button>
          </label>

          <label className="flex items-center justify-between">
            <div>
              <span className="text-sm font-medium text-gray-700">New Lead</span>
              <p className="text-xs text-gray-500">
                Notify staff when a new lead submits the form.
              </p>
            </div>
            <button
              type="button"
              onClick={() => setNotifyOnNewLead((value) => !value)}
              className={`relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors ${
                notifyOnNewLead ? 'bg-indigo-600' : 'bg-gray-200'
              }`}
            >
              <span
                className={`pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow transition ${
                  notifyOnNewLead ? 'translate-x-5' : 'translate-x-0'
                }`}
              />
            </button>
          </label>
        </div>
      </div>

      <div className="rounded-lg border border-gray-200 p-4">
        <div className="flex items-center justify-between">
          <h3 className="text-sm font-medium text-gray-900">Email Recipients</h3>
          <button
            type="button"
            onClick={() => setEmailEnabled((value) => !value)}
            className={`relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors ${
              emailEnabled ? 'bg-indigo-600' : 'bg-gray-200'
            }`}
          >
            <span
              className={`pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow transition ${
                emailEnabled ? 'translate-x-5' : 'translate-x-0'
              }`}
            />
          </button>
        </div>

        {emailEnabled && (
          <div className="mt-3 space-y-3">
            <div className="flex gap-2">
              <input
                type="email"
                value={newEmail}
                onChange={(e) => setNewEmail(e.target.value)}
                placeholder="owner@clinic.com"
                className="flex-1 rounded-md border border-gray-300 px-3 py-2 text-sm shadow-sm focus:border-indigo-500 focus:ring-indigo-500"
              />
              <button
                type="button"
                onClick={addEmail}
                disabled={!newEmail.trim()}
                className="rounded-md bg-indigo-600 px-3 py-2 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-50"
              >
                Add
              </button>
            </div>
            {emailRecipients.length === 0 ? (
              <p className="text-xs text-gray-500">No email recipients added yet.</p>
            ) : (
              <div className="space-y-2">
                {emailRecipients.map((email) => (
                  <div key={email} className="flex items-center justify-between rounded-md bg-gray-50 px-3 py-2 text-sm">
                    <span className="text-gray-700">{email}</span>
                    <button
                      type="button"
                      onClick={() => removeEmail(email)}
                      className="text-xs text-red-600 hover:text-red-700"
                    >
                      Remove
                    </button>
                  </div>
                ))}
              </div>
            )}
          </div>
        )}
      </div>

      <div className="rounded-lg border border-gray-200 p-4">
        <div className="flex items-center justify-between">
          <h3 className="text-sm font-medium text-gray-900">SMS Recipients</h3>
          <button
            type="button"
            onClick={() => setSmsEnabled((value) => !value)}
            className={`relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors ${
              smsEnabled ? 'bg-indigo-600' : 'bg-gray-200'
            }`}
          >
            <span
              className={`pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow transition ${
                smsEnabled ? 'translate-x-5' : 'translate-x-0'
              }`}
            />
          </button>
        </div>

        {smsEnabled && (
          <div className="mt-3 space-y-3">
            <div className="flex gap-2">
              <input
                type="tel"
                value={newPhone}
                onChange={(e) => setNewPhone(e.target.value)}
                placeholder="(555) 123-4567"
                className="flex-1 rounded-md border border-gray-300 px-3 py-2 text-sm shadow-sm focus:border-indigo-500 focus:ring-indigo-500"
              />
              <button
                type="button"
                onClick={addPhone}
                disabled={!newPhone.trim()}
                className="rounded-md bg-indigo-600 px-3 py-2 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-50"
              >
                Add
              </button>
            </div>
            {smsRecipients.length === 0 ? (
              <p className="text-xs text-gray-500">No SMS recipients added yet.</p>
            ) : (
              <div className="space-y-2">
                {smsRecipients.map((phone) => (
                  <div key={phone} className="flex items-center justify-between rounded-md bg-gray-50 px-3 py-2 text-sm">
                    <span className="text-gray-700">{formatPhone(phone)}</span>
                    <button
                      type="button"
                      onClick={() => removePhone(phone)}
                      className="text-xs text-red-600 hover:text-red-700"
                    >
                      Remove
                    </button>
                  </div>
                ))}
              </div>
            )}
          </div>
        )}
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
          className="rounded-md border border-transparent bg-indigo-600 py-2 px-4 text-sm font-medium text-white shadow-sm hover:bg-indigo-700 focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:ring-offset-2"
        >
          Complete Setup
        </button>
      </div>
    </form>
  );
}
