package tiktok

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// followingScene is the "scene" parameter TikTok's web client sends on
// /api/user/list/ to request the following list (accounts a user follows).
const followingScene = "21"

// FollowedUser is one account the viewer follows, as returned by the user list.
type FollowedUser struct {
	SecUID   string // TikTok's opaque per-account id (the channel [Client.UserPosts] takes)
	UniqueID string // the @handle, without the "@"
	Nickname string // the display name ("" when the account sets none)
}

// FollowingList is one page of the viewer's following list. MaxCursor is the
// pagination cursor to pass on the next call, and HasMore reports whether another
// page exists.
type FollowingList struct {
	Users     []FollowedUser
	MaxCursor string
	HasMore   bool
}

// Following fetches one page of the accounts secUid follows via TikTok's web user
// list API:
//
//	GET {BaseURL}/api/user/list/?scene=21&secUid=<secUid>&count=<n>&maxCursor=<c>&...
//
// It sets the same web parameters, headers and cookies (User-Agent, Referer,
// sessionid, msToken) as [Client.UserPosts]. count is the requested page size and
// maxCursor is the pagination cursor ("0" or "" for the first page).
//
// IMPORTANT — this is behind TikTok's request-signing wall. The endpoint requires
// a valid signed parameter (X-Bogus / _signature) derived from the viewer's own
// secUid, which a pure-Go client cannot forge. Unsigned, TikTok answers an
// anti-bot response — a non-2xx status, an empty body, or a 200 whose statusCode
// field is non-zero — each returned here as an error. A caller should treat that
// as "this needs a signed/authenticated request" rather than as a transient bug.
func (c *Client) Following(ctx context.Context, secUid string, count int, maxCursor string) (*FollowingList, error) {
	endpoint := c.BaseURL + "/api/user/list/"

	q := url.Values{}
	for k, v := range webParams {
		q.Set(k, v)
	}
	q.Set("scene", followingScene)
	q.Set("secUid", secUid)
	q.Set("count", strconv.Itoa(count))
	q.Set("maxCursor", maxCursor)
	q.Set("minCursor", "0")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+c.signedQuery(q), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("Referer", DefaultBaseURL+"/")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	if c.SessionID != "" {
		req.AddCookie(&http.Cookie{Name: "sessionid", Value: c.SessionID})
	}
	if c.MSToken != "" {
		req.AddCookie(&http.Cookie{Name: "msToken", Value: c.MSToken})
	}

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
		return nil, fmt.Errorf("tiktok: user list request failed: status %d: %s",
			resp.StatusCode, snippet(body))
	}
	// TikTok's anti-bot layer answers a blocked request with an empty body.
	if len(body) == 0 {
		return nil, fmt.Errorf("tiktok: empty response body (likely anti-bot block; " +
			"the following list needs a signed request)")
	}

	var raw userListResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("tiktok: decode user list response: %w", err)
	}
	// A non-zero statusCode is TikTok's in-band refusal (typically the signing
	// wall), returned with HTTP 200; report it rather than a healthy empty list.
	if raw.StatusCode != 0 {
		return nil, fmt.Errorf("tiktok: user list refused (statusCode %d %s); "+
			"the following list needs a signed request", raw.StatusCode, raw.StatusMsg)
	}

	list := &FollowingList{
		MaxCursor: string(raw.MaxCursor),
		HasMore:   bool(raw.HasMore),
	}
	for _, it := range raw.UserList {
		list.Users = append(list.Users, FollowedUser{
			SecUID:   it.User.SecUID,
			UniqueID: it.User.UniqueID,
			Nickname: it.User.Nickname,
		})
	}
	return list, nil
}

// userListResponse mirrors the relevant fields of TikTok's user/list JSON.
type userListResponse struct {
	UserList   []userListItem `json:"userList"`
	MaxCursor  flexString     `json:"maxCursor"`
	HasMore    boolNumber     `json:"hasMore"`
	StatusCode int            `json:"statusCode"`
	StatusMsg  string         `json:"statusMsg"`
}

type userListItem struct {
	User struct {
		SecUID   string `json:"secUid"`
		UniqueID string `json:"uniqueId"`
		Nickname string `json:"nickname"`
	} `json:"user"`
}

// flexString decodes a value TikTok returns as either a JSON number (maxCursor is
// often a number) or a quoted string, yielding it as a plain string. A JSON null
// decodes to the empty string.
type flexString string

// UnmarshalJSON accepts a number, a quoted string, or null.
func (f *flexString) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "null" {
		s = ""
	}
	*f = flexString(s)
	return nil
}
