package ingest

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

const testSecret = "test-webhook-secret"

func init() {
	// All tests rely on this env var being set; one place, no per-test setup.
	_ = os.Setenv(EnvSigningSecret, testSecret)
}

// validBody is a minimal CMS payload the handler will accept.
func validBody() []byte {
	return []byte(`{"alert_id":"alert-1","account_alias":"prod","region":"cn-hangzhou","severity":"warning"}`)
}

func signBody(body []byte) string {
	mac := hmac.New(sha256.New, []byte(testSecret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// newRouter wires the handler with the given inserter.
func newRouter(insert AlertInserter) http.Handler {
	r := chi.NewRouter()
	Mount(r, insert, zap.NewNop())
	return r
}

// stubInserter is a configurable AlertInserter for tests.
type stubInserter struct {
	id  string
	err error
}

func (s stubInserter) call(ctx context.Context, a Alert) (string, error) {
	return s.id, s.err
}

func TestWebhook_ValidSignature_Returns200(t *testing.T) {
	ins := stubInserter{id: "db-row-42"}
	body := validBody()
	req := httptest.NewRequest(http.MethodPost, "/webhook/cms", bytes.NewReader(body))
	req.Header.Set(HeaderSignature, signBody(body))
	rec := httptest.NewRecorder()

	newRouter(ins.call).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode resp: %v", err)
	}
	if resp["status"] != "persisted" {
		t.Errorf("status field: got %v, want persisted", resp["status"])
	}
	if resp["alert_id"] != "db-row-42" {
		t.Errorf("alert_id: got %v, want db-row-42", resp["alert_id"])
	}
}

func TestWebhook_InvalidSignature_Returns401(t *testing.T) {
	ins := stubInserter{id: "should-not-be-called"}
	body := validBody()
	req := httptest.NewRequest(http.MethodPost, "/webhook/cms", bytes.NewReader(body))
	req.Header.Set(HeaderSignature, "deadbeefdeadbeef") // wrong
	rec := httptest.NewRecorder()

	newRouter(ins.call).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want 401", rec.Code)
	}
}

func TestWebhook_MissingSignature_Returns401(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/webhook/cms", bytes.NewReader(validBody()))
	rec := httptest.NewRecorder()
	newRouter(stubInserter{}.call).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want 401", rec.Code)
	}
}

func TestWebhook_BadJSON_Returns400(t *testing.T) {
	body := []byte(`{not-json`)
	req := httptest.NewRequest(http.MethodPost, "/webhook/cms", bytes.NewReader(body))
	req.Header.Set(HeaderSignature, signBody(body))
	rec := httptest.NewRecorder()

	newRouter(stubInserter{}.call).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestWebhook_MissingAlertID_Returns400(t *testing.T) {
	body := []byte(`{"region":"cn-hangzhou"}`)
	req := httptest.NewRequest(http.MethodPost, "/webhook/cms", bytes.NewReader(body))
	req.Header.Set(HeaderSignature, signBody(body))
	rec := httptest.NewRecorder()

	newRouter(stubInserter{}.call).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestWebhook_DBError_Returns500(t *testing.T) {
	ins := stubInserter{err: errStubDB}
	body := validBody()
	req := httptest.NewRequest(http.MethodPost, "/webhook/cms", bytes.NewReader(body))
	req.Header.Set(HeaderSignature, signBody(body))
	rec := httptest.NewRecorder()

	newRouter(ins.call).ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500", rec.Code)
	}
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["error"] == nil {
		t.Errorf("expected error field in 500 body, got %v", resp)
	}
}

// errStubDB is a sentinel DB error for the 500 test.
var errStubDB = stringErr("connection refused")

type stringErr string

func (e stringErr) Error() string { return string(e) }