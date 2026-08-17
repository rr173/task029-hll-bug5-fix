// Package selfcheck runs an end-to-end verification of the HyperLogLog service
// against an in-process HTTP server. It is invoked by the --smoke-test flag and
// exits the process on completion.
package selfcheck

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"

	"task029-hll/internal/httpapi"
)

// Run exercises the full HTTP API and returns nil if every behavior matches the
// specification. On failure it returns an error describing the first mismatch.
func Run() error {
	srv := httpapi.New()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	c := ts.Client()

	// 1. Empty sketch estimates exactly zero.
	if err := create(c, ts.URL, "empty", 10); err != nil {
		return err
	}
	if est, err := estimate(c, ts.URL, "empty"); err != nil {
		return err
	} else if est != 0 {
		return fmt.Errorf("empty estimate = %d, want 0", est)
	}

	// 2. Precision bounds: 3 and 17 are rejected with 400.
	if code, body, err := createFails(c, ts.URL, "bad1", 3); err != nil {
		return err
	} else if code != http.StatusBadRequest || body["error"] != "precision out of range [4,16]" {
		return fmt.Errorf("precision=3: code=%d body=%v", code, body)
	}
	if code, body, err := createFails(c, ts.URL, "bad2", 17); err != nil {
		return err
	} else if code != http.StatusBadRequest || body["error"] != "precision out of range [4,16]" {
		return fmt.Errorf("precision=17: code=%d body=%v", code, body)
	}

	// 3. Accuracy: 100k distinct strings at p=10 within ±5%.
	if err := create(c, ts.URL, "acc", 10); err != nil {
		return err
	}
	vals := make([]string, 100000)
	for i := range vals {
		vals[i] = fmt.Sprintf("user-%d", i)
	}
	if _, err := add(c, ts.URL, "acc", vals); err != nil {
		return err
	}
	est, err := estimate(c, ts.URL, "acc")
	if err != nil {
		return err
	}
	rel := math.Abs(float64(est-100000)) / 100000
	fmt.Printf("[smoke] accuracy p=10 n=100000 estimate=%d relerr=%.3f%%\n", est, rel*100)
	if rel > 0.05 {
		return fmt.Errorf("accuracy relerr %.3f%% exceeds 5%%", rel*100)
	}

	// 4. Merge of overlapping sets: union cardinality 80000 within ±5%.
	if err := create(c, ts.URL, "A", 10); err != nil {
		return err
	}
	if err := create(c, ts.URL, "B", 10); err != nil {
		return err
	}
	aVals := make([]string, 50000)
	for i := range aVals {
		aVals[i] = fmt.Sprintf("k-%d", i+1)
	}
	if _, err := add(c, ts.URL, "A", aVals); err != nil {
		return err
	}
	bVals := make([]string, 50000)
	for i := range bVals {
		bVals[i] = fmt.Sprintf("k-%d", i+30001)
	}
	if _, err := add(c, ts.URL, "B", bVals); err != nil {
		return err
	}
	if err := merge(c, ts.URL, "A", "B"); err != nil {
		return err
	}
	estA, err := estimate(c, ts.URL, "A")
	if err != nil {
		return err
	}
	rel = math.Abs(float64(estA-80000)) / 80000
	fmt.Printf("[smoke] merge union=80000 estimate=%d relerr=%.3f%%\n", estA, rel*100)
	if rel > 0.05 {
		return fmt.Errorf("merge relerr %.3f%% exceeds 5%%", rel*100)
	}

	// 5. Merge precision mismatch → 409.
	if err := create(c, ts.URL, "C", 10); err != nil {
		return err
	}
	if err := create(c, ts.URL, "D", 11); err != nil {
		return err
	}
	if code, body, err := mergeFails(c, ts.URL, "C", "D"); err != nil {
		return err
	} else if code != http.StatusConflict || body["error"] != "precision mismatch" {
		return fmt.Errorf("merge mismatch: code=%d body=%v", code, body)
	}

	// 6. Merge into self → 400.
	if code, body, err := mergeFails(c, ts.URL, "C", "C"); err != nil {
		return err
	} else if code != http.StatusBadRequest || body["error"] != "cannot merge into self" {
		return fmt.Errorf("merge self: code=%d body=%v", code, body)
	}

	// 7. Missing sketch → 404 across endpoints.
	if code, _, err := get(c, ts.URL+"/hll/nope/estimate"); err != nil {
		return err
	} else if code != http.StatusNotFound {
		return fmt.Errorf("missing estimate: code=%d want 404", code)
	}
	if code, _, err := post(c, ts.URL+"/hll/nope/merge", map[string]string{"source": "A"}); err != nil {
		return err
	} else if code != http.StatusNotFound {
		return fmt.Errorf("missing merge source/dst: code=%d want 404", code)
	}

	// 8. Create conflict → 409.
	if code, body, err := createFails(c, ts.URL, "acc", 10); err != nil {
		return err
	} else if code != http.StatusConflict || body["error"] != "hll already exists" {
		return fmt.Errorf("conflict: code=%d body=%v", code, body)
	}

	// 9. State reports precision/registers/zeros.
	if code, body, err := get(c, ts.URL+"/hll/A/state"); err != nil {
		return err
	} else if code != http.StatusOK {
		return fmt.Errorf("state: code=%d", code)
	} else if body["precision"] != float64(10) || body["registers"] != float64(1024) {
		return fmt.Errorf("state: prec/regs=%v/%v", body["precision"], body["registers"])
	} else if body["zeros"] == nil {
		return fmt.Errorf("state: zeros missing")
	}

	// 10. Delete then 404.
	if code, _, err := del(c, ts.URL, "acc"); err != nil {
		return err
	} else if code != http.StatusNoContent {
		return fmt.Errorf("delete: code=%d want 204", code)
	}
	if code, _, err := get(c, ts.URL+"/hll/acc/estimate"); err != nil {
		return err
	} else if code != http.StatusNotFound {
		return fmt.Errorf("after delete: code=%d want 404", code)
	}

	return nil
}

// ---- HTTP helpers ----

func create(c *http.Client, base, name string, precision int) error {
	code, body, err := post(c, base+"/hll/"+name, map[string]int{"precision": precision})
	if err != nil {
		return err
	}
	if code != http.StatusCreated {
		return fmt.Errorf("create %s p=%d: code=%d body=%v", name, precision, code, body)
	}
	wantRegs := 1 << precision
	if body["registers"] != float64(wantRegs) {
		return fmt.Errorf("create %s: registers=%v want %d", name, body["registers"], wantRegs)
	}
	return nil
}

func createFails(c *http.Client, base, name string, precision int) (int, map[string]any, error) {
	return post(c, base+"/hll/"+name, map[string]int{"precision": precision})
}

func add(c *http.Client, base, name string, values []string) (int, error) {
	code, body, err := post(c, base+"/hll/"+name+"/add", map[string][]string{"values": values})
	if err != nil {
		return 0, err
	}
	if code != http.StatusOK {
		return code, fmt.Errorf("add %s: code=%d body=%v", name, code, body)
	}
	return code, nil
}

func estimate(c *http.Client, base, name string) (int64, error) {
	code, body, err := get(c, base+"/hll/"+name+"/estimate")
	if err != nil {
		return 0, err
	}
	if code != http.StatusOK {
		return 0, fmt.Errorf("estimate %s: code=%d body=%v", name, code, body)
	}
	v, _ := body["estimate"].(float64)
	return int64(v), nil
}

func merge(c *http.Client, base, dst, src string) error {
	code, body, err := post(c, base+"/hll/"+dst+"/merge", map[string]string{"source": src})
	if err != nil {
		return err
	}
	if code != http.StatusOK {
		return fmt.Errorf("merge %s<-%s: code=%d body=%v", dst, src, code, body)
	}
	return nil
}

func mergeFails(c *http.Client, base, dst, src string) (int, map[string]any, error) {
	return post(c, base+"/hll/"+dst+"/merge", map[string]string{"source": src})
}

func post(c *http.Client, url string, body any) (int, map[string]any, error) {
	buf, err := json.Marshal(body)
	if err != nil {
		return 0, nil, err
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	out := map[string]any{}
	if len(data) > 0 {
		_ = json.Unmarshal(data, &out)
	}
	return resp.StatusCode, out, nil
}

func get(c *http.Client, url string) (int, map[string]any, error) {
	resp, err := c.Get(url)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	out := map[string]any{}
	if len(data) > 0 {
		_ = json.Unmarshal(data, &out)
	}
	return resp.StatusCode, out, nil
}

func del(c *http.Client, base, name string) (int, map[string]any, error) {
	req, err := http.NewRequest(http.MethodDelete, base+"/hll/"+name, nil)
	if err != nil {
		return 0, nil, err
	}
	resp, err := c.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil, nil
}
