package deepseekusage

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchBalanceParses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user/balance" {
			t.Errorf("path=%q want /user/balance", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer key-abc" {
			t.Errorf("Authorization=%q want Bearer key-abc", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"is_available":true,"balance_infos":[` +
			`{"currency":"CNY","total_balance":"10.50","granted_balance":"5.00","topped_up_balance":"5.50"}]}`))
	}))
	defer srv.Close()

	b, err := fetchBalance("key-abc", srv.URL+"/user/balance")
	if err != nil {
		t.Fatal(err)
	}
	if b == nil || !b.IsAvailable || b.TotalBalance != "10.50" || b.Currency != "CNY" || b.GrantedBalance != "5.00" {
		t.Errorf("balance=%+v", b)
	}
}

func TestFetchBalanceEmptyToken(t *testing.T) {
	b, err := Fetch("")
	if err != nil || b != nil {
		t.Errorf("empty token should no-op, got b=%v err=%v", b, err)
	}
}

func TestFetchBalanceHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
	}))
	defer srv.Close()
	if _, err := fetchBalance("key", srv.URL+"/user/balance"); err == nil {
		t.Error("expected error on HTTP 401")
	}
}

func TestFetchBalanceEmptyBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"is_available":false,"balance_infos":[]}`))
	}))
	defer srv.Close()
	b, err := fetchBalance("key", srv.URL+"/user/balance")
	if err != nil {
		t.Fatal(err)
	}
	if b != nil {
		t.Errorf("empty balance_infos should yield nil, got %+v", b)
	}
}
