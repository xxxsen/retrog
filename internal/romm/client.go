package romm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"path"
	"strings"
	"time"
)

const (
	defaultLimit = 72
)

// Platform represents a RomM platform entry.
type Platform struct {
	ID       int `json:"id"`
	RomCount int `json:"rom_count"`
}

// RomFile describes a single file attached to a ROM.
type RomFile struct {
	MD5Hash string `json:"md5_hash"`
}

// Rom represents the payload returned by GET /api/roms.
type Rom struct {
	ID             int       `json:"id"`
	Name           string    `json:"name"`
	FsName         string    `json:"fs_name"`
	Summary        string    `json:"summary"`
	MD5Hash        string    `json:"md5_hash"`
	Files          []RomFile `json:"files"`
	PathCoverSmall string    `json:"path_cover_small"`
	PathCoverLarge string    `json:"path_cover_large"`
}

// ListRomsResponse captures the paginated response from GET /api/roms.
type ListRomsResponse struct {
	Items  []Rom `json:"items"`
	Limit  int   `json:"limit"`
	Offset int   `json:"offset"`
	Total  int   `json:"total"`
}

// UpdateRomArtwork carries artwork data for metadata update.
type UpdateRomArtwork struct {
	Filename    string
	ContentType string
	Data        []byte
}

// UpdateRomRequest represents the fields we update on a ROM.
type UpdateRomRequest struct {
	Name    string
	FsName  string
	Summary string
	Artwork *UpdateRomArtwork
}

// Client handles RomM API calls.
type Client struct {
	host       string
	httpClient *http.Client

	sessionCookie string
	csrfToken     string
}

var defaultClient *Client

// SetDefaultClient assigns the global RomM client.
func SetDefaultClient(c *Client) {
	defaultClient = c
}

// DefaultClient returns the configured global RomM client.
func DefaultClient() *Client {
	return defaultClient
}

// New creates a new RomM API client.
func New(host, session, csrf string) (*Client, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return nil, fmt.Errorf("romm host must be provided")
	}
	if !strings.HasPrefix(host, "http://") && !strings.HasPrefix(host, "https://") {
		host = "https://" + host
	}
	u, err := url.Parse(host)
	if err != nil {
		return nil, fmt.Errorf("invalid romm host: %w", err)
	}
	u.Path = strings.TrimSuffix(u.Path, "/")

	session = strings.TrimSpace(session)
	csrf = strings.TrimSpace(csrf)
	if session == "" {
		return nil, fmt.Errorf("romm_session must be provided")
	}
	if csrf == "" {
		return nil, fmt.Errorf("csrftoken must be provided")
	}

	return &Client{
		host:          strings.TrimSuffix(u.String(), "/"),
		sessionCookie: fmt.Sprintf("romm_session=%s; romm_csrftoken=%s", session, csrf),
		csrfToken:     csrf,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}, nil
}

func (c *Client) baseURL(p string) string {
	return c.host + path.Clean("/"+p)
}

func (c *Client) applyCommonHeaders(req *http.Request) {
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("User-Agent", "retrog/patch-romm-meta")
	req.Header.Set("Cookie", c.sessionCookie)
}

// GetPlatforms retrieves every platform from RomM.
func (c *Client) GetPlatforms(ctx context.Context) ([]Platform, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL("/api/platforms"), nil)
	if err != nil {
		return nil, err
	}
	c.applyCommonHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("get platforms: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var platforms []Platform
	if err := json.NewDecoder(resp.Body).Decode(&platforms); err != nil {
		return nil, fmt.Errorf("decode platforms: %w", err)
	}
	return platforms, nil
}

// ListRoms lists ROMs for a specific platform with paging.
func (c *Client) ListRoms(ctx context.Context, platformID int, limit, offset int) (*ListRomsResponse, error) {
	if limit <= 0 || limit > defaultLimit {
		limit = defaultLimit
	}

	query := url.Values{}
	query.Set("platform_id", fmt.Sprintf("%d", platformID))
	query.Set("limit", fmt.Sprintf("%d", limit))
	query.Set("offset", fmt.Sprintf("%d", offset))
	query.Set("order_by", "name")
	query.Set("order_dir", "asc")
	query.Set("group_by_meta_id", "true")

	endpoint := c.baseURL("/api/roms") + "?" + query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	c.applyCommonHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("list roms: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var result ListRomsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode roms: %w", err)
	}
	return &result, nil
}

// UpdateRom updates RomM metadata and optionally artwork for a ROM.
func (c *Client) UpdateRom(ctx context.Context, romID int, payload UpdateRomRequest) error {
	qs := url.Values{}
	qs.Set("remove_cover", "false")
	qs.Set("unmatch_metadata", "false")

	var buf bytes.Buffer
	writer := multipartWriter{w: &buf}
	writer.WriteField("name", payload.Name)
	writer.WriteField("fs_name", payload.FsName)
	writer.WriteField("summary", payload.Summary)

	if payload.Artwork != nil && len(payload.Artwork.Data) > 0 {
		ct := payload.Artwork.ContentType
		if ct == "" {
			ct = mime.TypeByExtension(path.Ext(payload.Artwork.Filename))
		}
		if err := writer.WriteFile("artwork", payload.Artwork.Filename, ct, payload.Artwork.Data); err != nil {
			return err
		}
	}

	if err := writer.Close(); err != nil {
		return err
	}

	endpoint := fmt.Sprintf("%s/api/roms/%d?%s", c.host, romID, qs.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, &buf)
	if err != nil {
		return err
	}
	c.applyCommonHeaders(req)
	req.Header.Set("X-CSRFToken", c.csrfToken)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("update rom %d: status %d: %s", romID, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var result struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("decode update response: %w", err)
	}
	if result.ID != romID {
		return fmt.Errorf("update rom %d: unexpected response id %d", romID, result.ID)
	}
	return nil
}

// multipartWriter is a thin wrapper that defers multipart creation until needed.
type multipartWriter struct {
	w      *bytes.Buffer
	writer *multipart.Writer
	err    error
	closed bool
}

func (mw *multipartWriter) init() {
	if mw.writer == nil && mw.err == nil {
		mw.writer = multipart.NewWriter(mw.w)
	}
}

func (mw *multipartWriter) WriteField(field, value string) {
	if mw.err != nil {
		return
	}
	mw.init()
	if mw.err != nil {
		return
	}
	mw.err = mw.writer.WriteField(field, value)
}

func (mw *multipartWriter) WriteFile(field, filename, contentType string, data []byte) error {
	if mw.err != nil {
		return mw.err
	}
	mw.init()
	if mw.err != nil {
		return mw.err
	}
	partHeaders := textproto.MIMEHeader{}
	partHeaders.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, field, filename))
	if contentType != "" {
		partHeaders.Set("Content-Type", contentType)
	}
	part, err := mw.writer.CreatePart(partHeaders)
	if err != nil {
		mw.err = err
		return err
	}
	if _, err := part.Write(data); err != nil {
		mw.err = err
		return err
	}
	return nil
}

func (mw *multipartWriter) Close() error {
	if mw.err != nil {
		return mw.err
	}
	if mw.writer == nil || mw.closed {
		return nil
	}
	mw.closed = true
	if err := mw.writer.Close(); err != nil {
		mw.err = err
	}
	return mw.err
}

func (mw *multipartWriter) FormDataContentType() string {
	if mw.writer == nil {
		mw.init()
	}
	if mw.writer == nil {
		return ""
	}
	return mw.writer.FormDataContentType()
}
