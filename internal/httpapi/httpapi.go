// Package httpapi exposes the HyperLogLog service over HTTP+JSON.
package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"

	"task029-hll/internal/hll"
)

// Server holds the in-memory collection of named sketches.
type Server struct {
	mu sync.Mutex
	m  map[string]*hll.HLL
}

// New returns a Server with an empty sketch collection.
func New() *Server {
	return &Server{m: make(map[string]*hll.HLL)}
}

// Handler returns the HTTP handler serving the HyperLogLog API.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/hll/", s.handle)
	return mux
}

// ---- request / response types ----

type createReq struct {
	Precision int `json:"precision"`
}

type createResp struct {
	Name       string  `json:"name"`
	Precision  int     `json:"precision"`
	Registers  int     `json:"registers"`
	ErrorBound float64 `json:"errorBound"`
}

type addReq struct {
	Values []string `json:"values"`
}

type addResp struct {
	Name  string `json:"name"`
	Added int    `json:"added"`
}

type estimateResp struct {
	Name      string `json:"name"`
	Estimate  int64  `json:"estimate"`
	Precision int    `json:"precision"`
}

type mergeReq struct {
	Source string `json:"source"`
}

type mergeResp struct {
	Name    string `json:"name"`
	Source  string `json:"source"`
	Merged  bool   `json:"merged"`
}

type stateResp struct {
	Name      string `json:"name"`
	Precision int    `json:"precision"`
	Registers int    `json:"registers"`
	Zeros     int    `json:"zeros"`
	Estimate  int64  `json:"estimate"`
}

type errResp struct {
	Error string `json:"error"`
}

// ---- routing ----

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/hll/")
	if rest == "" || strings.Contains(rest, "//") {
		writeJSON(w, http.StatusNotFound, errResp{Error: "not found"})
		return
	}
	parts := strings.SplitN(rest, "/", 2)
	name := parts[0]
	var sub string
	if len(parts) == 2 {
		sub = parts[1]
	}

	switch {
	case sub == "" && r.Method == http.MethodPost:
		s.create(w, r, name)
	case sub == "add" && r.Method == http.MethodPost:
		s.add(w, r, name)
	case sub == "estimate" && r.Method == http.MethodGet:
		s.estimate(w, r, name)
	case sub == "merge" && r.Method == http.MethodPost:
		s.merge(w, r, name)
	case sub == "state" && r.Method == http.MethodGet:
		s.state(w, r, name)
	case sub == "" && r.Method == http.MethodDelete:
		s.delete(w, r, name)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, errResp{Error: "method not allowed"})
	}
}

func (s *Server) create(w http.ResponseWriter, r *http.Request, name string) {
	var req createReq
	if err := decode(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, errResp{Error: "invalid request body"})
		return
	}
	sketch, err := hll.New(req.Precision)
	if err != nil {
		if errors.Is(err, hll.ErrPrecisionOutOfRange) {
			writeJSON(w, http.StatusBadRequest, errResp{Error: hll.ErrPrecisionOutOfRange.Error()})
			return
		}
		writeJSON(w, http.StatusInternalServerError, errResp{Error: err.Error()})
		return
	}

	s.mu.Lock()
	if _, ok := s.m[name]; ok {
		s.mu.Unlock()
		writeJSON(w, http.StatusConflict, errResp{Error: "hll already exists"})
		return
	}
	s.m[name] = sketch
	s.mu.Unlock()

	writeJSON(w, http.StatusCreated, createResp{
		Name:       name,
		Precision:  sketch.Precision(),
		Registers:  sketch.Registers(),
		ErrorBound: sketch.ErrorBound(),
	})
}

func (s *Server) add(w http.ResponseWriter, r *http.Request, name string) {
	var req addReq
	if err := decode(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, errResp{Error: "invalid request body"})
		return
	}
	s.mu.Lock()
	sketch, ok := s.m[name]
	s.mu.Unlock()
	if !ok {
		writeJSON(w, http.StatusNotFound, errResp{Error: "hll not found"})
		return
	}
	for _, v := range req.Values {
		sketch.Add([]byte(v))
	}
	writeJSON(w, http.StatusOK, addResp{Name: name, Added: len(req.Values)})
}

func (s *Server) estimate(w http.ResponseWriter, r *http.Request, name string) {
	s.mu.Lock()
	sketch, ok := s.m[name]
	s.mu.Unlock()
	if !ok {
		writeJSON(w, http.StatusNotFound, errResp{Error: "hll not found"})
		return
	}
	writeJSON(w, http.StatusOK, estimateResp{
		Name:      name,
		Estimate:  round(sketch.Estimate()),
		Precision: sketch.Precision(),
	})
}

func (s *Server) merge(w http.ResponseWriter, r *http.Request, name string) {
	var req mergeReq
	if err := decode(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, errResp{Error: "invalid request body"})
		return
	}
	if req.Source == "" {
		writeJSON(w, http.StatusBadRequest, errResp{Error: "source required"})
		return
	}
	if req.Source == name {
		writeJSON(w, http.StatusBadRequest, errResp{Error: "cannot merge into self"})
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	dst, ok := s.m[name]
	if !ok {
		writeJSON(w, http.StatusNotFound, errResp{Error: "hll not found"})
		return
	}
	src, ok := s.m[req.Source]
	if !ok {
		writeJSON(w, http.StatusNotFound, errResp{Error: "hll not found"})
		return
	}
	if err := dst.Merge(src); err != nil {
		if errors.Is(err, hll.ErrPrecisionMismatch) {
			writeJSON(w, http.StatusConflict, errResp{Error: hll.ErrPrecisionMismatch.Error()})
			return
		}
		writeJSON(w, http.StatusInternalServerError, errResp{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, mergeResp{Name: name, Source: req.Source, Merged: true})
}

func (s *Server) state(w http.ResponseWriter, r *http.Request, name string) {
	s.mu.Lock()
	sketch, ok := s.m[name]
	s.mu.Unlock()
	if !ok {
		writeJSON(w, http.StatusNotFound, errResp{Error: "hll not found"})
		return
	}
	writeJSON(w, http.StatusOK, stateResp{
		Name:      name,
		Precision: sketch.Precision(),
		Registers: sketch.Registers(),
		Zeros:     sketch.Zeros(),
		Estimate:  round(sketch.Estimate()),
	})
}

func (s *Server) delete(w http.ResponseWriter, r *http.Request, name string) {
	s.mu.Lock()
	_, ok := s.m[name]
	if ok {
		delete(s.m, name)
	}
	s.mu.Unlock()
	if !ok {
		writeJSON(w, http.StatusNotFound, errResp{Error: "hll not found"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- helpers ----

func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	defer r.Body.Close()
	return dec.Decode(v)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// round converts a float estimate to the nearest non-negative integer.
func round(f float64) int64 {
	if f <= 0 {
		return 0
	}
	return int64(mathRound(f))
}
