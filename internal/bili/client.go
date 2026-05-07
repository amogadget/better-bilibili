package bili

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"bili-web-vps/internal/config"
)

const (
	UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	Referer   = "https://www.bilibili.com"
)

type Client struct {
	cfg *config.Config
	// HTTP is the short-timeout client used for JSON API calls (search, info, etc.).
	HTTP *http.Client
	// HTTPStream has no overall timeout because video range responses can run for
	// minutes; cancellation is driven by the request context instead.
	HTTPStream *http.Client

	wbiMu sync.RWMutex
	wbi   wbiKeys
}

func New(cfg *config.Config) *Client {
	return &Client{
		cfg:        cfg,
		HTTP:       &http.Client{Timeout: 15 * time.Second},
		HTTPStream: &http.Client{},
	}
}

// NewRequest builds an outbound HTTP request with bilibili-friendly headers
// (UA, Referer, and the configured cookies if present).
func (c *Client) NewRequest(ctx context.Context, method, url string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	c.ApplyHeaders(req)
	return req, nil
}

func (c *Client) ApplyHeaders(req *http.Request) {
	req.Header.Set("User-Agent", UserAgent)
	if req.Header.Get("Referer") == "" {
		req.Header.Set("Referer", Referer)
	}
	ck := c.cfg.GetCookies()
	if ck.SESSDATA != "" {
		req.Header.Set("Cookie", fmt.Sprintf("SESSDATA=%s; bili_jct=%s; DedeUserID=%s",
			ck.SESSDATA, ck.BiliJct, ck.DedeUserID))
	}
}

// GetJSON fetches a JSON endpoint and decodes into dst.
func (c *Client) GetJSON(ctx context.Context, url string, dst any) error {
	req, err := c.NewRequest(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("bilibili %s: status %d body=%q", url, resp.StatusCode, b)
	}
	if dst != nil {
		return json.NewDecoder(resp.Body).Decode(dst)
	}
	return nil
}
