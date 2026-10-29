package stripe

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

const secret = "whsec_test"

func signedHeader(t time.Time, payload []byte, key string) string {
	return fmt.Sprintf("t=%d,v1=%s", t.Unix(), sign(key, t.Unix(), payload))
}

func verifierAt(now time.Time) *WebhookVerifier {
	v := NewWebhookVerifier(secret)
	v.now = func() time.Time { return now }
	return v
}

func TestVerify_PaymentIntentEvent(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	payload := []byte(`{"id":"evt_1","type":"payment_intent.succeeded","data":{"object":{"id":"pi_1","object":"payment_intent"}}}`)

	ev, err := verifierAt(now).Verify(payload, signedHeader(now, payload, secret))

	if err != nil {
		t.Fatal(err)
	}
	if ev.Type != "payment_intent.succeeded" || ev.IntentID != "pi_1" || ev.ID != "evt_1" {
		t.Fatalf("unexpected event %+v", ev)
	}
}

func TestVerify_ChargeEventUsesItsPaymentIntent(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	payload := []byte(`{"id":"evt_2","type":"charge.refunded","data":{"object":{"id":"ch_1","object":"charge","payment_intent":"pi_9"}}}`)

	ev, err := verifierAt(now).Verify(payload, signedHeader(now, payload, secret))

	if err != nil || ev.IntentID != "pi_9" {
		t.Fatalf("got %+v, %v", ev, err)
	}
}

func TestVerify_Rejections(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	payload := []byte(`{"id":"evt_1","type":"payment_intent.succeeded","data":{"object":{"id":"pi_1","object":"payment_intent"}}}`)
	cases := map[string]struct {
		header   string
		verifier *WebhookVerifier
	}{
		"wrong secret":     {signedHeader(now, payload, "whsec_other"), verifierAt(now)},
		"tampered payload": {signedHeader(now, []byte(`{}`), secret), verifierAt(now)},
		"replayed later":   {signedHeader(now, payload, secret), verifierAt(now.Add(10 * time.Minute))},
		"missing header":   {"", verifierAt(now)},
		"no secret":        {signedHeader(now, payload, ""), NewWebhookVerifier("")},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := tc.verifier.Verify(payload, tc.header); !errors.Is(err, ErrInvalidSignature) {
				t.Fatalf("expected ErrInvalidSignature, got %v", err)
			}
		})
	}
}
