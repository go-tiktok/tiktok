package tiktok

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// errReadCloser returns an error on Read to exercise the io.ReadAll branch.
type errReadCloser struct{}

func (errReadCloser) Read([]byte) (int, error) { return 0, errors.New("boom") }
func (errReadCloser) Close() error             { return nil }

// roundTripFunc adapts a function to an http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestNewDefaults(t *testing.T) {
	c := New()
	if c.BaseURL != DefaultBaseURL {
		t.Errorf("BaseURL = %q", c.BaseURL)
	}
	if c.HTTPClient != http.DefaultClient {
		t.Error("HTTPClient not default")
	}
	if c.UserAgent != DefaultUserAgent {
		t.Errorf("UserAgent = %q", c.UserAgent)
	}
}

func TestOptions(t *testing.T) {
	hc := &http.Client{}
	c := New(
		WithHTTPClient(hc),
		WithBaseURL("http://example.test"),
		WithUserAgent("ua/1"),
		WithMSToken("tok"),
		WithSessionID("sess"),
	)
	if c.HTTPClient != hc {
		t.Error("WithHTTPClient")
	}
	if c.BaseURL != "http://example.test" {
		t.Error("WithBaseURL")
	}
	if c.UserAgent != "ua/1" {
		t.Error("WithUserAgent")
	}
	if c.MSToken != "tok" {
		t.Error("WithMSToken")
	}
	if c.SessionID != "sess" {
		t.Error("WithSessionID")
	}
}

func TestUserPostsSuccess(t *testing.T) {
	var gotReq *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotReq = r
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{
			"itemList": [
				{
					"id": "123",
					"desc": "hello",
					"createTime": 1600000000,
					"author": {"uniqueId": "alice"},
					"video": {"cover": "c.jpg", "playAddr": "p.mp4"},
					"stats": {"diggCount": 10, "commentCount": 2, "shareCount": 3, "playCount": 100}
				},
				{
					"id": "",
					"desc": "no author or id",
					"createTime": "1600000001",
					"author": {"uniqueId": ""},
					"video": {"cover": "", "playAddr": ""},
					"stats": {}
				}
			],
			"cursor": "next",
			"hasMore": true
		}`)
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL), WithMSToken("mt"), WithSessionID("sid"))
	feed, err := c.UserPosts(context.Background(), "SEC", 5, "0")
	if err != nil {
		t.Fatalf("UserPosts: %v", err)
	}

	// Request assertions.
	if gotReq.Header.Get("User-Agent") != DefaultUserAgent {
		t.Error("missing UA")
	}
	if gotReq.Header.Get("Referer") != DefaultBaseURL+"/" {
		t.Error("missing Referer")
	}
	if gotReq.URL.Query().Get("secUid") != "SEC" {
		t.Error("secUid")
	}
	if gotReq.URL.Query().Get("count") != "5" {
		t.Error("count")
	}
	if gotReq.URL.Query().Get("cursor") != "0" {
		t.Error("cursor")
	}
	if gotReq.URL.Query().Get("aid") != "1988" {
		t.Error("constant param aid")
	}
	if gotReq.URL.Query().Get("msToken") != "mt" {
		t.Error("msToken query")
	}
	if ck, _ := gotReq.Cookie("sessionid"); ck == nil || ck.Value != "sid" {
		t.Error("sessionid cookie")
	}
	if ck, _ := gotReq.Cookie("msToken"); ck == nil || ck.Value != "mt" {
		t.Error("msToken cookie")
	}

	// Response assertions.
	if feed.Username != "alice" {
		t.Errorf("Username = %q", feed.Username)
	}
	if feed.Cursor != "next" || !feed.HasMore {
		t.Errorf("cursor/hasMore = %q/%v", feed.Cursor, feed.HasMore)
	}
	if len(feed.Videos) != 2 {
		t.Fatalf("videos = %d", len(feed.Videos))
	}
	v := feed.Videos[0]
	if v.ID != "123" || v.Description != "hello" || v.Author != "alice" {
		t.Errorf("video0 fields: %+v", v)
	}
	if v.Permalink != DefaultBaseURL+"/@alice/video/123" {
		t.Errorf("permalink = %q", v.Permalink)
	}
	if v.CoverURL != "c.jpg" || v.PlayURL != "p.mp4" {
		t.Errorf("cover/play: %q/%q", v.CoverURL, v.PlayURL)
	}
	if v.Likes != 10 || v.Comments != 2 || v.Shares != 3 || v.Plays != 100 {
		t.Errorf("stats: %+v", v)
	}
	if !v.CreateTime.Equal(time.Unix(1600000000, 0).UTC()) {
		t.Errorf("createTime = %v", v.CreateTime)
	}
	// Second item: string createTime, no permalink.
	v1 := feed.Videos[1]
	if v1.Permalink != "" {
		t.Errorf("expected empty permalink, got %q", v1.Permalink)
	}
	if !v1.CreateTime.Equal(time.Unix(1600000001, 0).UTC()) {
		t.Errorf("string createTime = %v", v1.CreateTime)
	}
}

func TestUserPostsEmptyResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"itemList": [], "cursor": "0", "hasMore": false}`)
	}))
	defer srv.Close()

	// No MSToken/SessionID: exercises the unset branches.
	c := New(WithBaseURL(srv.URL))
	feed, err := c.UserPosts(context.Background(), "SEC", 10, "")
	if err != nil {
		t.Fatalf("UserPosts: %v", err)
	}
	if len(feed.Videos) != 0 || feed.HasMore {
		t.Errorf("expected empty non-final: %+v", feed)
	}
}

func TestUserPostsUnsetTokenNoMSTokenQuery(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Has("msToken") {
			t.Error("msToken should be absent")
		}
		if _, err := r.Cookie("sessionid"); err == nil {
			t.Error("sessionid cookie should be absent")
		}
		io.WriteString(w, `{}`)
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	if _, err := c.UserPosts(context.Background(), "SEC", 1, "0"); err != nil {
		t.Fatalf("UserPosts: %v", err)
	}
}

func TestUserPostsNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		io.WriteString(w, strings.Repeat("x", 300)) // long body -> snippet truncation
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	_, err := c.UserPosts(context.Background(), "SEC", 1, "0")
	if err == nil || !strings.Contains(err.Error(), "status 403") {
		t.Fatalf("expected 403 error, got %v", err)
	}
	if !strings.Contains(err.Error(), "...") {
		t.Errorf("expected truncated snippet, got %v", err)
	}
}

func TestUserPostsEmptyBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 200 with zero-length body (anti-bot).
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	_, err := c.UserPosts(context.Background(), "SEC", 1, "0")
	if err == nil || !strings.Contains(err.Error(), "empty response body") {
		t.Fatalf("expected empty-body error, got %v", err)
	}
}

func TestUserPostsBadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{not json`)
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	_, err := c.UserPosts(context.Background(), "SEC", 1, "0")
	if err == nil || !strings.Contains(err.Error(), "decode item_list") {
		t.Fatalf("expected decode error, got %v", err)
	}
}

func TestUserPostsBadRequestURL(t *testing.T) {
	// Control byte in BaseURL makes http.NewRequestWithContext fail.
	c := New(WithBaseURL("http://\x7f.example"))
	_, err := c.UserPosts(context.Background(), "SEC", 1, "0")
	if err == nil {
		t.Fatal("expected request build error")
	}
}

func TestUserPostsDoError(t *testing.T) {
	c := New(WithHTTPClient(&http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("dial fail")
		}),
	}), WithBaseURL("http://example.test"))
	_, err := c.UserPosts(context.Background(), "SEC", 1, "0")
	if err == nil || !strings.Contains(err.Error(), "dial fail") {
		t.Fatalf("expected Do error, got %v", err)
	}
}

func TestUserPostsReadError(t *testing.T) {
	c := New(WithHTTPClient(&http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 200,
				Body:       errReadCloser{},
				Header:     make(http.Header),
			}, nil
		}),
	}), WithBaseURL("http://example.test"))
	_, err := c.UserPosts(context.Background(), "SEC", 1, "0")
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected read error, got %v", err)
	}
}

func TestSnippetShort(t *testing.T) {
	if got := snippet([]byte("short")); got != "short" {
		t.Errorf("snippet = %q", got)
	}
}

func TestUnixTimeUnmarshal(t *testing.T) {
	cases := []struct {
		in   string
		want int64
		err  bool
	}{
		{`1600000000`, 1600000000, false},
		{`"1600000001"`, 1600000001, false},
		{`null`, 0, false},
		{`""`, 0, false},
		{`"notanumber"`, 0, true},
	}
	for _, tc := range cases {
		var u unixTime
		err := json.Unmarshal([]byte(tc.in), &u)
		if tc.err {
			if err == nil {
				t.Errorf("%s: expected error", tc.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", tc.in, err)
			continue
		}
		if int64(u) != tc.want {
			t.Errorf("%s: got %d want %d", tc.in, int64(u), tc.want)
		}
	}
}

func TestBoolNumberUnmarshal(t *testing.T) {
	cases := []struct {
		in   string
		want bool
		err  bool
	}{
		{`true`, true, false},
		{`false`, false, false},
		{`null`, false, false},
		{`1`, true, false},
		{`0`, false, false},
		{`"true"`, true, false},
		{`"1"`, true, false},
		{`"false"`, false, false},
		{`"0"`, false, false},
		{`""`, false, false},
		{`"maybe"`, false, true},
		{`5`, false, true},
	}
	for _, tc := range cases {
		var n boolNumber
		err := json.Unmarshal([]byte(tc.in), &n)
		if tc.err {
			if err == nil {
				t.Errorf("%s: expected error", tc.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", tc.in, err)
			continue
		}
		if bool(n) != tc.want {
			t.Errorf("%s: got %v want %v", tc.in, bool(n), tc.want)
		}
	}
}
