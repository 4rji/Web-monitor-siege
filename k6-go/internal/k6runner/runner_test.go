package k6runner

import (
	"testing"
	"time"
)

func TestValidate(t *testing.T) {
	limits := Limits{MaxVUs: 100, MaxDuration: 10 * time.Minute}
	valid := Request{URL: "https://example.com", VUs: 10, Duration: "30s", P95Ms: 500, MaxFailureRate: 0.02}

	p, err := valid.Validate(limits)
	if err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}
	if p.Duration != 30*time.Second {
		t.Errorf("duration = %s, want 30s", p.Duration)
	}

	tests := []struct {
		name   string
		mutate func(*Request)
		ok     bool
	}{
		{"no scheme gets http", func(r *Request) { r.URL = "localhost:8080" }, true},
		{"ftp scheme", func(r *Request) { r.URL = "ftp://example.com" }, false},
		{"javascript scheme", func(r *Request) { r.URL = "javascript:alert(1)" }, false},
		{"empty url", func(r *Request) { r.URL = "  " }, false},
		{"zero vus", func(r *Request) { r.VUs = 0 }, false},
		{"too many vus", func(r *Request) { r.VUs = 101 }, false},
		{"bare seconds", func(r *Request) { r.Duration = "45" }, true},
		{"compound duration", func(r *Request) { r.Duration = "1m30s" }, true},
		{"negative duration", func(r *Request) { r.Duration = "-5s" }, false},
		{"sub-second duration", func(r *Request) { r.Duration = "300ms" }, false},
		{"duration over max", func(r *Request) { r.Duration = "11m" }, false},
		{"garbage duration", func(r *Request) { r.Duration = "30s'); evil()" }, false},
		{"zero p95", func(r *Request) { r.P95Ms = 0 }, false},
		{"zero failure rate", func(r *Request) { r.MaxFailureRate = 0 }, false},
		{"failure rate over 1", func(r *Request) { r.MaxFailureRate = 1.5 }, false},
		{"failure rate of 1", func(r *Request) { r.MaxFailureRate = 1 }, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := valid
			tc.mutate(&r)
			_, err := r.Validate(limits)
			if (err == nil) != tc.ok {
				t.Errorf("ok = %t, want %t (err: %v)", err == nil, tc.ok, err)
			}
		})
	}
}
