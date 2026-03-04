package clinic

import (
	"strings"
	"testing"
	"time"
)

func TestGetSMSRecipients(t *testing.T) {
	tests := []struct {
		name string
		np   NotificationPrefs
		want []string
	}{
		{"empty", NotificationPrefs{}, nil},
		{"legacy only", NotificationPrefs{SMSRecipient: "+11234567890"}, []string{"+11234567890"}},
		{"new only", NotificationPrefs{SMSRecipients: []string{"+11111111111", "+12222222222"}}, []string{"+11111111111", "+12222222222"}},
		{"both deduped", NotificationPrefs{SMSRecipient: "+11111111111", SMSRecipients: []string{"+11111111111", "+12222222222"}}, []string{"+11111111111", "+12222222222"}},
		{"empty strings filtered", NotificationPrefs{SMSRecipients: []string{"", "+11111111111", ""}}, []string{"+11111111111"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.np.GetSMSRecipients()
			if len(got) != len(tt.want) {
				t.Fatalf("GetSMSRecipients() = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("GetSMSRecipients()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestServiceNeedsProviderPreference(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *Config
		service string
		want    bool
	}{
		{"nil moxie config", &Config{}, "botox", false},
		{"nil maps", &Config{MoxieConfig: &MoxieConfig{}}, "botox", false},
		{
			"single provider",
			&Config{MoxieConfig: &MoxieConfig{
				ServiceMenuItems:     map[string]string{"tox": "20424"},
				ServiceProviderCount: map[string]int{"20424": 1},
			}},
			"Tox",
			false,
		},
		{
			"multiple providers",
			&Config{MoxieConfig: &MoxieConfig{
				ServiceMenuItems:     map[string]string{"tox": "20424"},
				ServiceProviderCount: map[string]int{"20424": 2},
			}},
			"Tox",
			true,
		},
		{
			"via alias",
			&Config{
				ServiceAliases: map[string]string{"botox": "Tox"},
				MoxieConfig: &MoxieConfig{
					ServiceMenuItems:     map[string]string{"tox": "20424"},
					ServiceProviderCount: map[string]int{"20424": 3},
				},
			},
			"Botox",
			true,
		},
		{
			"unknown service",
			&Config{MoxieConfig: &MoxieConfig{
				ServiceMenuItems:     map[string]string{"tox": "20424"},
				ServiceProviderCount: map[string]int{"20424": 2},
			}},
			"unknown",
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.ServiceNeedsProviderPreference(tt.service); got != tt.want {
				t.Errorf("ServiceNeedsProviderPreference(%q) = %v, want %v", tt.service, got, tt.want)
			}
		})
	}
}

func TestProviderNamesForService(t *testing.T) {
	cfg := &Config{
		ServiceAliases: map[string]string{"botox": "Tox"},
		MoxieConfig: &MoxieConfig{
			ServiceMenuItems: map[string]string{"tox": "38140"},
			ProviderNames: map[string]string{
				"34577": "Angela Solenthaler",
				"34572": "Brady Steineck",
				"34579": "Brandy Roberts",
				"34575": "McKenna Zehnder",
			},
			ServiceProviders: map[string][]string{
				"38140": {"34579", "34577", "34572"},
			},
		},
	}

	t.Run("service-specific list from alias", func(t *testing.T) {
		got := cfg.ProviderNamesForService("Botox")
		want := []string{"Angela Solenthaler", "Brady Steineck", "Brandy Roberts"}
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Fatalf("ProviderNamesForService(Botox) = %v, want %v", got, want)
		}
	})

	t.Run("no service mapping returns non-nil empty slice", func(t *testing.T) {
		cfg2 := &Config{MoxieConfig: &MoxieConfig{ProviderNames: cfg.MoxieConfig.ProviderNames}}
		got := cfg2.ProviderNamesForService("Unknown")
		if got == nil {
			t.Fatal("expected non-nil empty slice, got nil")
		}
		if len(got) != 0 {
			t.Fatalf("expected empty slice, got %v", got)
		}
	})
}

func TestGetServiceVariants(t *testing.T) {
	cfg := &Config{
		ServiceVariants: map[string][]string{
			"weight loss": {"Weight Loss - In Person", "Weight Loss - Virtual"},
			"solo":        {"Only One Option"},
		},
	}

	tests := []struct {
		name    string
		cfg     *Config
		service string
		wantNil bool
		wantLen int
	}{
		{"nil config", nil, "anything", true, 0},
		{"empty variants", &Config{}, "anything", true, 0},
		{"exact match", cfg, "weight loss", false, 2},
		{"case insensitive", cfg, "Weight Loss", false, 2},
		{"fuzzy contains", cfg, "weight loss consultation", false, 2},
		{"single variant ignored", cfg, "solo", true, 0},
		{"no match", cfg, "botox", true, 0},
		{"already a variant", cfg, "Weight Loss - In Person", true, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.cfg.GetServiceVariants(tt.service)
			if tt.wantNil && got != nil {
				t.Errorf("GetServiceVariants(%q) = %v, want nil", tt.service, got)
			}
			if !tt.wantNil && len(got) != tt.wantLen {
				t.Errorf("GetServiceVariants(%q) len = %d, want %d", tt.service, len(got), tt.wantLen)
			}
		})
	}
}

func TestUsesVagaroBooking(t *testing.T) {
	tests := []struct {
		name     string
		cfg      *Config
		expected bool
	}{
		{"nil", nil, false},
		{"empty", &Config{}, false},
		{"vagaro", &Config{BookingPlatform: "vagaro"}, true},
		{"Vagaro", &Config{BookingPlatform: "Vagaro"}, true},
		{"moxie", &Config{BookingPlatform: "moxie"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.UsesVagaroBooking(); got != tt.expected {
				t.Errorf("UsesVagaroBooking() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestUsesStripePayment(t *testing.T) {
	tests := []struct {
		name     string
		cfg      *Config
		expected bool
	}{
		{"nil", nil, false},
		{"empty", &Config{}, false},
		{"stripe", &Config{PaymentProvider: "stripe"}, true},
		{"Stripe", &Config{PaymentProvider: "Stripe"}, true},
		{"square", &Config{PaymentProvider: "square"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.UsesStripePayment(); got != tt.expected {
				t.Errorf("UsesStripePayment() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestDepositAmountForService(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *Config
		service string
		want    int
	}{
		{"nil config", nil, "botox", 0},
		{"default amount", &Config{DepositAmountCents: 5000}, "botox", 5000},
		{"per-service override", &Config{
			DepositAmountCents:        5000,
			ServiceDepositAmountCents: map[string]int{"botox": 7500},
		}, "Botox", 7500},
		{"unknown service falls back", &Config{
			DepositAmountCents:        5000,
			ServiceDepositAmountCents: map[string]int{"botox": 7500},
		}, "fillers", 5000},
		{"empty service name", &Config{DepositAmountCents: 5000}, "", 5000},
		{"zero default", &Config{}, "botox", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.DepositAmountForService(tt.service); got != tt.want {
				t.Errorf("DepositAmountForService(%q) = %d, want %d", tt.service, got, tt.want)
			}
		})
	}
}

func TestPriceTextForService(t *testing.T) {
	cfg := &Config{
		ServicePriceText: map[string]string{
			"botox":   "$12-15/unit",
			"fillers": "$600-800/syringe",
		},
	}

	tests := []struct {
		name      string
		cfg       *Config
		service   string
		wantText  string
		wantFound bool
	}{
		{"nil config", nil, "botox", "", false},
		{"nil map", &Config{}, "botox", "", false},
		{"found", cfg, "Botox", "$12-15/unit", true},
		{"not found", cfg, "laser", "", false},
		{"empty service", cfg, "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text, found := tt.cfg.PriceTextForService(tt.service)
			if text != tt.wantText || found != tt.wantFound {
				t.Errorf("PriceTextForService(%q) = (%q, %v), want (%q, %v)", tt.service, text, found, tt.wantText, tt.wantFound)
			}
		})
	}
}

func TestAIPersonaContext(t *testing.T) {
	tests := []struct {
		name        string
		cfg         *Config
		wantEmpty   bool
		wantContain string
	}{
		{"nil config", nil, true, ""},
		{"empty persona", &Config{}, true, ""},
		{
			"solo operator",
			&Config{
				Name: "Glow MedSpa",
				AIPersona: AIPersona{
					ProviderName:   "Brandi",
					IsSoloOperator: true,
				},
			},
			false, "SOLO PRACTITIONER",
		},
		{
			"provider name only",
			&Config{AIPersona: AIPersona{ProviderName: "Dr. Smith"}},
			false, "Primary provider",
		},
		{
			"custom greeting",
			&Config{AIPersona: AIPersona{CustomGreeting: "Hey there!"}},
			false, "GREETING STYLE",
		},
		{
			"after hours greeting",
			&Config{AIPersona: AIPersona{AfterHoursGreeting: "We're closed!"}},
			false, "AFTER HOURS",
		},
		{
			"busy message",
			&Config{AIPersona: AIPersona{BusyMessage: "With a patient"}},
			false, "BUSY MESSAGE",
		},
		{
			"tone clinical",
			&Config{AIPersona: AIPersona{Tone: "clinical"}},
			false, "Clinical and professional",
		},
		{
			"tone warm",
			&Config{AIPersona: AIPersona{Tone: "warm"}},
			false, "Warm and approachable",
		},
		{
			"tone professional",
			&Config{AIPersona: AIPersona{Tone: "professional"}},
			false, "Straightforward",
		},
		{
			"special services",
			&Config{AIPersona: AIPersona{SpecialServices: []string{"hyperhidrosis", "migraines"}}},
			false, "MEDICAL SERVICES NOTE",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.cfg.AIPersonaContext()
			if tt.wantEmpty && got != "" {
				t.Errorf("AIPersonaContext() = %q, want empty", got)
			}
			if !tt.wantEmpty && !strings.Contains(got, tt.wantContain) {
				t.Errorf("AIPersonaContext() missing %q, got: %s", tt.wantContain, got)
			}
		})
	}
}

func TestResolveServiceName_PluralAndFuzzy(t *testing.T) {
	cfg := &Config{
		ServiceAliases: map[string]string{
			"lip filler": "Dermal Filler - Lips",
			"filler":     "Dermal Filler",
		},
	}

	tests := []struct {
		input string
		want  string
	}{
		{"lip fillers", "Dermal Filler - Lips"},          // plural stripping
		{"fillers", "Dermal Filler"},                     // plural stripping
		{"lip filler treatment", "Dermal Filler - Lips"}, // fuzzy - contains "lip filler"
		{"unknown", "unknown"},                           // no match
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := cfg.ResolveServiceName(tt.input); got != tt.want {
				t.Errorf("ResolveServiceName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsOpenAt_NoBusinessHours(t *testing.T) {
	// Clinic with no business hours configured (appointment-only)
	cfg := &Config{
		Timezone:      "America/New_York",
		BusinessHours: BusinessHours{}, // all nil
	}
	loc, _ := time.LoadLocation("America/New_York")

	// 10 AM should be "open" (heuristic: 7-21)
	if !cfg.IsOpenAt(time.Date(2025, 12, 8, 10, 0, 0, 0, loc)) {
		t.Error("expected open at 10 AM with no hours configured")
	}
	// 11 PM should be "closed"
	if cfg.IsOpenAt(time.Date(2025, 12, 8, 23, 0, 0, 0, loc)) {
		t.Error("expected closed at 11 PM with no hours configured")
	}
	// 6 AM should be "closed"
	if cfg.IsOpenAt(time.Date(2025, 12, 8, 6, 0, 0, 0, loc)) {
		t.Error("expected closed at 6 AM with no hours configured")
	}
}

func TestIsOpenAt_InvalidTimezone(t *testing.T) {
	cfg := &Config{
		Timezone: "Invalid/Timezone",
		BusinessHours: BusinessHours{
			Monday: &DayHours{Open: "09:00", Close: "17:00"},
		},
	}
	// Should fall back to UTC without panic
	_ = cfg.IsOpenAt(time.Now())
}

func TestExpectedCallbackTime_CurrentlyOpen(t *testing.T) {
	cfg := DefaultConfig("test-org")
	loc, _ := time.LoadLocation("America/New_York")
	// Monday 10 AM - currently open
	monday10am := time.Date(2025, 12, 8, 10, 0, 0, 0, loc)
	result := cfg.ExpectedCallbackTime(monday10am)
	if result != "shortly" {
		t.Errorf("ExpectedCallbackTime when open = %q, want 'shortly'", result)
	}
}

func TestGetHoursForDay_AllDays(t *testing.T) {
	bh := BusinessHours{
		Sunday:    &DayHours{Open: "10:00", Close: "14:00"},
		Monday:    &DayHours{Open: "09:00", Close: "18:00"},
		Tuesday:   &DayHours{Open: "09:00", Close: "18:00"},
		Wednesday: &DayHours{Open: "09:00", Close: "18:00"},
		Thursday:  &DayHours{Open: "09:00", Close: "18:00"},
		Friday:    &DayHours{Open: "09:00", Close: "17:00"},
		Saturday:  &DayHours{Open: "10:00", Close: "15:00"},
	}
	for day := time.Sunday; day <= time.Saturday; day++ {
		if got := bh.GetHoursForDay(day); got == nil {
			t.Errorf("GetHoursForDay(%s) = nil, want non-nil", day)
		}
	}
}

func TestResolveProviderID_NilConfig(t *testing.T) {
	cfg := &Config{} // no MoxieConfig
	if got := cfg.ResolveProviderID("anyone"); got != "" {
		t.Errorf("ResolveProviderID on nil MoxieConfig = %q, want empty", got)
	}
}
