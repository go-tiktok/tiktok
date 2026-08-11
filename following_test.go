package tiktok

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFollowingSuccess(t *testing.T) {
	var gotPath, gotUA, gotReferer, gotCookie string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path + "?" + r.URL.RawQuery
		gotUA = r.Header.Get("User-Agent")
		gotReferer = r.Header.Get("Referer")
		gotCookie = r.Header.Get("Cookie")
		_, _ = w.Write([]byte(`{
			"userList": [
				{"user": {"secUid": "SEC_ALICE", "uniqueId": "alice", "nickname": "Alice"}, "stats": {}},
				{"user": {"secUid": "SEC_BOB", "uniqueId": "bob", "nickname": ""}}
			],
			"maxCursor": 1700000000,
			"hasMore": true,
			"statusCode": 0
		}`))
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL), WithMSToken("MS1"), WithSessionID("SESS1"))
	list, err := c.Following(context.Background(), "SEC_VIEWER", 30, "0")
	if err != nil {
		t.Fatalf("Following: %v", err)
	}

	if !strings.Contains(gotPath, "/api/user/list/") {
		t.Errorf("path = %q", gotPath)
	}
	if !strings.Contains(gotPath, "scene=21") || !strings.Contains(gotPath, "secUid=SEC_VIEWER") ||
		!strings.Contains(gotPath, "count=30") || !strings.Contains(gotPath, "maxCursor=0") ||
		!strings.Contains(gotPath, "msToken=MS1") {
		t.Errorf("query params missing: %q", gotPath)
	}
	if gotUA != DefaultUserAgent {
		t.Errorf("User-Agent = %q", gotUA)
	}
	if gotReferer != DefaultBaseURL+"/" {
		t.Errorf("Referer = %q", gotReferer)
	}
	if !strings.Contains(gotCookie, "sessionid=SESS1") || !strings.Contains(gotCookie, "msToken=MS1") {
		t.Errorf("Cookie = %q", gotCookie)
	}

	if list.MaxCursor != "1700000000" || !list.HasMore {
		t.Errorf("cursor/hasMore = %q/%v", list.MaxCursor, list.HasMore)
	}
	if len(list.Users) != 2 {
		t.Fatalf("users = %d, want 2", len(list.Users))
	}
	if list.Users[0] != (FollowedUser{SecUID: "SEC_ALICE", UniqueID: "alice", Nickname: "Alice"}) {
		t.Errorf("user0 = %+v", list.Users[0])
	}
	if list.Users[1] != (FollowedUser{SecUID: "SEC_BOB", UniqueID: "bob", Nickname: ""}) {
		t.Errorf("user1 = %+v", list.Users[1])
	}
}

func TestFollowingTerminalPage(t *testing.T) {
	// maxCursor null (flexString null branch) and hasMore false.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"userList": [], "maxCursor": null, "hasMore": false, "statusCode": 0}`))
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	list, err := c.Following(context.Background(), "SEC", 30, "")
	if err != nil {
		t.Fatalf("Following: %v", err)
	}
	if list.MaxCursor != "" || list.HasMore || len(list.Users) != 0 {
		t.Errorf("list = %+v, want empty terminal page", list)
	}
}

func TestFollowingNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("blocked"))
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	_, err := c.Following(context.Background(), "SEC", 30, "")
	if err == nil || !strings.Contains(err.Error(), "status 403") {
		t.Fatalf("expected 403 error, got %v", err)
	}
}

func TestFollowingEmptyBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	_, err := c.Following(context.Background(), "SEC", 30, "")
	if err == nil || !strings.Contains(err.Error(), "empty response body") {
		t.Fatalf("expected empty-body error, got %v", err)
	}
}

func TestFollowingStatusCodeRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"userList": [], "statusCode": 10201, "statusMsg": "not logged in"}`))
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	_, err := c.Following(context.Background(), "SEC", 30, "")
	if err == nil || !strings.Contains(err.Error(), "statusCode 10201") ||
		!strings.Contains(err.Error(), "signed request") {
		t.Fatalf("expected in-band refusal error, got %v", err)
	}
}

func TestFollowingDecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{not json`))
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	_, err := c.Following(context.Background(), "SEC", 30, "")
	if err == nil || !strings.Contains(err.Error(), "decode user list") {
		t.Fatalf("expected decode error, got %v", err)
	}
}

func TestFollowingBuildRequestError(t *testing.T) {
	c := New(WithBaseURL("http://\x7f.example"))
	_, err := c.Following(context.Background(), "SEC", 30, "")
	if err == nil {
		t.Fatalf("expected build request error, got nil")
	}
}

func TestFollowingRequestFailed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close() // connection refused

	c := New(WithBaseURL(url))
	_, err := c.Following(context.Background(), "SEC", 30, "")
	if err == nil {
		t.Fatalf("expected transport error, got nil")
	}
}

func TestFollowingReadBodyError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "500")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			_, _ = w.Write([]byte("short"))
			f.Flush()
		}
		if hj, ok := w.(http.Hijacker); ok {
			conn, _, _ := hj.Hijack()
			_ = conn.Close()
		}
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	_, err := c.Following(context.Background(), "SEC", 30, "")
	if err == nil {
		t.Fatalf("expected read body error, got nil")
	}
}

func TestFlexStringQuoted(t *testing.T) {
	// The quoted-string branch: TikTok occasionally returns maxCursor as a string.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"userList": [], "maxCursor": "42", "hasMore": false, "statusCode": 0}`))
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	list, err := c.Following(context.Background(), "SEC", 30, "")
	if err != nil {
		t.Fatalf("Following: %v", err)
	}
	if list.MaxCursor != "42" {
		t.Errorf("MaxCursor = %q, want 42", list.MaxCursor)
	}
}
