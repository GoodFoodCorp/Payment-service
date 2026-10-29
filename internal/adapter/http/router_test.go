package http

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/rs/zerolog"

	"goodfood/payment-service/internal/domain"
)

const (
	testSecret     = "jwt-secret"
	testServiceKey = "service-key"
)

type rejectingVerifier struct{}

func (rejectingVerifier) Verify([]byte, string) (*domain.GatewayEvent, error) {
	return nil, errors.New("bad signature")
}

// newTestRouter wires the router without use cases: every request below is
// expected to be refused before reaching them.
func newTestRouter() http.Handler {
	return NewRouter(NewPaymentHandler(nil), NewWebhookHandler(nil, rejectingVerifier{}), RouterConfig{
		JWT:           JWTConfig{Secret: testSecret, Issuer: "goodfood-auth", Audience: "goodfood-api"},
		ServiceAPIKey: testServiceKey,
		Log:           zerolog.Nop(),
		DBCheck:       nil,
	})
}

func token(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()
	base := jwt.MapClaims{
		"sub": "cust-1", "typ": "access", "iss": "goodfood-auth", "aud": "goodfood-api",
		"exp": time.Now().Add(time.Hour).Unix(),
	}
	for k, v := range claims {
		base[k] = v
	}
	s, err := jwt.NewWithClaims(jwt.SigningMethodHS256, base).SignedString([]byte(testSecret))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func do(h http.Handler, method, path string, headers map[string]string) int {
	req := httptest.NewRequest(method, path, strings.NewReader(`{"order_id":"o1","amount_cents":1}`))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}

// The vulnerability being closed: a customer creating their own intent with
// an amount of their choosing.
func TestCustomersCannotCreateOrConfirmPayments(t *testing.T) {
	h := newTestRouter()
	bearer := map[string]string{"Authorization": "Bearer " + token(t, nil)}

	if code := do(h, http.MethodPost, "/api/payments/intents", bearer); code != http.StatusMethodNotAllowed && code != http.StatusNotFound {
		t.Fatalf("public intent creation must not exist, got %d", code)
	}
	if code := do(h, http.MethodPost, "/api/payments/o1/confirm", bearer); code != http.StatusMethodNotAllowed && code != http.StatusNotFound {
		t.Fatalf("public confirmation must not exist, got %d", code)
	}
	if code := do(h, http.MethodPost, "/internal/payments/intents", bearer); code != http.StatusUnauthorized {
		t.Fatalf("a user JWT must not open the internal API, got %d", code)
	}
}

func TestInternalRoutesRequireTheServiceKey(t *testing.T) {
	h := newTestRouter()
	for _, key := range []string{"", "wrong"} {
		headers := map[string]string{ServiceKeyHeader: key}
		if code := do(h, http.MethodPost, "/internal/payments/intents", headers); code != http.StatusUnauthorized {
			t.Fatalf("key %q: got %d", key, code)
		}
		if code := do(h, http.MethodPost, "/internal/payments/o1/refund", headers); code != http.StatusUnauthorized {
			t.Fatalf("key %q: got %d", key, code)
		}
	}
}

func TestAuthRejectsNonAccessTokens(t *testing.T) {
	h := newTestRouter()
	cases := map[string]string{
		"refresh token":  token(t, jwt.MapClaims{"typ": "refresh"}),
		"untyped legacy": token(t, jwt.MapClaims{"typ": nil}),
		"other audience": token(t, jwt.MapClaims{"aud": "elsewhere"}),
		"other issuer":   token(t, jwt.MapClaims{"iss": "elsewhere"}),
		"expired":        token(t, jwt.MapClaims{"exp": time.Now().Add(-time.Hour).Unix()}),
	}
	for name, tok := range cases {
		t.Run(name, func(t *testing.T) {
			if code := do(h, http.MethodGet, "/api/payments/me", map[string]string{"Authorization": "Bearer " + tok}); code != http.StatusUnauthorized {
				t.Fatalf("got %d", code)
			}
		})
	}
}

func TestWebhookRejectsBadSignature(t *testing.T) {
	if code := do(newTestRouter(), http.MethodPost, "/api/payments/webhooks/stripe", map[string]string{"Stripe-Signature": "t=1,v1=00"}); code != http.StatusBadRequest {
		t.Fatalf("got %d", code)
	}
}
