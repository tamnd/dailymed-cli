package dailymed

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// newTestClient returns a Client wired to a test server with pacing/retries off.
func newTestClient(srv *httptest.Server) *Client {
	c := NewClient()
	c.HTTP = srv.Client()
	c.Rate = 0
	c.Retries = 1
	return c
}

func TestGet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("request carried no User-Agent")
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	body, err := c.Get(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "ok" {
		t.Errorf("body = %q, want %q", body, "ok")
	}
}

func TestGetRetriesOn503(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("recovered"))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	c.Retries = 5

	start := time.Now()
	body, err := c.Get(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "recovered" {
		t.Errorf("body = %q after retries", body)
	}
	if hits != 3 {
		t.Errorf("server saw %d hits, want 3", hits)
	}
	if time.Since(start) < 500*time.Millisecond {
		t.Error("retries did not back off")
	}
}

func TestSearchSPLs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/dailymed/services/v2/spls.json" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("drug_name"); got != "aspirin" {
			t.Errorf("drug_name = %q, want aspirin", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"metadata":{"total_elements":2},
			"data":[
				{"setid":"aaa","title":"Aspirin 81 mg","published_date":"Jun 01, 2026"},
				{"setid":"bbb","title":"Aspirin 325 mg","published_date":"May 15, 2026"}
			]
		}`))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	c.HTTP.Transport = rewriteTransport{base: srv.Client().Transport, target: srv.URL}

	spls, err := c.SearchSPLs(context.Background(), "aspirin", 5, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(spls) != 2 {
		t.Fatalf("got %d SPLs, want 2", len(spls))
	}
	if spls[0].SetID != "aaa" {
		t.Errorf("spls[0].SetID = %q, want aaa", spls[0].SetID)
	}
	if spls[1].Title != "Aspirin 325 mg" {
		t.Errorf("spls[1].Title = %q, want Aspirin 325 mg", spls[1].Title)
	}
}

func TestGetSPL(t *testing.T) {
	const setid = "53c11fb4-ba31-b5e5-e063-6394a90a9c1a"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// GetSPL uses the packaging.json sub-endpoint.
		if r.URL.Path != "/dailymed/services/v2/spls/"+setid+"/packaging.json" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"data":{
				"setid":"` + setid + `",
				"title":"LOW DOSE ASPIRIN (ASPIRIN) TABLET, DELAYED RELEASE",
				"published_date":"Jun 10, 2026",
				"spl_version":1
			},
			"metadata":{}
		}`))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	c.HTTP.Transport = rewriteTransport{base: srv.Client().Transport, target: srv.URL}

	spl, err := c.GetSPL(context.Background(), setid)
	if err != nil {
		t.Fatal(err)
	}
	if spl.SetID != setid {
		t.Errorf("SetID = %q, want %q", spl.SetID, setid)
	}
	if spl.Title != "LOW DOSE ASPIRIN (ASPIRIN) TABLET, DELAYED RELEASE" {
		t.Errorf("Title = %q", spl.Title)
	}
}

func TestGetSPLNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Empty setid signals not found.
		_, _ = w.Write([]byte(`{"data":{"setid":"","title":"","published_date":""},"metadata":{}}`))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	c.HTTP.Transport = rewriteTransport{base: srv.Client().Transport, target: srv.URL}

	_, err := c.GetSPL(context.Background(), "no-such-setid")
	if err == nil {
		t.Error("expected error for empty setid in response, got nil")
	}
}

func TestListNDCs(t *testing.T) {
	const setid = "53c11fb4-ba31-b5e5-e063-6394a90a9c1a"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/dailymed/services/v2/spls/"+setid+"/ndcs.json" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		// Real wire format: data is an object with an "ndcs" array.
		_, _ = w.Write([]byte(`{
			"data":{
				"setid":"` + setid + `",
				"title":"LOW DOSE ASPIRIN",
				"published_date":"Jun 10, 2026",
				"ndcs":[
					{"ndc":"87438-0003-1"},
					{"ndc":"87438-0003-2"}
				]
			},
			"metadata":{"total_elements":2}
		}`))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	c.HTTP.Transport = rewriteTransport{base: srv.Client().Transport, target: srv.URL}

	ndcs, err := c.ListNDCs(context.Background(), setid, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(ndcs) != 2 {
		t.Fatalf("got %d NDCs, want 2", len(ndcs))
	}
	if ndcs[0].NDC != "87438-0003-1" {
		t.Errorf("ndcs[0].NDC = %q, want 87438-0003-1", ndcs[0].NDC)
	}
	if ndcs[1].NDC != "87438-0003-2" {
		t.Errorf("ndcs[1].NDC = %q, want 87438-0003-2", ndcs[1].NDC)
	}
}

func TestListNDCsLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"data":{
				"setid":"any",
				"ndcs":[
					{"ndc":"0001"},
					{"ndc":"0002"},
					{"ndc":"0003"}
				]
			},
			"metadata":{}
		}`))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	c.HTTP.Transport = rewriteTransport{base: srv.Client().Transport, target: srv.URL}

	ndcs, err := c.ListNDCs(context.Background(), "any-setid", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(ndcs) != 2 {
		t.Errorf("got %d NDCs with limit=2, want 2", len(ndcs))
	}
}

// rewriteTransport rewrites the host portion of each request to point at a
// test server, so we can use the real BaseURL-building code in the client
// while still hitting a local httptest.Server.
type rewriteTransport struct {
	base   http.RoundTripper
	target string
}

func (rt rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.URL.Scheme = "http"
	clone.URL.Host = rt.target[len("http://"):]
	return rt.base.RoundTrip(clone)
}
