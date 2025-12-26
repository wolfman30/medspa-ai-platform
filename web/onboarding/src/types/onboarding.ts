// Onboarding form data types

export interface ClinicInfo {
  name: string;
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

export interface PhoneConfig {
  phoneNumber: string;
  status: 'pending' | 'verified' | 'active';
}

export interface OnboardingData {
  clinicInfo: ClinicInfo;
  businessHours: BusinessHours;
  knowledge: KnowledgeBase;
  payment: PaymentConfig;
  phone: PhoneConfig;
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
