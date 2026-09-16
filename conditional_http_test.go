package s3lite

import (
	"net/http"
	"strings"
	"testing"
)

func doReqWithHeaders(t *testing.T, method, url, body string, headers map[string]string) *http.Response {
	t.Helper()
	var bodyReader *strings.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	} else {
		bodyReader = strings.NewReader("")
	}
	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestServerPutWithMetadata(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()
	doReq(t, http.MethodPut, ts.URL+"/b", "")

	doReqWithHeaders(t, http.MethodPut, ts.URL+"/b/k", "data", map[string]string{
		"X-Amz-Meta-Author": "ashish",
	})

	get := doReq(t, http.MethodGet, ts.URL+"/b/k", "")
	if got := get.Header.Get("X-Amz-Meta-Author"); got != "ashish" {
		t.Fatalf("X-Amz-Meta-Author = %q, want ashish", got)
	}
}

func TestServerConditionalPutIfNoneMatchStar(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()
	doReq(t, http.MethodPut, ts.URL+"/b", "")

	first := doReqWithHeaders(t, http.MethodPut, ts.URL+"/b/k", "v1", map[string]string{"If-None-Match": "*"})
	if first.StatusCode != 200 {
		t.Fatalf("first create-only PUT: status %d", first.StatusCode)
	}
	second := doReqWithHeaders(t, http.MethodPut, ts.URL+"/b/k", "v2", map[string]string{"If-None-Match": "*"})
	if second.StatusCode != 412 {
		t.Fatalf("second create-only PUT: status %d, want 412", second.StatusCode)
	}
}

func TestServerConditionalGetIfNoneMatch(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()
	doReq(t, http.MethodPut, ts.URL+"/b", "")
	put := doReq(t, http.MethodPut, ts.URL+"/b/k", "data")
	etag := put.Header.Get("ETag")

	notModified := doReqWithHeaders(t, http.MethodGet, ts.URL+"/b/k", "", map[string]string{"If-None-Match": etag})
	if notModified.StatusCode != 304 {
		t.Fatalf("If-None-Match matching current ETag: status %d, want 304", notModified.StatusCode)
	}

	changed := doReqWithHeaders(t, http.MethodGet, ts.URL+"/b/k", "", map[string]string{"If-None-Match": `"different"`})
	if changed.StatusCode != 200 {
		t.Fatalf("If-None-Match not matching: status %d, want 200", changed.StatusCode)
	}
}

func TestServerConditionalGetIfMatchFails(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()
	doReq(t, http.MethodPut, ts.URL+"/b", "")
	doReq(t, http.MethodPut, ts.URL+"/b/k", "data")

	resp := doReqWithHeaders(t, http.MethodGet, ts.URL+"/b/k", "", map[string]string{"If-Match": `"stale"`})
	if resp.StatusCode != 412 {
		t.Fatalf("If-Match mismatch: status %d, want 412", resp.StatusCode)
	}
}

func TestServerConditionalPutIfMatchOverHTTP(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()
	doReq(t, http.MethodPut, ts.URL+"/b", "")
	put := doReq(t, http.MethodPut, ts.URL+"/b/k", "v1")
	etag := put.Header.Get("ETag")

	ok := doReqWithHeaders(t, http.MethodPut, ts.URL+"/b/k", "v2", map[string]string{"If-Match": etag})
	if ok.StatusCode != 200 {
		t.Fatalf("conditional PUT against current ETag: status %d", ok.StatusCode)
	}

	stale := doReqWithHeaders(t, http.MethodPut, ts.URL+"/b/k", "v3", map[string]string{"If-Match": etag})
	if stale.StatusCode != http.StatusPreconditionFailed {
		t.Fatalf("conditional PUT against stale ETag: status %d, want %d", stale.StatusCode, http.StatusPreconditionFailed)
	}
}
