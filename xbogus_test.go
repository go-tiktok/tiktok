package tiktok

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

// fixedUA is the User-Agent the reference vectors below were generated with.
const fixedUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) " +
	"AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"

// TestXBogusVector pins the pure-Go signer to the output of the public reference
// implementation (armxe/tiktok-api's Web/bogus.py) for two fixed inputs at a
// fixed timestamp. If these ever diverge, the port has drifted from the
// canonical algorithm.
func TestXBogusVector(t *testing.T) {
	const ts = 1700000000
	cases := []struct{ query, want string }{
		{"aid=1988&count=30&secUid=MS4wLjABAAAA", "DFSzswVOhXvANn-StmWx-e9WX7r4"},
		{"", "DFSzswVO0IJANn-StmWx-e9WX7rA"},
	}
	for _, tc := range cases {
		if got := XBogus(tc.query, fixedUA, ts); got != tc.want {
			t.Errorf("XBogus(%q) = %q, want %q", tc.query, got, tc.want)
		}
		if len(XBogus(tc.query, fixedUA, ts)) != 28 {
			t.Errorf("XBogus(%q) length != 28", tc.query)
		}
	}
}

// TestCustomBase64PadPath covers the short-trailing-group branch of the encoder
// (a group of fewer than three bytes), which the final X-Bogus payload never
// reaches but the intermediate User-Agent encoding can.
func TestCustomBase64PadPath(t *testing.T) {
	if got := customBase64([]byte{1, 2}, xbStdAlphabet); got != "AQ==" {
		t.Errorf("customBase64 pad path = %q, want AQ==", got)
	}
}

func TestSignedQueryFixedNow(t *testing.T) {
	c := New(WithMSToken("tok"))
	c.now = func() time.Time { return time.Unix(1700000000, 0) }
	q := url.Values{}
	q.Set("aid", "1988")
	raw := c.signedQuery(q)
	vals, err := url.ParseQuery(raw)
	if err != nil {
		t.Fatalf("ParseQuery: %v", err)
	}
	if vals.Get("X-Bogus") == "" {
		t.Error("X-Bogus missing")
	}
	if vals.Get("msToken") != "tok" {
		t.Error("msToken not folded into the signed query")
	}
	// Signature is computed over the encoded string up to "&X-Bogus=".
	enc := strings.TrimSuffix(raw, "&X-Bogus="+vals.Get("X-Bogus"))
	if want := XBogus(enc, DefaultUserAgent, 1700000000); vals.Get("X-Bogus") != want {
		t.Errorf("X-Bogus = %q, want %q", vals.Get("X-Bogus"), want)
	}
}

// TestSignedQueryNilNow exercises the fallback in signedQuery when a Client was
// built literally (no now seam) rather than via New.
func TestSignedQueryNilNow(t *testing.T) {
	c := &Client{UserAgent: "ua"}
	raw := c.signedQuery(url.Values{})
	if !strings.Contains(raw, "X-Bogus=") {
		t.Errorf("expected X-Bogus in %q", raw)
	}
}
