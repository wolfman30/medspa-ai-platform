package payments

import "testing"

func TestUsePaymentLinks(t *testing.T) {
	tests := []struct {
		mode    string
		sandbox bool
		want    bool
	}{
		{"payment_links", false, true},
		{"payment-links", false, true},
		{"paymentlinks", false, true},
		{"legacy", true, false},
		{"checkout", true, false},
		{"auto", true, true},
		{"auto", false, false},
		{"", true, true},
		{"", false, false},
		{"unknown", true, true},
		{"unknown", false, false},
		{" Payment_Links ", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.mode+"_sandbox_"+boolStr(tt.sandbox), func(t *testing.T) {
			if got := UsePaymentLinks(tt.mode, tt.sandbox); got != tt.want {
				t.Errorf("UsePaymentLinks(%q, %v) = %v, want %v", tt.mode, tt.sandbox, got, tt.want)
			}
		})
	}
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
