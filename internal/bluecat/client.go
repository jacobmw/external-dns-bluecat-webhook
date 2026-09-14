package bluecat

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	recordTypeHost  = "HostRecord"
	recordTypeAlias = "AliasRecord"
	recordTypeTXT   = "TXTRecord"
	recordTypeExt   = "ExternalHostRecord"
	deployQuick     = "QuickDeployment"
)

// Client talks to BlueCat Address Manager REST API v2.
type Client interface {
	ListZones(ctx context.Context, rootZone string) ([]Zone, error)
	ListHostRecords(ctx context.Context, zone Zone) ([]HostRecord, error)
	ListAliasRecords(ctx context.Context, zone Zone) ([]AliasRecord, error)
	ListTXTRecords(ctx context.Context, zone Zone) ([]TXTRecord, error)
	GetRecord(ctx context.Context, absoluteName, recordType string) (*GenericRecord, error)
	CreateOrUpdateHost(ctx context.Context, zone Zone, rec HostRecord) error
	CreateOrUpdateAlias(ctx context.Context, zone Zone, rec AliasRecord) error
	CreateOrUpdateTXT(ctx context.Context, zone Zone, rec TXTRecord) error
	DeleteRecord(ctx context.Context, id int64) error
	EnableDynamicUpdates(ctx context.Context, zones []Zone) error
	DeployZone(ctx context.Context, zone Zone) error
}

type httpClient struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// Config is the Address Manager connection settings.
type Config struct {
	Host          string
	Username      string
	Password      string
	CAFile        string
	SkipTLSVerify bool
	Timeout       time.Duration
}

// Login creates an authenticated v2 client.
func Login(ctx context.Context, cfg Config) (Client, error) {
	if cfg.Host == "" {
		return nil, fmt.Errorf("bluecat host is required")
	}
	if cfg.Username == "" || cfg.Password == "" {
		return nil, fmt.Errorf("bluecat username and password are required")
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	tlsCfg, err := tlsConfig(cfg.SkipTLSVerify, cfg.CAFile)
	if err != nil {
		return nil, err
	}
	c := &httpClient{
		baseURL: strings.TrimRight(cfg.Host, "/"),
		httpClient: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				Proxy:             http.ProxyFromEnvironment,
				TLSClientConfig:   tlsCfg,
				ForceAttemptHTTP2: true,
			},
		},
	}

	session := Session{Username: ptr(cfg.Username), Password: ptr(cfg.Password)}
	body, err := json.Marshal(session)
	if err != nil {
		return nil, fmt.Errorf("marshal session: %w", err)
	}
	resp, err := c.do(ctx, http.MethodPost, "/api/v2/sessions", "", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read session response: %w", err)
	}
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("create session: http %d: %s", resp.StatusCode, raw)
	}
	if err := json.Unmarshal(raw, &session); err != nil {
		return nil, fmt.Errorf("decode session: %w", err)
	}
	if session.APIToken == nil || *session.APIToken == "" {
		return nil, fmt.Errorf("session response missing apiToken")
	}
	c.token = base64.StdEncoding.EncodeToString([]byte(cfg.Username + ":" + *session.APIToken))
	return c, nil
}

func (c *httpClient) ListZones(ctx context.Context, rootZone string) ([]Zone, error) {
	q := url.Values{}
	q.Set("total", "true")
	if rootZone != "" {
		q.Set("filter", fmt.Sprintf("absoluteName:contains('%s')", rootZone))
	}
	var out collection[Zone]
	if err := c.getJSON(ctx, "/api/v2/zones?"+q.Encode(), &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

func (c *httpClient) ListHostRecords(ctx context.Context, zone Zone) ([]HostRecord, error) {
	q := url.Values{}
	q.Set("total", "true")
	q.Set("filter", "type:eq('HostRecord')")
	q.Set("fields", "embed(addresses)")
	var out collection[HostRecord]
	if err := c.getJSON(ctx, "/api/v2/zones/"+zone.IDString()+"/resourceRecords?"+q.Encode(), &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

func (c *httpClient) ListAliasRecords(ctx context.Context, zone Zone) ([]AliasRecord, error) {
	q := url.Values{}
	q.Set("total", "true")
	q.Set("filter", "type:eq('AliasRecord')")
	var out collection[AliasRecord]
	if err := c.getJSON(ctx, "/api/v2/zones/"+zone.IDString()+"/resourceRecords?"+q.Encode(), &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

func (c *httpClient) ListTXTRecords(ctx context.Context, zone Zone) ([]TXTRecord, error) {
	q := url.Values{}
	q.Set("total", "true")
	q.Set("filter", "type:eq('TXTRecord')")
	var out collection[TXTRecord]
	if err := c.getJSON(ctx, "/api/v2/zones/"+zone.IDString()+"/resourceRecords?"+q.Encode(), &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

func (c *httpClient) GetRecord(ctx context.Context, absoluteName, recordType string) (*GenericRecord, error) {
	q := url.Values{}
	q.Set("total", "true")
	filter := fmt.Sprintf("absoluteName:eq('%s')", absoluteName)
	if recordType != "" {
		filter += fmt.Sprintf(" and type:eq('%s')", recordType)
	}
	q.Set("filter", filter)
	var out collection[GenericRecord]
	if err := c.getJSON(ctx, "/api/v2/resourceRecords?"+q.Encode(), &out); err != nil {
		return nil, err
	}
	if len(out.Data) == 0 {
		return nil, nil
	}
	return &out.Data[0], nil
}

func (c *httpClient) CreateOrUpdateHost(ctx context.Context, zone Zone, rec HostRecord) error {
	rec.Type = recordTypeHost
	return c.upsert(ctx, zone, rec.ID, rec.AbsoluteName, rec)
}

func (c *httpClient) CreateOrUpdateAlias(ctx context.Context, zone Zone, rec AliasRecord) error {
	rec.Type = recordTypeAlias
	if rec.LinkedRecord != nil && rec.LinkedRecord.AbsoluteName != nil {
		target, err := c.GetRecord(ctx, *rec.LinkedRecord.AbsoluteName, "")
		if err != nil {
			return fmt.Errorf("resolve cname target %s: %w", *rec.LinkedRecord.AbsoluteName, err)
		}
		if target != nil {
			rec.LinkedRecord.ID = target.ID
			rec.LinkedRecord.Type = target.Type
			rec.LinkedRecord.AbsoluteName = target.AbsoluteName
		} else if rec.LinkedRecord.Type == nil {
			rec.LinkedRecord.Type = ptr(recordTypeExt)
		}
	}
	return c.upsert(ctx, zone, rec.ID, rec.AbsoluteName, rec)
}

func (c *httpClient) CreateOrUpdateTXT(ctx context.Context, zone Zone, rec TXTRecord) error {
	rec.Type = recordTypeTXT
	return c.upsert(ctx, zone, rec.ID, rec.AbsoluteName, rec)
}

func (c *httpClient) DeleteRecord(ctx context.Context, id int64) error {
	resp, err := c.do(ctx, http.MethodDelete, "/api/v2/resourceRecords/"+strconvFormat(id), c.token, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delete record %d: http %d: %s", id, resp.StatusCode, raw)
	}
	return nil
}

func (c *httpClient) EnableDynamicUpdates(ctx context.Context, zones []Zone) error {
	enabled := true
	for _, zone := range zones {
		if zone.ID == nil {
			continue
		}
		if zone.DynamicUpdateEnabled != nil && *zone.DynamicUpdateEnabled {
			continue
		}
		body := map[string]any{
			"id":                   *zone.ID,
			"dynamicUpdateEnabled": true,
		}
		if err := c.putJSON(ctx, "/api/v2/zones/"+zone.IDString(), body); err != nil {
			return fmt.Errorf("enable dynamic updates on %s: %w", zone.AbsoluteNameOrEmpty(), err)
		}
		zone.DynamicUpdateEnabled = &enabled
	}
	return nil
}

func (c *httpClient) DeployZone(ctx context.Context, zone Zone) error {
	body, err := json.Marshal(quickDeployment{Type: deployQuick})
	if err != nil {
		return err
	}
	resp, err := c.do(ctx, http.MethodPost, "/api/v2/zones/"+zone.IDString()+"/deployments", c.token, bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("deploy zone %s: http %d: %s", zone.AbsoluteNameOrEmpty(), resp.StatusCode, raw)
	}
	return nil
}

func (c *httpClient) upsert(ctx context.Context, zone Zone, id *int64, absoluteName *string, payload any) error {
	relName := relativeName(derefString(absoluteName), zone.AbsoluteNameOrEmpty())
	switch rec := payload.(type) {
	case HostRecord:
		rec.Name = ptr(relName)
		rec.AbsoluteName = nil
		payload = rec
	case AliasRecord:
		rec.Name = ptr(relName)
		rec.AbsoluteName = nil
		payload = rec
	case TXTRecord:
		rec.Name = ptr(relName)
		rec.AbsoluteName = nil
		payload = rec
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	method := http.MethodPost
	path := "/api/v2/zones/" + zone.IDString() + "/resourceRecords"
	if id != nil {
		method = http.MethodPut
		path = "/api/v2/resourceRecords/" + strconvFormat(*id)
	}
	resp, err := c.do(ctx, method, path, c.token, bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s %s: http %d: %s", method, path, resp.StatusCode, raw)
	}
	return nil
}

func (c *httpClient) getJSON(ctx context.Context, path string, dest any) error {
	resp, err := c.do(ctx, http.MethodGet, path, c.token, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: http %d: %s", path, resp.StatusCode, raw)
	}
	if err := json.Unmarshal(raw, dest); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

func (c *httpClient) putJSON(ctx context.Context, path string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	resp, err := c.do(ctx, http.MethodPut, path, c.token, bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("PUT %s: http %d: %s", path, resp.StatusCode, raw)
	}
	return nil
}

func (c *httpClient) do(ctx context.Context, method, path, token string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/hal+json")
	if method == http.MethodPost || method == http.MethodPut {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Basic "+token)
	}
	return c.httpClient.Do(req)
}

func tlsConfig(skipVerify bool, caFile string) (*tls.Config, error) {
	if skipVerify && caFile != "" {
		return nil, fmt.Errorf("cannot set both skip TLS verify and a CA file")
	}
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if skipVerify {
		cfg.InsecureSkipVerify = true //nolint:gosec // operator-controlled for lab BAM certs
		return cfg, nil
	}
	if caFile == "" {
		return cfg, nil
	}
	pem, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("read CA file %s: %w", caFile, err)
	}
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("no certificates found in CA file %s", caFile)
	}
	cfg.RootCAs = pool
	return cfg, nil
}

func relativeName(absolute, zone string) string {
	absolute = strings.TrimSuffix(absolute, ".")
	zone = strings.TrimSuffix(zone, ".")
	if strings.EqualFold(absolute, zone) {
		return ""
	}
	return strings.TrimSuffix(absolute, "."+zone)
}

func strconvFormat(id int64) string {
	return fmt.Sprintf("%d", id)
}
