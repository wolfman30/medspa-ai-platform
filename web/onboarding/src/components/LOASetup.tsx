import { useState } from 'react';
import {
  checkHostedEligibility,
  startHostedOrder,
  uploadLOADocument,
  uploadPhoneBill,
} from '../api/client';

interface Props {
  orgId: string;
  clinicName: string;
  contactName?: string;
  contactEmail?: string;
  contactPhone?: string;
  onBack: () => void;
  onComplete: () => void;
}

type Step = 'check' | 'order' | 'upload' | 'done';

export function LOASetup({
  orgId,
  clinicName,
  contactName = '',
  contactEmail = '',
  contactPhone = '',
  onBack,
  onComplete,
}: Props) {
  const [step, setStep] = useState<Step>('check');
  const [phoneNumber, setPhoneNumber] = useState('');
  const [checking, setChecking] = useState(false);
  const [eligible, setEligible] = useState<boolean | null>(null);
  const [phoneType, setPhoneType] = useState('');
  const [eligibilityError, setEligibilityError] = useState<string | null>(null);

  const [orderId, setOrderId] = useState('');
  const [creating, setCreating] = useState(false);
  const [orderError, setOrderError] = useState<string | null>(null);

  const [loaFile, setLoaFile] = useState<File | null>(null);
  const [billFile, setBillFile] = useState<File | null>(null);
  const [uploading, setUploading] = useState(false);
  const [uploadError, setUploadError] = useState<string | null>(null);

  const handleCheckEligibility = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!phoneNumber.trim()) return;

    setChecking(true);
    setEligibilityError(null);
    setEligible(null);

    try {
      const result = await checkHostedEligibility(phoneNumber.trim());
      setEligible(result.eligible);
      setPhoneType(result.phone_type || 'unknown');
      if (result.eligible) {
        setStep('order');
      }
    } catch (err) {
      setEligibilityError(err instanceof Error ? err.message : 'Eligibility check failed');
    } finally {
      setChecking(false);
    }
  };

  const handleCreateOrder = async () => {
    setCreating(true);
    setOrderError(null);

    try {
      const result = await startHostedOrder({
        clinic_id: orgId,
        phone_number: phoneNumber,
        contact_name: contactName || clinicName,
        contact_email: contactEmail,
        contact_phone: contactPhone,
      });
      setOrderId(result.id);
      setStep('upload');
    } catch (err) {
      setOrderError(err instanceof Error ? err.message : 'Failed to create hosted order');
    } finally {
      setCreating(false);
    }
  };

  const handleUpload = async () => {
    if (!loaFile || !billFile) return;

    setUploading(true);
    setUploadError(null);

    try {
      await uploadLOADocument(orderId, loaFile);
      await uploadPhoneBill(orderId, billFile);
      setStep('done');
    } catch (err) {
      setUploadError(err instanceof Error ? err.message : 'Upload failed');
    } finally {
      setUploading(false);
    }
  };

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-xl font-semibold text-gray-900">SMS Number Setup (LOA)</h2>
        <p className="mt-1 text-sm text-gray-600">
          To send SMS from your clinic&apos;s existing phone number, we need a Letter of Authorization (LOA)
          and a recent phone bill. This lets us route text messages through your number while keeping
          your voice calls unchanged.
        </p>
      </div>

      {/* Step 1: Check Eligibility */}
      {step === 'check' && (
        <form onSubmit={handleCheckEligibility} className="space-y-4">
          <div>
            <label htmlFor="smsPhone" className="ui-label">
              Clinic Phone Number for SMS
            </label>
            <p className="mt-1 text-xs text-gray-500">
              Must be a <strong>landline or VoIP</strong> number. Cell/wireless numbers are not eligible for hosted SMS.
            </p>
            <input
              type="tel"
              id="smsPhone"
              value={phoneNumber}
              onChange={(e) => setPhoneNumber(e.target.value)}
              className="ui-input mt-2"
              placeholder="+1 (555) 123-4567"
            />
          </div>

          {eligible === false && (
            <div className="rounded-md bg-red-50 p-4">
              <p className="text-sm text-red-700">
                This number ({phoneType}) is <strong>not eligible</strong> for hosted SMS.
                {phoneType === 'cell' || phoneType === 'wireless'
                  ? ' Cell/wireless numbers cannot be hosted. You can use a new Telnyx number instead, or switch to a landline/VoIP number.'
                  : ' Please check the number and try again.'}
              </p>
            </div>
          )}

          {eligibilityError && (
            <div className="rounded-md bg-red-50 p-4">
              <p className="text-sm text-red-700">{eligibilityError}</p>
            </div>
          )}

          <div className="flex gap-3">
            <button type="button" onClick={onBack} className="ui-btn-secondary">
              Back
            </button>
            <button
              type="submit"
              disabled={checking || !phoneNumber.trim()}
              className="ui-btn-primary disabled:opacity-50"
            >
              {checking ? 'Checking...' : 'Check Eligibility'}
            </button>
          </div>
        </form>
      )}

      {/* Step 2: Create Hosted Order */}
      {step === 'order' && (
        <div className="space-y-4">
          <div className="rounded-md bg-green-50 p-4">
            <p className="text-sm text-green-700">
              ✅ <strong>{phoneNumber}</strong> is eligible for hosted SMS ({phoneType}).
            </p>
          </div>

          <p className="text-sm text-gray-600">
            Next, we&apos;ll create a hosting request with Telnyx. You&apos;ll then need to upload a signed LOA
            and a recent phone bill to verify ownership.
          </p>

          {orderError && (
            <div className="rounded-md bg-red-50 p-4">
              <p className="text-sm text-red-700">{orderError}</p>
            </div>
          )}

          <div className="flex gap-3">
            <button type="button" onClick={() => setStep('check')} className="ui-btn-secondary">
              Back
            </button>
            <button
              type="button"
              onClick={handleCreateOrder}
              disabled={creating}
              className="ui-btn-primary disabled:opacity-50"
            >
              {creating ? 'Creating...' : 'Create Hosting Request'}
            </button>
          </div>
        </div>
      )}

      {/* Step 3: Upload Documents */}
      {step === 'upload' && (
        <div className="space-y-4">
          <div className="rounded-md bg-blue-50 p-4">
            <p className="text-sm text-blue-700">
              Hosting request created. Please upload the following documents:
            </p>
          </div>

          <div>
            <label className="ui-label">
              Signed Letter of Authorization (LOA) *
            </label>
            <p className="mt-1 text-xs text-gray-500">
              An authorized representative of the clinic must sign the LOA granting permission to host SMS on this number.
            </p>
            <input
              type="file"
              accept=".pdf,.png,.jpg,.jpeg"
              onChange={(e) => setLoaFile(e.target.files?.[0] || null)}
              className="mt-2 block w-full text-sm text-gray-500 file:mr-4 file:rounded file:border-0 file:bg-indigo-50 file:px-4 file:py-2 file:text-sm file:font-semibold file:text-indigo-700 hover:file:bg-indigo-100"
            />
            {loaFile && (
              <p className="mt-1 text-xs text-green-600">✓ {loaFile.name}</p>
            )}
          </div>

          <div>
            <label className="ui-label">
              Recent Phone Bill *
            </label>
            <p className="mt-1 text-xs text-gray-500">
              A recent bill showing the clinic&apos;s ownership of {phoneNumber}. Must clearly show the phone number and account holder name.
            </p>
            <input
              type="file"
              accept=".pdf,.png,.jpg,.jpeg"
              onChange={(e) => setBillFile(e.target.files?.[0] || null)}
              className="mt-2 block w-full text-sm text-gray-500 file:mr-4 file:rounded file:border-0 file:bg-indigo-50 file:px-4 file:py-2 file:text-sm file:font-semibold file:text-indigo-700 hover:file:bg-indigo-100"
            />
            {billFile && (
              <p className="mt-1 text-xs text-green-600">✓ {billFile.name}</p>
            )}
          </div>

          {uploadError && (
            <div className="rounded-md bg-red-50 p-4">
              <p className="text-sm text-red-700">{uploadError}</p>
            </div>
          )}

          <div className="flex gap-3">
            <button type="button" onClick={() => setStep('order')} className="ui-btn-secondary">
              Back
            </button>
            <button
              type="button"
              onClick={handleUpload}
              disabled={uploading || !loaFile || !billFile}
              className="ui-btn-primary disabled:opacity-50"
            >
              {uploading ? 'Uploading...' : 'Upload Documents'}
            </button>
          </div>
        </div>
      )}

      {/* Step 4: Done */}
      {step === 'done' && (
        <div className="space-y-4">
          <div className="rounded-md bg-green-50 p-4">
            <div className="space-y-2">
              <p className="text-sm font-medium text-green-800">
                ✅ LOA and phone bill uploaded successfully!
              </p>
              <p className="text-sm text-green-700">
                Telnyx will review your documents and activate SMS hosting on <strong>{phoneNumber}</strong>.
                This typically takes <strong>1-3 business days</strong>. We&apos;ll notify you when it&apos;s approved.
              </p>
            </div>
          </div>

          <button
            type="button"
            onClick={onComplete}
            className="ui-btn-primary"
          >
            Continue
          </button>
        </div>
      )}
    </div>
  );
}
