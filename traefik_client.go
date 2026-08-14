package traefik

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// Traefik's documented, fixed page size for paginated API endpoints.
const traefikApiPageSize = 100

type ITraefikClient interface {
	GetHttpRouters() (*[]HttpRouter, error)
}

type TraefikClient struct {
	ITraefikClient
	httpRoutersUrl string
	config         *TraefikConfig
	client         *http.Client
}

func NewTraefikClient(cfg *TraefikConfig) (*TraefikClient, error) {
	httpRoutersUrl, err := url.JoinPath(cfg.baseUrl.String(), "/http/routers")
	if err != nil {
		return nil, err
	}

	client := &TraefikClient{
		httpRoutersUrl: httpRoutersUrl,
		config:         cfg,
	}

	if cfg.insecureSkipVerify {
		tr := &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
		client.client = &http.Client{Transport: tr}
	} else {
		client.client = &http.Client{}
	}

	return client, nil
}

// Traefik's /api/http/routers endpoint paginates (100 items/page by default)
// once enough routers are registered. A single unpaginated GET silently
// truncates the result, so routers past page 1 never make it into this
// plugin's zone map. Page through until a short page (or an out-of-range
// page request) signals the end.
func (c *TraefikClient) GetHttpRouters() (*[]HttpRouter, error) {
	result := []HttpRouter{}
	page := 1

	for {
		pageURL := fmt.Sprintf("%s?page=%d", c.httpRoutersUrl, page)
		log.Debugf("Connecting to %s", pageURL)
		response, err := c.client.Get(pageURL)

		if err != nil {
			log.Errorf("Failed to fetch http routers: %q", err)
			return nil, err
		}

		var body []byte
		var readErr error
		if response.Body != nil {
			body, readErr = io.ReadAll(response.Body)
			response.Body.Close()
		}

		if readErr != nil {
			log.Errorf("Failed to read response body: %q", readErr)
			return nil, readErr
		}

		if response.StatusCode == 400 && page > 1 {
			// Traefik returns 400 for a page beyond the last one.
			break
		}

		if response.StatusCode != 200 {
			if body != nil {
				bodyStr := string(body[:])
				return nil, fmt.Errorf("Received %d response from API: %s", response.StatusCode, bodyStr)
			} else {
				return nil, fmt.Errorf("Received %d response from API", response.StatusCode)
			}
		}

		batch := []HttpRouter{}
		if body != nil {
			err = json.Unmarshal(body, &batch)
			if err != nil {
				return nil, fmt.Errorf("Failed to parse json body: %q", err)
			}
		}

		result = append(result, batch...)

		if len(batch) < traefikApiPageSize {
			break
		}

		page++
	}

	return &result, nil
}
