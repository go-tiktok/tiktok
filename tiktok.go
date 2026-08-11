// Package tiktok is a pure-Go, dependency-free, best-effort read client for
// public TikTok content, talking to TikTok's undocumented web JSON endpoints.
//
// # Best-effort and fragile by nature
//
// TikTok does not publish or support a stable public web API. The endpoints
// used here are the ones its own website calls, and TikTok actively defends
// them. This client computes the "X-Bogus" request signature in pure Go (see
// [XBogus], verified against the public reference implementation) and folds it
// into every signed request, but X-Bogus alone is no longer sufficient: TikTok
// also requires a browser-minted msToken cookie and a newer "X-Gnarly"
// signature, both derived from JavaScript fingerprinting that cannot be
// reproduced without a browser. Supply an msToken and sessionid captured from a
// real logged-in browser via [WithMSToken] and [WithSessionID]. TikTok returns
// anti-bot responses (HTTP 403/429, or a 200 with an empty or "{}" body) when it
// decides a request looks automated.
//
// Consequently this client is BEST-EFFORT: it builds correct requests and
// parses correct responses, but it can and will break without notice when
// TikTok changes its web API or tightens its bot defenses. Use it accordingly,
// respect TikTok's Terms of Service, and do not rely on it for anything
// critical. Supplying msToken via [WithMSToken] and a sessionid via
// [WithSessionID] improves — but does not guarantee — success.
package tiktok

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// DefaultBaseURL is the default TikTok web origin.
const DefaultBaseURL = "https://www.tiktok.com"

// DefaultUserAgent is a plausible desktop browser User-Agent. TikTok inspects
// this header; an empty or obviously automated value is more likely blocked.
const DefaultUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) " +
	"AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"

// Client is a best-effort read client for public TikTok content.
//
// A zero Client is not ready for use; construct one with [New].
type Client struct {
	// BaseURL is the TikTok web origin (default https://www.tiktok.com).
	BaseURL string
	// HTTPClient performs requests (default http.DefaultClient).
	HTTPClient *http.Client
	// UserAgent is sent as the User-Agent header.
	UserAgent string
	// MSToken is TikTok's msToken, sent as both a query param and a cookie
	// when non-empty.
	MSToken string
	// SessionID is the sessionid cookie for authenticated reads, sent when
	// non-empty.
	SessionID string

	// now returns the current time; overridable in tests so a signed query is
	// deterministic. Defaults to time.Now.
	now func() time.Time
}

// Option configures a [Client].
type Option func(*Client)

// WithHTTPClient sets the underlying [http.Client].
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.HTTPClient = h }
}

// WithBaseURL overrides the TikTok web origin (useful for testing).
func WithBaseURL(u string) Option {
	return func(c *Client) { c.BaseURL = u }
}

// WithUserAgent overrides the User-Agent header.
func WithUserAgent(ua string) Option {
	return func(c *Client) { c.UserAgent = ua }
}

// WithMSToken sets the msToken query param / cookie.
func WithMSToken(t string) Option {
	return func(c *Client) { c.MSToken = t }
}

// WithSessionID sets the sessionid cookie for authenticated reads.
func WithSessionID(s string) Option {
	return func(c *Client) { c.SessionID = s }
}

// New constructs a [Client] with the given options applied.
func New(opts ...Option) *Client {
	c := &Client{
		BaseURL:    DefaultBaseURL,
		HTTPClient: http.DefaultClient,
		UserAgent:  DefaultUserAgent,
		now:        time.Now,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// signedQuery adds the msToken (when configured), encodes q, and appends the
// X-Bogus signature computed over that exact encoded string — the way TikTok's
// web client signs its item_list/user/list requests. It returns the full raw
// query string ready to place after "?".
func (c *Client) signedQuery(q url.Values) string {
	if c.MSToken != "" {
		q.Set("msToken", c.MSToken)
	}
	enc := q.Encode()
	nowFn := c.now
	if nowFn == nil {
		nowFn = time.Now
	}
	return enc + "&X-Bogus=" + XBogus(enc, c.UserAgent, nowFn().Unix())
}

// Video is a single public TikTok video.
type Video struct {
	ID          string
	Description string
	Author      string // unique_id / username
	Permalink   string // https://www.tiktok.com/@<author>/video/<id>
	CoverURL    string // thumbnail
	PlayURL     string // video URL (often expiring)
	Likes       int
	Comments    int
	Shares      int
	Plays       int
	CreateTime  time.Time
}

// UserFeed is a page of a user's videos.
type UserFeed struct {
	Username string
	Videos   []Video
	Cursor   string
	HasMore  bool
}

// webParams holds the constant query parameters TikTok's website sends on
// item_list requests. Kept in one place so they are easy to audit and update
// when TikTok changes its web API.
var webParams = map[string]string{
	"aid":              "1988",
	"app_language":     "en",
	"app_name":         "tiktok_web",
	"channel":          "tiktok_web",
	"device_platform":  "web_pc",
	"cookie_enabled":   "true",
	"history_len":      "1",
	"focus_state":      "true",
	"is_fullscreen":    "false",
	"is_page_visible":  "true",
	"language":         "en",
	"os":               "mac",
	"priority_region":  "",
	"referer":          "",
	"region":           "US",
	"screen_height":    "1080",
	"screen_width":     "1920",
	"tz_name":          "America/New_York",
	"webcast_language": "en",
}

// UserPosts fetches a user's recent videos via TikTok's web item_list API:
//
//	GET {BaseURL}/api/post/item_list/?secUid=<secUid>&count=<n>&cursor=<c>&...
//
// The secUid is TikTok's opaque secondary user id; the caller obtains it once
// (for example from a profile page's embedded JSON) and passes it here. count
// is the requested page size and cursor is the pagination cursor ("0" or ""
// for the first page). Headers (User-Agent, Referer) and cookies (sessionid,
// msToken) are set when configured.
//
// An empty result (no videos, HasMore=false) is returned without error. A
// non-2xx status, an empty/anti-bot body, or malformed JSON returns an error.
func (c *Client) UserPosts(ctx context.Context, secUid string, count int, cursor string) (*UserFeed, error) {
	q := c.baseValues()
	q.Set("secUid", secUid)
	q.Set("count", strconv.Itoa(count))
	q.Set("cursor", cursor)
	return c.itemList(ctx, "/api/post/item_list/", q)
}

// FollowingFeed fetches one page of the authenticated viewer's following feed —
// the videos from accounts they follow — via TikTok's web following item_list
// endpoint:
//
//	GET {BaseURL}/api/following/item_list/?count=<n>&maxCursor=<c>&...&X-Bogus=…
//
// It requires a session (see [WithSessionID]) and a browser-minted msToken (see
// [WithMSToken]); without a valid msToken TikTok answers the anti-bot empty body
// even though the X-Bogus signature is correct — see the note on [XBogus].
func (c *Client) FollowingFeed(ctx context.Context, count int, maxCursor string) (*UserFeed, error) {
	q := c.baseValues()
	q.Set("count", strconv.Itoa(count))
	q.Set("maxCursor", maxCursor)
	q.Set("minCursor", "0")
	return c.itemList(ctx, "/api/following/item_list/", q)
}

// Recommend fetches one page of the "For You" / home recommend feed via
// TikTok's web recommend item_list endpoint:
//
//	GET {BaseURL}/api/recommend/item_list/?count=<n>&...&X-Bogus=…
//
// Like [Client.FollowingFeed] it needs a browser-minted msToken to get past the
// anti-bot layer; the request is otherwise correctly signed.
func (c *Client) Recommend(ctx context.Context, count int, cursor string) (*UserFeed, error) {
	q := c.baseValues()
	q.Set("count", strconv.Itoa(count))
	q.Set("cursor", cursor)
	q.Set("pull_type", "0")
	return c.itemList(ctx, "/api/recommend/item_list/", q)
}

// baseValues returns a fresh url.Values seeded with the constant web parameters.
func (c *Client) baseValues() url.Values {
	q := url.Values{}
	for k, v := range webParams {
		q.Set(k, v)
	}
	return q
}

// itemList performs a signed GET against an item_list-shaped endpoint and maps
// the response to a [UserFeed]. It sets the standard headers/cookies, appends
// the X-Bogus signature, and classifies TikTok's anti-bot responses (non-2xx,
// empty body, or a non-zero in-band statusCode) as errors.
func (c *Client) itemList(ctx context.Context, path string, q url.Values) (*UserFeed, error) {
	endpoint := c.BaseURL + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+c.signedQuery(q), nil)
	if err != nil {
		return nil, err
	}
	c.decorate(req)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("tiktok: item_list request failed: status %d: %s",
			resp.StatusCode, snippet(body))
	}

	// TikTok's anti-bot layer answers a blocked request with an empty body or
	// a bare "{}" (HTTP 200). Treat an empty body as an explicit error; "{}"
	// decodes to a zero response and is handled below.
	if len(body) == 0 {
		return nil, fmt.Errorf("tiktok: empty response body (likely anti-bot block; " +
			"a browser-minted msToken is required)")
	}

	var raw itemListResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("tiktok: decode item_list response: %w", err)
	}
	// A non-zero statusCode is TikTok's in-band refusal (typically the signing /
	// msToken wall), returned with HTTP 200; surface it rather than a healthy
	// empty feed.
	if raw.StatusCode != 0 {
		return nil, fmt.Errorf("tiktok: item_list refused (statusCode %d %s); "+
			"a browser-minted msToken is required", raw.StatusCode, raw.StatusMsg)
	}

	feed := &UserFeed{
		Cursor:  raw.Cursor,
		HasMore: bool(raw.HasMore),
	}
	for _, it := range raw.ItemList {
		v := Video{
			ID:          it.ID,
			Description: it.Desc,
			Author:      it.Author.UniqueID,
			CoverURL:    it.Video.Cover,
			PlayURL:     it.Video.PlayAddr,
			Likes:       it.Stats.DiggCount,
			Comments:    it.Stats.CommentCount,
			Shares:      it.Stats.ShareCount,
			Plays:       it.Stats.PlayCount,
			CreateTime:  time.Unix(int64(it.CreateTime), 0).UTC(),
		}
		if v.Author != "" && v.ID != "" {
			v.Permalink = fmt.Sprintf("%s/@%s/video/%s", DefaultBaseURL, v.Author, v.ID)
			feed.Username = v.Author
		}
		feed.Videos = append(feed.Videos, v)
	}
	return feed, nil
}

// decorate sets the standard headers and cookies TikTok's web client sends.
func (c *Client) decorate(req *http.Request) {
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("Referer", DefaultBaseURL+"/")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	if c.SessionID != "" {
		req.AddCookie(&http.Cookie{Name: "sessionid", Value: c.SessionID})
	}
	if c.MSToken != "" {
		req.AddCookie(&http.Cookie{Name: "msToken", Value: c.MSToken})
	}
}

// snippet returns a short, printable prefix of a response body for use in
// error messages.
func snippet(b []byte) string {
	const max = 200
	if len(b) > max {
		return string(b[:max]) + "..."
	}
	return string(b)
}

// itemListResponse mirrors the relevant fields of TikTok's item_list JSON.
type itemListResponse struct {
	ItemList   []item     `json:"itemList"`
	Cursor     string     `json:"cursor"`
	HasMore    boolNumber `json:"hasMore"`
	StatusCode int        `json:"statusCode"`
	StatusMsg  string     `json:"statusMsg"`
}

type item struct {
	ID         string     `json:"id"`
	Desc       string     `json:"desc"`
	CreateTime unixTime   `json:"createTime"`
	Author     itemAuthor `json:"author"`
	Video      itemVideo  `json:"video"`
	Stats      itemStats  `json:"stats"`
}

type itemAuthor struct {
	UniqueID string `json:"uniqueId"`
}

type itemVideo struct {
	Cover    string `json:"cover"`
	PlayAddr string `json:"playAddr"`
}

type itemStats struct {
	DiggCount    int `json:"diggCount"`
	CommentCount int `json:"commentCount"`
	ShareCount   int `json:"shareCount"`
	PlayCount    int `json:"playCount"`
}

// unixTime tolerates TikTok's createTime arriving as either a JSON number or a
// quoted string of a Unix timestamp.
type unixTime int64

func (u *unixTime) UnmarshalJSON(b []byte) error {
	s := string(b)
	if s == "null" {
		*u = 0
		return nil
	}
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = s[1 : len(s)-1]
	}
	if s == "" {
		*u = 0
		return nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return fmt.Errorf("tiktok: parse createTime %q: %w", s, err)
	}
	*u = unixTime(n)
	return nil
}

// boolNumber tolerates hasMore arriving as a JSON bool, a number (0/1), or a
// quoted string.
type boolNumber bool

func (n *boolNumber) UnmarshalJSON(b []byte) error {
	s := string(b)
	switch s {
	case "null":
		*n = false
	case "true":
		*n = true
	case "false":
		*n = false
	default:
		if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
			s = s[1 : len(s)-1]
		}
		switch s {
		case "true", "1":
			*n = true
		case "false", "0", "":
			*n = false
		default:
			return fmt.Errorf("tiktok: parse hasMore %q", s)
		}
	}
	return nil
}
