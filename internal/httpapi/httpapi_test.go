package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestServer(t *testing.T) (*httptest.Server, *Server) {
	t.Helper()
	s := New()
	ts := httptest.NewServer(s.Handler())
	return ts, s
}

func doRequest(t *testing.T, method, url string, body any) (int, map[string]any) {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		rdr = bytes.NewReader(buf)
	}
	req, err := http.NewRequest(method, url, rdr)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	out := map[string]any{}
	if len(data) > 0 {
		_ = json.Unmarshal(data, &out)
	}
	return resp.StatusCode, out
}

func mustCreate(t *testing.T, base, name string, precision int) {
	t.Helper()
	code, body := doRequest(t, http.MethodPost, base+"/hll/"+name, map[string]int{"precision": precision})
	if code != http.StatusCreated {
		t.Fatalf("create %s p=%d: got %d body=%v", name, precision, code, body)
	}
}

func TestCreateAndEstimateEmpty(t *testing.T) {
	ts, _ := newTestServer(t)
	defer ts.Close()

	code, body := doRequest(t, http.MethodPost, ts.URL+"/hll/empty", map[string]int{"precision": 10})
	if code != http.StatusCreated {
		t.Fatalf("create: got %d, want 201", code)
	}
	if body["registers"] != float64(1024) {
		t.Errorf("registers = %v, want 1024", body["registers"])
	}
	if body["errorBound"] == nil {
		t.Errorf("errorBound missing")
	}

	code, body = doRequest(t, http.MethodGet, ts.URL+"/hll/empty/estimate", nil)
	if code != http.StatusOK {
		t.Fatalf("estimate: got %d, want 200", code)
	}
	if body["estimate"] != float64(0) {
		t.Errorf("empty estimate = %v, want 0", body["estimate"])
	}
}

func TestCreatePrecisionOutOfRange(t *testing.T) {
	ts, _ := newTestServer(t)
	defer ts.Close()
	for _, p := range []int{3, 17} {
		code, body := doRequest(t, http.MethodPost, ts.URL+"/hll/bad", map[string]int{"precision": p})
		if code != http.StatusBadRequest {
			t.Errorf("precision=%d: got %d, want 400", p, code)
		}
		if body["error"] != "precision out of range [4,16]" {
			t.Errorf("precision=%d error = %v", p, body["error"])
		}
	}
}

func TestCreateConflict(t *testing.T) {
	ts, _ := newTestServer(t)
	defer ts.Close()
	mustCreate(t, ts.URL, "dup", 10)
	code, body := doRequest(t, http.MethodPost, ts.URL+"/hll/dup", map[string]int{"precision": 10})
	if code != http.StatusConflict {
		t.Fatalf("conflict: got %d, want 409", code)
	}
	if body["error"] != "hll already exists" {
		t.Errorf("conflict error = %v", body["error"])
	}
}

func TestAddAndEstimate(t *testing.T) {
	ts, _ := newTestServer(t)
	defer ts.Close()
	mustCreate(t, ts.URL, "acc", 12)
	vals := make([]string, 50000)
	for i := range vals {
		vals[i] = fmt.Sprintf("v-%d", i)
	}
	code, body := doRequest(t, http.MethodPost, ts.URL+"/hll/acc/add", map[string][]string{"values": vals})
	if code != http.StatusOK {
		t.Fatalf("add: got %d, want 200", code)
	}
	if body["added"] != float64(len(vals)) {
		t.Errorf("added = %v, want %d", body["added"], len(vals))
	}
	code, body = doRequest(t, http.MethodGet, ts.URL+"/hll/acc/estimate", nil)
	if code != http.StatusOK {
		t.Fatalf("estimate: got %d", code)
	}
	est := int64(body["estimate"].(float64))
	rel := float64(est-50000) / 50000
	if rel < 0 {
		rel = -rel
	}
	t.Logf("p=12 n=50000 estimate=%d relerr=%.3f%%", est, rel*100)
	if rel > 0.05 {
		t.Errorf("relerr %.3f%% exceeds 5%%", rel*100)
	}
}

func TestMergePrecisionMismatch(t *testing.T) {
	ts, _ := newTestServer(t)
	defer ts.Close()
	mustCreate(t, ts.URL, "a", 10)
	mustCreate(t, ts.URL, "b", 11)
	code, body := doRequest(t, http.MethodPost, ts.URL+"/hll/a/merge", map[string]string{"source": "b"})
	if code != http.StatusConflict {
		t.Errorf("merge mismatch: got %d, want 409", code)
	}
	if body["error"] != "precision mismatch" {
		t.Errorf("merge mismatch error = %v", body["error"])
	}
}

func TestMergeSelf(t *testing.T) {
	ts, _ := newTestServer(t)
	defer ts.Close()
	mustCreate(t, ts.URL, "s", 10)
	code, body := doRequest(t, http.MethodPost, ts.URL+"/hll/s/merge", map[string]string{"source": "s"})
	if code != http.StatusBadRequest {
		t.Errorf("merge self: got %d, want 400", code)
	}
	if body["error"] != "cannot merge into self" {
		t.Errorf("merge self error = %v", body["error"])
	}
}

func TestMissingHLL(t *testing.T) {
	ts, _ := newTestServer(t)
	defer ts.Close()
	code, _ := doRequest(t, http.MethodGet, ts.URL+"/hll/ghost/estimate", nil)
	if code != http.StatusNotFound {
		t.Errorf("missing estimate: got %d, want 404", code)
	}
	code, _ = doRequest(t, http.MethodPost, ts.URL+"/hll/ghost/add", map[string][]string{"values": {"x"}})
	if code != http.StatusNotFound {
		t.Errorf("missing add: got %d, want 404", code)
	}
}

func TestDelete(t *testing.T) {
	ts, _ := newTestServer(t)
	defer ts.Close()
	mustCreate(t, ts.URL, "d", 10)
	code, _ := doRequest(t, http.MethodDelete, ts.URL+"/hll/d", nil)
	if code != http.StatusNoContent {
		t.Fatalf("delete: got %d, want 204", code)
	}
	code, _ = doRequest(t, http.MethodGet, ts.URL+"/hll/d/estimate", nil)
	if code != http.StatusNotFound {
		t.Errorf("after delete estimate: got %d, want 404", code)
	}
}

func TestState(t *testing.T) {
	ts, _ := newTestServer(t)
	defer ts.Close()
	mustCreate(t, ts.URL, "st", 8)
	doRequest(t, http.MethodPost, ts.URL+"/hll/st/add", map[string][]string{"values": {"a", "b", "c"}})
	code, body := doRequest(t, http.MethodGet, ts.URL+"/hll/st/state", nil)
	if code != http.StatusOK {
		t.Fatalf("state: got %d", code)
	}
	if body["precision"] != float64(8) || body["registers"] != float64(256) {
		t.Errorf("state prec/regs = %v/%v", body["precision"], body["registers"])
	}
}
