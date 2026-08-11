package tiktok

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFollowingFeedSignedAndMapped(t *testing.T) {
	var gotReq *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotReq = r
		io.WriteString(w, `{"itemList":[{"id":"9","desc":"hi","createTime":1600000000,
			"author":{"uniqueId":"bob"},"video":{"cover":"c","playAddr":"p"},
			"stats":{"diggCount":1,"commentCount":2}}],"cursor":"c2","hasMore":true}`)
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL), WithMSToken("mt"), WithSessionID("sid"))
	feed, err := c.FollowingFeed(context.Background(), 12, "0")
	if err != nil {
		t.Fatalf("FollowingFeed: %v", err)
	}
	if !strings.HasSuffix(gotReq.URL.Path, "/api/following/item_list/") {
		t.Errorf("path = %q", gotReq.URL.Path)
	}
	if gotReq.URL.Query().Get("X-Bogus") == "" {
		t.Error("request not signed (no X-Bogus)")
	}
	if gotReq.URL.Query().Get("count") != "12" || gotReq.URL.Query().Get("maxCursor") != "0" {
		t.Errorf("params: %s", gotReq.URL.RawQuery)
	}
	if len(feed.Videos) != 1 || feed.Videos[0].ID != "9" || !feed.HasMore || feed.Cursor != "c2" {
		t.Fatalf("feed = %+v", feed)
	}
}

func TestRecommendSignedAndMapped(t *testing.T) {
	var gotReq *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotReq = r
		io.WriteString(w, `{"itemList":[{"id":"1","author":{"uniqueId":"eve"}}],"cursor":"","hasMore":false}`)
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	feed, err := c.Recommend(context.Background(), 8, "0")
	if err != nil {
		t.Fatalf("Recommend: %v", err)
	}
	if !strings.HasSuffix(gotReq.URL.Path, "/api/recommend/item_list/") {
		t.Errorf("path = %q", gotReq.URL.Path)
	}
	if gotReq.URL.Query().Get("pull_type") != "0" || gotReq.URL.Query().Get("X-Bogus") == "" {
		t.Errorf("params: %s", gotReq.URL.RawQuery)
	}
	if len(feed.Videos) != 1 || feed.Videos[0].ID != "1" {
		t.Fatalf("feed = %+v", feed)
	}
}

// TestItemListInBandRefusal covers the non-zero statusCode branch (TikTok's
// HTTP-200 anti-bot / msToken refusal).
func TestItemListInBandRefusal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"statusCode":10000,"statusMsg":"blocked"}`)
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	_, err := c.Recommend(context.Background(), 5, "0")
	if err == nil || !strings.Contains(err.Error(), "statusCode 10000") {
		t.Fatalf("expected in-band refusal error, got %v", err)
	}
	if !strings.Contains(err.Error(), "browser-minted msToken") {
		t.Errorf("expected msToken guidance, got %v", err)
	}
}
