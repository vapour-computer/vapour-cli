// Package api is a small client for the Vapour HTTP API (contract: v1).
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"path/filepath"
	"strings"
)

type Client struct {
	Base      string
	Token     string
	UserAgent string
	HTTP      *http.Client
}

func New(base, token, userAgent string) *Client {
	return &Client{
		Base:      strings.TrimRight(base, "/"),
		Token:     token,
		UserAgent: userAgent,
		// No overall timeout: uploads can be large. Callers use contexts.
		HTTP: &http.Client{Timeout: 0},
	}
}

type Error struct {
	Status  int
	Code    string
	Message string
}

func (e *Error) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("%s (%s, HTTP %d)", e.Message, e.Code, e.Status)
	}
	return fmt.Sprintf("%s (HTTP %d)", e.Message, e.Status)
}

type User struct {
	UID    string `json:"uid"`
	Name   string `json:"name"`
	Email  string `json:"email"`
	Role   string `json:"role"`
	Avatar string `json:"avatar"`
}

type Dev struct {
	UID         string `json:"uid"`
	DisplayName string `json:"display_name"`
}

type Game struct {
	ID       string `json:"id"`
	DevUID   string `json:"dev_uid"`
	Title    string `json:"title"`
	Slug     string `json:"slug"`
	Engine   string `json:"engine"`
	Version  string `json:"version"`
	BuildURL string `json:"build_url"`
	CoverURL string `json:"cover_url"`
	Status   string `json:"status"`
}

type MeResponse struct {
	User User   `json:"user"`
	Dev  *Dev   `json:"dev"`
	Auth string `json:"auth"`
}

type CreateProjectRequest struct {
	Title  string `json:"title"`
	Slug   string `json:"slug,omitempty"`
	Engine string `json:"engine"`
}

type PublishResponse struct {
	Game Game   `json:"game"`
	URL  string `json:"url"`
}

func (c *Client) do(ctx context.Context, method, path string, body io.Reader, contentType string, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, c.Base+path, body)
	if err != nil {
		return err
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Accept", "application/json")
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("cannot reach %s: %w", c.Base, err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if res.StatusCode >= 400 {
		var e struct {
			Error struct{ Code, Message string } `json:"error"`
		}
		if json.Unmarshal(data, &e) == nil && e.Error.Message != "" {
			return &Error{Status: res.StatusCode, Code: e.Error.Code, Message: e.Error.Message}
		}
		return &Error{Status: res.StatusCode, Message: strings.TrimSpace(string(data))}
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("unexpected response from server: %w", err)
		}
	}
	return nil
}

func (c *Client) Me(ctx context.Context) (*MeResponse, error) {
	var r MeResponse
	if err := c.do(ctx, http.MethodGet, "/me", nil, "", &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func (c *Client) CreateProject(ctx context.Context, req CreateProjectRequest) (*Game, error) {
	b, _ := json.Marshal(req)
	var g Game
	if err := c.do(ctx, http.MethodPost, "/projects", bytes.NewReader(b), "application/json", &g); err != nil {
		return nil, err
	}
	return &g, nil
}

// File is one multipart upload part.
type File struct {
	Path string
	Name string
	Body io.Reader
}

// Publish uploads a build (zip) and an optional cover image, streaming the request body.
func (c *Client) Publish(ctx context.Context, projectID, version string, build File, cover *File) (*PublishResponse, error) {
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		err := func() error {
			if err := mw.WriteField("version", version); err != nil {
				return err
			}
			if err := part(mw, "build", build, "application/zip"); err != nil {
				return err
			}
			if cover != nil {
				if err := part(mw, "cover", *cover, coverType(cover.Name)); err != nil {
					return err
				}
			}
			return mw.Close()
		}()
		pw.CloseWithError(err)
	}()
	var r PublishResponse
	err := c.do(ctx, http.MethodPost, "/projects/"+projectID+"/publish", pr, mw.FormDataContentType(), &r)
	pr.CloseWithError(err)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func part(mw *multipart.Writer, field string, f File, ctype string) error {
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename=%q`, field, filepath.Base(f.Name)))
	h.Set("Content-Type", ctype)
	w, err := mw.CreatePart(h)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, f.Body)
	return err
}

func coverType(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	default:
		return "image/png"
	}
}
