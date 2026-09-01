package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

var allowedPaths = map[string]bool{
	"/health/live":            true,
	"/health/ready":           true,
	"/api/v1/payment-options": true,
	"/api/v1/payment-methods": true,
	"/api/v1/providers":       true,
}

type result struct {
	duration time.Duration
	status   int
	err      error
}

func main() {
	baseURL := flag.String("base-url", envOr("PAYMENT_PROXY_BASE_URL", "http://localhost:18080"), "Payment Proxy base URL")
	path := flag.String("path", "/api/v1/payment-options", "allowlisted read-only endpoint")
	total := flag.Int("requests", 10_000, "total request count")
	concurrency := flag.Int("concurrency", 100, "parallel workers")
	timeout := flag.Duration("timeout", 3*time.Second, "per-request timeout")
	p95Target := flag.Duration("p95-target", 500*time.Millisecond, "maximum accepted p95")
	maxErrorRate := flag.Float64("max-error-rate", 0.001, "maximum accepted non-2xx/network error ratio")
	flag.Parse()

	if err := run(*baseURL, *path, *total, *concurrency, *timeout, *p95Target, *maxErrorRate); err != nil {
		fmt.Fprintln(os.Stderr, "read-only load test failed:", err)
		os.Exit(1)
	}
}

func run(baseURL, path string, total, concurrency int, timeout, p95Target time.Duration, maxErrorRate float64) error {
	if total < 1 || total > 1_000_000 || concurrency < 1 || concurrency > 2_000 {
		return errors.New("requests must be 1..1000000 and concurrency must be 1..2000")
	}
	if timeout <= 0 || p95Target <= 0 || maxErrorRate < 0 || maxErrorRate > 1 {
		return errors.New("timeout, p95 target, or error rate is invalid")
	}
	base, err := url.Parse(strings.TrimRight(strings.TrimSpace(baseURL), "/"))
	if err != nil || base.Host == "" || (base.Scheme != "http" && base.Scheme != "https") || base.User != nil {
		return errors.New("base URL must be an HTTP(S) origin without credentials")
	}
	requested, err := url.Parse(path)
	if err != nil || !allowedPaths[requested.Path] || requested.RawQuery != "" || requested.Fragment != "" {
		return errors.New("path is not in the read-only load-test allowlist")
	}
	serviceKey := strings.TrimSpace(os.Getenv("SERVICE_API_KEY"))
	merchantID := strings.TrimSpace(envOr("DASHBOARD_MERCHANT_ID", "merchant_load_test"))
	mode := strings.TrimSpace(envOr("PAYMENT_PROXY_EXECUTION_MODE", "sandbox"))
	if strings.HasPrefix(requested.Path, "/api/v1/") && serviceKey == "" {
		return errors.New("SERVICE_API_KEY is required for /api/v1 read tests")
	}

	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		MaxIdleConns:          concurrency * 2,
		MaxIdleConnsPerHost:   concurrency,
		MaxConnsPerHost:       concurrency,
		IdleConnTimeout:       30 * time.Second,
		ResponseHeaderTimeout: timeout,
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return errors.New("redirects are not allowed during load tests")
		},
	}
	defer transport.CloseIdleConnections()

	jobs := make(chan struct{})
	results := make(chan result, total)
	var workers sync.WaitGroup
	workers.Add(concurrency)
	if requested.Path == "/api/v1/payment-options" {
		query := requested.Query()
		query.Set("environment", mode)
		requested.RawQuery = query.Encode()
	}
	target := base.ResolveReference(requested).String()
	for worker := 0; worker < concurrency; worker++ {
		go func() {
			defer workers.Done()
			for range jobs {
				started := time.Now()
				request, requestErr := http.NewRequestWithContext(context.Background(), http.MethodGet, target, nil)
				if requestErr == nil && strings.HasPrefix(requested.Path, "/api/v1/") {
					request.Header.Set("Authorization", "Bearer "+serviceKey)
					request.Header.Set("X-Emisell-Merchant-ID", merchantID)
				}
				if requestErr != nil {
					results <- result{duration: time.Since(started), err: requestErr}
					continue
				}
				response, requestErr := client.Do(request)
				status := 0
				if response != nil {
					status = response.StatusCode
					_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
					_ = response.Body.Close()
				}
				results <- result{duration: time.Since(started), status: status, err: requestErr}
			}
		}()
	}
	started := time.Now()
	go func() {
		for request := 0; request < total; request++ {
			jobs <- struct{}{}
		}
		close(jobs)
		workers.Wait()
		close(results)
	}()

	durations := make([]time.Duration, 0, total)
	failures := 0
	statusCounts := make(map[int]int)
	for item := range results {
		durations = append(durations, item.duration)
		statusCounts[item.status]++
		if item.err != nil || item.status < 200 || item.status >= 300 {
			failures++
		}
	}
	elapsed := time.Since(started)
	sort.Slice(durations, func(left, right int) bool { return durations[left] < durations[right] })
	p50 := percentile(durations, 0.50)
	p95 := percentile(durations, 0.95)
	p99 := percentile(durations, 0.99)
	errorRate := float64(failures) / float64(total)
	rps := float64(total) / elapsed.Seconds()
	fmt.Printf("read_only_load_test endpoint=%s requests=%d concurrency=%d elapsed=%s rps=%.1f p50=%s p95=%s p99=%s failures=%d error_rate=%.4f status=%v\n",
		requested.Path, total, concurrency, elapsed.Round(time.Millisecond), rps, p50.Round(time.Millisecond), p95.Round(time.Millisecond), p99.Round(time.Millisecond), failures, errorRate, statusCounts)
	if errorRate > maxErrorRate {
		return fmt.Errorf("error rate %.4f exceeds %.4f", errorRate, maxErrorRate)
	}
	if p95 > p95Target {
		return fmt.Errorf("p95 %s exceeds target %s", p95, p95Target)
	}
	return nil
}

func percentile(values []time.Duration, percentile float64) time.Duration {
	if len(values) == 0 {
		return 0
	}
	index := int(float64(len(values)-1) * percentile)
	return values[index]
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
