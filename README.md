<p align="center"><img src="https://raw.githubusercontent.com/go-tiktok/brand/main/social/go-tiktok.png" alt="go-tiktok/tiktok" width="720"></p>

# go-tiktok/tiktok

[![CI](https://github.com/go-tiktok/tiktok/actions/workflows/ci.yml/badge.svg)](https://github.com/go-tiktok/tiktok/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/go-tiktok/tiktok.svg)](https://pkg.go.dev/github.com/go-tiktok/tiktok)
[![License: BSD-3-Clause](https://img.shields.io/badge/License-BSD--3--Clause-blue.svg)](LICENSE)

A pure-Go, dependency-free, **best-effort** read client for public TikTok
content, talking to TikTok's undocumented web JSON endpoints.

- **CGO_ENABLED=0**, standard library only, **zero third-party dependencies**.
- Stable, small Go API with an overridable base URL for network-free testing.
- 100% test coverage against a mock server.

## ⚠️ Fragility & Terms-of-Service caveat

**Read this before depending on the library.**

TikTok does **not** publish or support a public web API. This client calls the
same internal endpoints TikTok's own website uses, and TikTok actively defends
them. In practice you should expect:

- Requests often need a valid **`msToken`** (query parameter and cookie) and a
  signed parameter (`X-Bogus` / `_signature`) that this library does **not**
  compute. Many reads additionally require a logged-in **`sessionid`** cookie.
- TikTok returns anti-bot responses — HTTP `403`/`429`, or an HTTP `200` with an
  empty or `{}` body — when it decides a request looks automated.
- The endpoint shape, parameters, and response schema **can change without
  notice**, breaking this library at any time.

This project is therefore **best-effort**: the code builds correct requests and
parses correct responses, but working end-to-end against live TikTok is not
guaranteed and is not something the maintainers can promise to keep working.

You are responsible for complying with
[TikTok's Terms of Service](https://www.tiktok.com/legal/terms-of-service) and
all applicable law and rate limits. Do not use this for anything abusive,
high-volume, or that TikTok's terms prohibit.

## Install

```sh
go get github.com/go-tiktok/tiktok
```

## Usage

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/go-tiktok/tiktok"
)

func main() {
	c := tiktok.New(
		tiktok.WithMSToken("...msToken from a browser session..."),
		tiktok.WithSessionID("...sessionid cookie for authed reads..."),
	)

	// secUid is TikTok's opaque per-user id; obtain it once from a profile
	// page's embedded JSON, then pass it here.
	feed, err := c.UserPosts(context.Background(), "MS4wLjABAAAA...", 20, "0")
	if err != nil {
		log.Fatal(err)
	}

	for _, v := range feed.Videos {
		fmt.Printf("%s  %d likes  %s\n", v.Permalink, v.Likes, v.Description)
	}
	fmt.Printf("cursor=%s hasMore=%v\n", feed.Cursor, feed.HasMore)
}
```

An empty page (no videos, `HasMore == false`) is returned **without** error. A
non-2xx status, an empty/anti-bot body, or malformed JSON returns a descriptive
error including the HTTP status where relevant.

## API

| Symbol | Purpose |
| --- | --- |
| `New(...Option) *Client` | Construct a client. |
| `WithHTTPClient`, `WithBaseURL`, `WithUserAgent`, `WithMSToken`, `WithSessionID` | Options. |
| `(*Client).UserPosts(ctx, secUid, count, cursor) (*UserFeed, error)` | Fetch a user's recent videos via the web `item_list` API. |
| `Video`, `UserFeed` | Result types. |

## License

BSD-3-Clause. Copyright the go-tiktok/tiktok authors. See [LICENSE](LICENSE).
