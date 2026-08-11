package tiktok

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const rehydrationTmpl = `<html><body>` +
	`<script id="__UNIVERSAL_DATA_FOR_REHYDRATION__" type="application/json">%s</script>` +
	`</body></html>`

func htmlPage(scopeJSON string) string {
	return strings.Replace(rehydrationTmpl, "%s", scopeJSON, 1)
}

func TestProfileSecUIDSuccess(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		io.WriteString(w, htmlPage(`{"__DEFAULT_SCOPE__":{"webapp.user-detail":`+
			`{"userInfo":{"user":{"secUid":"SEC123","uniqueId":"alice","nickname":"A"}}}}}`))
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL), WithMSToken("mt"))
	sec, err := c.ProfileSecUID(context.Background(), "@alice")
	if err != nil {
		t.Fatalf("ProfileSecUID: %v", err)
	}
	if sec != "SEC123" {
		t.Errorf("secUid = %q", sec)
	}
	if gotPath != "/@alice" {
		t.Errorf("path = %q", gotPath)
	}
}

func TestProfileSecUIDEmptyUsername(t *testing.T) {
	c := New()
	if _, err := c.ProfileSecUID(context.Background(), "  @  "); err == nil ||
		!strings.Contains(err.Error(), "empty username") {
		t.Fatalf("expected empty-username error, got %v", err)
	}
}

func TestProfileSecUIDNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, htmlPage(`{"__DEFAULT_SCOPE__":{"webapp.user-detail":`+
			`{"userInfo":{"user":{"secUid":""}}}}}`))
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	if _, err := c.ProfileSecUID(context.Background(), "ghost"); err == nil ||
		!strings.Contains(err.Error(), "no secUid found") {
		t.Fatalf("expected not-found error, got %v", err)
	}
}

func TestViewerSecUIDNeedsSession(t *testing.T) {
	c := New()
	if _, err := c.ViewerSecUID(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "needs a session") {
		t.Fatalf("expected needs-session error, got %v", err)
	}
}

func TestViewerSecUIDSuccess(t *testing.T) {
	var gotPath string
	var hadSession bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if ck, _ := r.Cookie("sessionid"); ck != nil && ck.Value == "sid" {
			hadSession = true
		}
		io.WriteString(w, htmlPage(`{"__DEFAULT_SCOPE__":{"webapp.app-context":`+
			`{"user":{"secUid":"VIEWER","uniqueId":"me"}}}}`))
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL), WithSessionID("sid"))
	sec, err := c.ViewerSecUID(context.Background())
	if err != nil {
		t.Fatalf("ViewerSecUID: %v", err)
	}
	if sec != "VIEWER" {
		t.Errorf("secUid = %q", sec)
	}
	if gotPath != "/foryou" || !hadSession {
		t.Errorf("path=%q session=%v", gotPath, hadSession)
	}
}

func TestViewerSecUIDAbsent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, htmlPage(`{"__DEFAULT_SCOPE__":{"webapp.app-context":{}}}`))
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL), WithSessionID("sid"))
	if _, err := c.ViewerSecUID(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "viewer secUid not present") {
		t.Fatalf("expected absent error, got %v", err)
	}
}

func TestViewerSecUIDFetchError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL), WithSessionID("sid"))
	if _, err := c.ViewerSecUID(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "status 503") {
		t.Fatalf("expected fetch error, got %v", err)
	}
}

func TestFetchRehydrationNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		io.WriteString(w, "nope")
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	if _, err := c.ProfileSecUID(context.Background(), "alice"); err == nil ||
		!strings.Contains(err.Error(), "status 403") {
		t.Fatalf("expected 403 error, got %v", err)
	}
}

func TestFetchRehydrationNoScript(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "<html>no data here</html>")
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	if _, err := c.ProfileSecUID(context.Background(), "alice"); err == nil ||
		!strings.Contains(err.Error(), "rehydration JSON not found") {
		t.Fatalf("expected not-found error, got %v", err)
	}
}

func TestFetchRehydrationBadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, htmlPage(`{not json`))
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	if _, err := c.ProfileSecUID(context.Background(), "alice"); err == nil ||
		!strings.Contains(err.Error(), "decode rehydration") {
		t.Fatalf("expected decode error, got %v", err)
	}
}

func TestFetchRehydrationRequestBuildError(t *testing.T) {
	c := New(WithBaseURL("http://\x7f.example"))
	if _, err := c.ProfileSecUID(context.Background(), "alice"); err == nil {
		t.Fatal("expected request build error")
	}
}

func TestFetchRehydrationDoError(t *testing.T) {
	c := New(WithHTTPClient(&http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("dial fail")
		}),
	}), WithBaseURL("http://example.test"))
	if _, err := c.ProfileSecUID(context.Background(), "alice"); err == nil ||
		!strings.Contains(err.Error(), "dial fail") {
		t.Fatalf("expected Do error, got %v", err)
	}
}

func TestFetchRehydrationReadError(t *testing.T) {
	c := New(WithHTTPClient(&http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: errReadCloser{}, Header: make(http.Header)}, nil
		}),
	}), WithBaseURL("http://example.test"))
	if _, err := c.ProfileSecUID(context.Background(), "alice"); err == nil ||
		!strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected read error, got %v", err)
	}
}
