package agent

import (
	"context"
	"log"
	"strings"
	"sync"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	"github.com/we-be/shoal/internal/api"
)

// xhrCollector captures XHR/fetch responses during a navigation using
// CDP Network events. Filters by URL substring if provided.
type xhrCollector struct {
	filter    string
	mu        sync.Mutex
	responses map[network.RequestID]*capturedResponse
}

type capturedResponse struct {
	url     string
	status  int
	headers map[string]string
}

func newXHRCollector(filter string) *xhrCollector {
	return &xhrCollector{
		filter:    filter,
		responses: make(map[network.RequestID]*capturedResponse),
	}
}

// start enables network tracking and registers event listeners.
func (x *xhrCollector) start(ctx context.Context) {
	// Enable network events
	chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		return network.Enable().Do(ctx)
	}))

	// Listen for responses
	chromedp.ListenTarget(ctx, func(ev interface{}) {
		switch e := ev.(type) {
		case *network.EventResponseReceived:
			// Only capture XHR and Fetch requests
			if e.Type != network.ResourceTypeXHR && e.Type != network.ResourceTypeFetch {
				return
			}

			url := e.Response.URL
			if x.filter != "" && !strings.Contains(url, x.filter) {
				return
			}

			headers := make(map[string]string)
			for k, v := range e.Response.Headers {
				if s, ok := v.(string); ok {
					headers[k] = s
				}
			}

			x.mu.Lock()
			x.responses[e.RequestID] = &capturedResponse{
				url:     url,
				status:  int(e.Response.Status),
				headers: headers,
			}
			x.mu.Unlock()
		}
	})
}

// collect retrieves response bodies for all captured XHR responses.
func (x *xhrCollector) collect(ctx context.Context) []api.XHRResponse {
	x.mu.Lock()
	defer x.mu.Unlock()

	if len(x.responses) == 0 {
		return nil
	}

	out := make([]api.XHRResponse, 0, len(x.responses))

	for reqID, resp := range x.responses {
		// Fetch the response body via CDP
		body, err := network.GetResponseBody(reqID).Do(ctx)
		if err != nil {
			log.Printf("xhr: failed to get body for %s: %v", resp.url, err)
			continue
		}

		out = append(out, api.XHRResponse{
			URL:     resp.url,
			Status:  resp.status,
			Headers: resp.headers,
			Body:    string(body),
		})
	}

	return out
}
