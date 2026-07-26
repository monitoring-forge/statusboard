package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync"
	"testing"
	"time"
)

func writeLogAndRenderStatusPage(testCtx context.Context, workerIterations int, opt *Opt, log *ServiceLog, reportErr func(error)) {
	for i := 0; i < workerIterations; i++ {
		select {
		case <-testCtx.Done():
			return
		default:
		}
		status := 0
		if i%2 == 1 {
			status = 1
		}
		err := opt.appendServiceLog(&ServiceLog{
			Time:         time.Now(),
			Name:         "Google",
			CategoryName: "Web",
			Command:      []string{"ping", "google.com"},
			Status:       status,
		})
		if err != nil {
			reportErr(fmt.Errorf("appendServiceLog failed: %w", err))
			return
		}
		if err := opt.renderStatusPage(testCtx); err != nil {
			reportErr(fmt.Errorf("renderStatusPage failed: %w", err))
			return
		}
	}
}

func clientDoRequest(testCtx context.Context, client *http.Client, path string, i int, ts *httptest.Server) (*http.Response, error) {

	req, err := http.NewRequestWithContext(testCtx, http.MethodGet, ts.URL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("new request failed: %w", err)
	}
	if i%3 == 0 {
		req.Header.Set("If-Modified-Since", time.Now().UTC().Format(http.TimeFormat))
	}

	return client.Do(req)
}

func statusOKorNotModified(code int) bool {
	return code == http.StatusOK || code == http.StatusNotModified
}

func clientRequestLoop(testCtx context.Context, id int, ts *httptest.Server, requestsPerClient int, reportErr func(error)) {
	client := ts.Client()
	for i := 0; i < requestsPerClient; i++ {
		select {
		case <-testCtx.Done():
			return
		default:
		}

		path := "/_json"
		if (id+i)%2 == 0 {
			path = "/"
		}

		resp, err := clientDoRequest(testCtx, client, path, i, ts)
		if err != nil {
			reportErr(fmt.Errorf("http request failed: %w", err))
			return
		}

		if !statusOKorNotModified(resp.StatusCode) {
			resp.Body.Close()
			reportErr(fmt.Errorf("unexpected status %d for %s", resp.StatusCode, path))
			return
		}

		if resp.StatusCode == http.StatusNotModified {
			resp.Body.Close()
			continue
		}

		if resp.Header.Get("Last-Modified") == "" {
			resp.Body.Close()
			reportErr(fmt.Errorf("missing Last-Modified header for %s", path))
			return
		}

		if path == "/_json" {
			payload := map[string]any{}
			if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
				resp.Body.Close()
				reportErr(fmt.Errorf("json decode failed: %w", err))
				return
			}
		}

		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}

}

func TestScenario_ConcurrentWorkerAndHTTPHandlers(t *testing.T) {
	opt := newTestOpt(t)

	testCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	now := time.Now()
	writeServiceLog(t, opt.Data, []*ServiceLog{
		{
			Time:         now.Add(-30 * time.Minute),
			Name:         "Google",
			CategoryName: "Web",
			Command:      []string{"ping", "google.com"},
			Status:       0,
		},
	}, now.Format("20060102"))

	if err := opt.renderStatusPage(testCtx); err != nil {
		t.Fatalf("initial renderStatusPage failed: %v", err)
	}

	e := opt.buildHandler()
	ts := httptest.NewServer(e)
	defer ts.Close()

	// set GOMAXPROCS to clientGoroutines + 1 to ensure that the worker and HTTP handlers can run concurrently
	clientGoroutines := runtime.GOMAXPROCS(0) + 1
	var workerIterations = clientGoroutines * 10
	var requestsPerClient = clientGoroutines * 10

	errCh := make(chan error, 100)
	reportErr := func(err error) {
		if err == nil {
			return
		}
		select {
		case errCh <- err:
			cancel()
		default:
		}
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		writeLogAndRenderStatusPage(testCtx, workerIterations, opt, nil, reportErr)
	}()

	for g := 0; g < clientGoroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			clientRequestLoop(testCtx, id, ts, requestsPerClient, reportErr)
		}(g)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		select {
		case err := <-errCh:
			t.Fatal(err)
		default:
		}
	case err := <-errCh:
		t.Fatal(err)
	case <-testCtx.Done():
		t.Fatalf("scenario test timed out: %v", testCtx.Err())
	}
}
