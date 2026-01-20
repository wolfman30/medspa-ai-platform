// Onboarding form data types

export interface ClinicInfo {
  name: string;
  website: string;
  email: string;
  phone: string;
  address: string;
  city: string;
  state: string;
  zipCode: string;
  timezone: string;
}

export interface BusinessHours {
  monday: DayHours;
  tuesday: DayHours;
  wednesday: DayHours;
  thursday: DayHours;
  friday: DayHours;
  saturday: DayHours;
  sunday: DayHours;
}

export interface DayHours {
  open: string;
  close: string;
  closed: boolean;
}

export interface Service {
  name: string;
  description: string;
  durationMinutes: number;
  priceRange: string;
}

export interface KnowledgeBase {
  services: Service[];
  policies: string;
  faq: string;
}

export interface PaymentConfig {
  squareConnected: boolean;
  merchantId?: string;
  depositAmountCents: number;
}

export interface ContactInfo {
  emailEnabled: boolean;
  smsEnabled: boolean;
  emailRecipients: string[];
  smsRecipients: string[];
  notifyOnPayment: boolean;
  notifyOnNewLead: boolean;
}

export interface OnboardingData {
  clinicInfo: ClinicInfo;
  businessHours: BusinessHours;
  knowledge: KnowledgeBase;
  payment: PaymentConfig;
  contactInfo: ContactInfo;
}

export interface OnboardingStep {
  id: string;
  name: string;
  description: string;
  completed: boolean;
  required: boolean;
}

export interface OnboardingStatus {
  orgId: string;
  clinicName: string;
  overallProgress: number;
  readyForLaunch: boolean;
  steps: OnboardingStep[];
  nextAction?: string;
  nextActionUrl?: string;
}

// 10DLC Types
export interface BrandRegistration {
  clinicId: string;
  legalName: string;
  ein: string;
  website: string;
  addressLine: string;
  city: string;
  state: string;
  postalCode: string;
  country: string;
  contactName: string;
  contactEmail: string;
  contactPhone: string;
  vertical: string;
}

export interface CampaignRegistration {
  brandInternalId: string;
  useCase: string;
  description: string;
  sampleMessages: string[];
  messageFlow: string;
  optInDescription: string;
  optOutDescription: string;
  helpMessage: string;
  stopMessage: string;
}

export interface BrandResponse {
  brand_id: string;
  status: string;
  legal_name?: string;
}

export interface CampaignResponse {
  campaign_id: string;
  status: string;
  use_case: string;
}
