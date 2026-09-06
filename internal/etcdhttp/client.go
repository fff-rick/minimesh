package etcdhttp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

type Client struct {
	endpoint string
	http     *http.Client
}

func New(endpoint string) *Client {
	return &Client{endpoint: strings.TrimRight(endpoint, "/"), http: &http.Client{}}
}

type KV struct {
	Key, Value  []byte
	ModRevision int64
}
type WatchEvent struct {
	Type     string
	KV       KV
	PrevKV   KV
	Revision int64
}

func (c *Client) Grant(ctx context.Context, ttl int64) (int64, error) {
	var out struct {
		ID  json.RawMessage `json:"ID"`
		TTL json.RawMessage `json:"TTL"`
	}
	if err := c.post(ctx, "/v3/lease/grant", map[string]any{"TTL": ttl}, &out); err != nil {
		return 0, err
	}
	return parseInt(out.ID)
}
func (c *Client) KeepAliveOnce(ctx context.Context, id int64) error {
	req, err := c.newRequest(ctx, "/v3/lease/keepalive", map[string]any{"ID": strconv.FormatInt(id, 10)})
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("etcd keepalive: %s: %s", resp.Status, string(b))
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return fmt.Errorf("etcd keepalive decode: %w", err)
	}
	return nil
}
func (c *Client) Revoke(ctx context.Context, id int64) error {
	var out any
	return c.post(ctx, "/v3/lease/revoke", map[string]any{"ID": strconv.FormatInt(id, 10)}, &out)
}
func (c *Client) Put(ctx context.Context, key string, value []byte, lease int64) error {
	body := map[string]any{"key": b64([]byte(key)), "value": b64(value)}
	if lease != 0 {
		body["lease"] = strconv.FormatInt(lease, 10)
	}
	var out any
	return c.post(ctx, "/v3/kv/put", body, &out)
}
func (c *Client) Delete(ctx context.Context, key string) error {
	var out any
	return c.post(ctx, "/v3/kv/deleterange", map[string]any{"key": b64([]byte(key))}, &out)
}
func (c *Client) RangePrefix(ctx context.Context, prefix string) ([]KV, int64, error) {
	var out struct {
		Header struct {
			Revision json.RawMessage `json:"revision"`
		} `json:"header"`
		KVs []struct {
			Key, Value  string
			ModRevision json.RawMessage `json:"mod_revision"`
		} `json:"kvs"`
	}
	if err := c.post(ctx, "/v3/kv/range", map[string]any{"key": b64([]byte(prefix)), "range_end": b64(prefixEnd([]byte(prefix)))}, &out); err != nil {
		return nil, 0, err
	}
	rev, _ := parseInt(out.Header.Revision)
	kvs := make([]KV, 0, len(out.KVs))
	for _, item := range out.KVs {
		k, err := base64.StdEncoding.DecodeString(item.Key)
		if err != nil {
			return nil, 0, err
		}
		v, err := base64.StdEncoding.DecodeString(item.Value)
		if err != nil {
			return nil, 0, err
		}
		mr, _ := parseInt(item.ModRevision)
		kvs = append(kvs, KV{Key: k, Value: v, ModRevision: mr})
	}
	return kvs, rev, nil
}
func (c *Client) WatchPrefix(ctx context.Context, prefix string, startRev int64, handle func(WatchEvent) error) error {
	body := map[string]any{"create_request": map[string]any{"key": b64([]byte(prefix)), "range_end": b64(prefixEnd([]byte(prefix))), "start_revision": strconv.FormatInt(startRev, 10), "progress_notify": true, "prev_kv": true}}
	req, err := c.newRequest(ctx, "/v3/watch", body)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("etcd watch: %s: %s", resp.Status, string(b))
	}
	dec := json.NewDecoder(bufio.NewReader(resp.Body))
	for {
		var msg struct {
			Result struct {
				Header struct {
					Revision json.RawMessage `json:"revision"`
				} `json:"header"`
				Events []struct {
					Type string `json:"type"`
					KV   struct {
						Key, Value  string
						ModRevision json.RawMessage `json:"mod_revision"`
					} `json:"kv"`
					PrevKV struct {
						Key, Value  string
						ModRevision json.RawMessage `json:"mod_revision"`
					} `json:"prev_kv"`
				} `json:"events"`
			} `json:"result"`
		}
		if err := dec.Decode(&msg); err != nil {
			return err
		}
		hr, _ := parseInt(msg.Result.Header.Revision)
		for _, e := range msg.Result.Events {
			ev := WatchEvent{Type: e.Type, Revision: hr}
			ev.KV = decodeKV(e.KV.Key, e.KV.Value, e.KV.ModRevision)
			ev.PrevKV = decodeKV(e.PrevKV.Key, e.PrevKV.Value, e.PrevKV.ModRevision)
			if ev.KV.ModRevision > 0 {
				ev.Revision = ev.KV.ModRevision
			}
			if err := handle(ev); err != nil {
				return err
			}
		}
	}
}
func decodeKV(k, v string, mr json.RawMessage) KV {
	kb, _ := base64.StdEncoding.DecodeString(k)
	vb, _ := base64.StdEncoding.DecodeString(v)
	r, _ := parseInt(mr)
	return KV{Key: kb, Value: vb, ModRevision: r}
}
func (c *Client) post(ctx context.Context, path string, body any, out any) error {
	req, err := c.newRequest(ctx, path, body)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("etcd %s: %s: %s", path, resp.Status, string(b))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
func (c *Client) newRequest(ctx context.Context, path string, body any) (*http.Request, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+path, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}
func b64(v []byte) string { return base64.StdEncoding.EncodeToString(v) }
func prefixEnd(p []byte) []byte {
	out := append([]byte(nil), p...)
	for i := len(out) - 1; i >= 0; i-- {
		if out[i] < 0xff {
			out[i]++
			return out[:i+1]
		}
	}
	return []byte{0}
}
func parseInt(raw json.RawMessage) (int64, error) {
	if len(raw) == 0 {
		return 0, nil
	}
	var s string
	if raw[0] == '"' {
		if err := json.Unmarshal(raw, &s); err != nil {
			return 0, err
		}
	} else {
		s = string(raw)
	}
	return strconv.ParseInt(s, 10, 64)
}
