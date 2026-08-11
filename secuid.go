package tiktok

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

// rehydrationRe extracts the JSON payload TikTok embeds in every web page under
// the __UNIVERSAL_DATA_FOR_REHYDRATION__ script tag. secUids and the viewer's
// own account live inside it.
var rehydrationRe = regexp.MustCompile(
	`(?s)<script id="__UNIVERSAL_DATA_FOR_REHYDRATION__" type="application/json">(.*?)</script>`)

// rehydration mirrors the two scopes we read out of the embedded JSON: the
// viewer's app context (populated when a sessionid is present) and a profile
// page's user-detail block.
type rehydration struct {
	DefaultScope struct {
		AppContext struct {
			// User is the authenticated viewer, present on an app page loaded
			// with a valid session.
			User struct {
				SecUID   string `json:"secUid"`
				UniqueID string `json:"uniqueId"`
			} `json:"user"`
		} `json:"webapp.app-context"`
		UserDetail struct {
			UserInfo struct {
				User struct {
					SecUID   string `json:"secUid"`
					UniqueID string `json:"uniqueId"`
					Nickname string `json:"nickname"`
				} `json:"user"`
			} `json:"userInfo"`
		} `json:"webapp.user-detail"`
	} `json:"__DEFAULT_SCOPE__"`
}

// ProfileSecUID fetches a public profile page and returns that account's secUid
// (the opaque id [Client.UserPosts] and [Client.Following] take). username is the
// @handle without the leading "@". This needs no signing and works anonymously.
func (c *Client) ProfileSecUID(ctx context.Context, username string) (string, error) {
	username = strings.TrimPrefix(strings.TrimSpace(username), "@")
	if username == "" {
		return "", fmt.Errorf("tiktok: ProfileSecUID: empty username")
	}
	data, err := c.fetchRehydration(ctx, "/@"+username)
	if err != nil {
		return "", err
	}
	if sec := data.DefaultScope.UserDetail.UserInfo.User.SecUID; sec != "" {
		return sec, nil
	}
	return "", fmt.Errorf("tiktok: no secUid found for @%s (profile private, missing, or page changed)", username)
}

// ViewerSecUID returns the authenticated viewer's own secUid. It fetches a
// TikTok web app page carrying the session cookie and reads the viewer out of
// the embedded rehydration JSON's app-context. A session is required (see
// [WithSessionID]); without one, TikTok serves a logged-out page that carries no
// viewer identity and this returns an error.
func (c *Client) ViewerSecUID(ctx context.Context) (string, error) {
	if c.SessionID == "" {
		return "", fmt.Errorf("tiktok: ViewerSecUID needs a session (set WithSessionID)")
	}
	data, err := c.fetchRehydration(ctx, "/foryou")
	if err != nil {
		return "", err
	}
	if sec := data.DefaultScope.AppContext.User.SecUID; sec != "" {
		return sec, nil
	}
	return "", fmt.Errorf("tiktok: viewer secUid not present (session expired, or TikTok changed its page shape)")
}

// fetchRehydration GETs a web page and unmarshals its embedded rehydration JSON.
func (c *Client) fetchRehydration(ctx context.Context, path string) (*rehydration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
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
		return nil, fmt.Errorf("tiktok: page fetch failed: status %d: %s", resp.StatusCode, snippet(body))
	}

	m := rehydrationRe.FindSubmatch(body)
	if m == nil {
		return nil, fmt.Errorf("tiktok: rehydration JSON not found (page changed or anti-bot block)")
	}
	var data rehydration
	if err := json.Unmarshal(m[1], &data); err != nil {
		return nil, fmt.Errorf("tiktok: decode rehydration JSON: %w", err)
	}
	return &data, nil
}
