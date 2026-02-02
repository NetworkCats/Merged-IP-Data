package downloader

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"merged-ip-data/internal/config"
)

// Result holds the download result for a single source
type Result struct {
	Source config.DatabaseSource
	Error  error
}

// Downloader handles downloading database files
type Downloader struct {
	client      *http.Client
	maxRetries  int
	retryDelay  time.Duration
	concurrency int
}

// New creates a new Downloader with the given configuration
func New() *Downloader {
	return &Downloader{
		client: &http.Client{
			Timeout: time.Duration(config.DownloadTimeout) * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return fmt.Errorf("too many redirects")
				}
				return nil
			},
		},
		maxRetries:  config.DownloadMaxRetries,
		retryDelay:  time.Duration(config.DownloadRetryDelay) * time.Second,
		concurrency: config.DownloadConcurrency,
	}
}

// DownloadAll downloads all database sources concurrently
func (d *Downloader) DownloadAll(ctx context.Context) ([]Result, error) {
	sources := config.GetAllSources()
	results := make([]Result, len(sources))

	if err := os.MkdirAll("download", 0755); err != nil {
		return nil, fmt.Errorf("failed to create download directory: %w", err)
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, d.concurrency)

	for i, source := range sources {
		wg.Add(1)
		go func(idx int, src config.DatabaseSource) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			err := d.downloadWithRetry(ctx, src)
			results[idx] = Result{
				Source: src,
				Error:  err,
			}
		}(i, source)
	}

	wg.Wait()

	var failedCount int
	for _, result := range results {
		if result.Error != nil {
			failedCount++
		}
	}

	if failedCount > 0 {
		return results, fmt.Errorf("%d of %d downloads failed", failedCount, len(sources))
	}

	return results, nil
}

// downloadWithRetry attempts to download a file with retries
func (d *Downloader) downloadWithRetry(ctx context.Context, source config.DatabaseSource) error {
	var lastErr error

	for attempt := 1; attempt <= d.maxRetries; attempt++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		fmt.Printf("[%s] Downloading (attempt %d/%d)...\n", source.Name, attempt, d.maxRetries)

		err := d.download(ctx, source)
		if err == nil {
			fmt.Printf("[%s] Download completed successfully\n", source.Name)
			return nil
		}

		lastErr = err
		fmt.Printf("[%s] Download failed: %v\n", source.Name, err)

		if attempt < d.maxRetries {
			fmt.Printf("[%s] Retrying in %v...\n", source.Name, d.retryDelay)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(d.retryDelay):
			}
		}
	}

	return fmt.Errorf("failed after %d attempts: %w", d.maxRetries, lastErr)
}

// download performs the actual HTTP download
func (d *Downloader) download(ctx context.Context, source config.DatabaseSource) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source.URL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "Merged-IP-Data/1.0")

	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	if err := os.MkdirAll(filepath.Dir(source.Path), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	tmpPath := source.Path + ".tmp"
	file, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}

	_, err = io.Copy(file, resp.Body)
	if closeErr := file.Close(); closeErr != nil && err == nil {
		err = closeErr
	}

	if err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to write file: %w", err)
	}

	if err := os.Rename(tmpPath, source.Path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to rename file: %w", err)
	}

	fileInfo, err := os.Stat(source.Path)
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}

	fmt.Printf("[%s] Downloaded %s (%d bytes)\n", source.Name, source.Path, fileInfo.Size())
	return nil
}

// VerifyFiles checks that all required database files exist
func VerifyFiles() error {
	sources := config.GetAllSources()
	var missing []string

	for _, source := range sources {
		if _, err := os.Stat(source.Path); os.IsNotExist(err) {
			missing = append(missing, source.Path)
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("missing database files: %v", missing)
	}

	return nil
}
