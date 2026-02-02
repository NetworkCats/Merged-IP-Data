package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"merged-ip-data/internal/config"
	"merged-ip-data/internal/downloader"
	"merged-ip-data/internal/merger"
	"merged-ip-data/internal/writer"
)

func main() {
	skipDownload := flag.Bool("skip-download", false, "Skip downloading databases (use existing files)")
	outputPath := flag.String("output", config.OutputFile, "Output file path")
	flag.Parse()

	fmt.Println("=== Merged IP Database Generator ===")
	fmt.Printf("Output: %s\n\n", *outputPath)

	startTime := time.Now()

	if !*skipDownload {
		if err := downloadDatabases(); err != nil {
			fmt.Fprintf(os.Stderr, "Error downloading databases: %v\n", err)
			os.Exit(1)
		}
	} else {
		fmt.Println("Skipping database download (using existing files)")
		if err := downloader.VerifyFiles(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}

	if err := mergeDatabases(*outputPath); err != nil {
		fmt.Fprintf(os.Stderr, "Error merging databases: %v\n", err)
		os.Exit(1)
	}

	elapsed := time.Since(startTime)
	fmt.Printf("\n=== Complete ===\n")
	fmt.Printf("Total time: %v\n", elapsed)
}

func downloadDatabases() error {
	fmt.Println("=== Downloading Databases ===")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	dl := downloader.New()
	results, err := dl.DownloadAll(ctx)

	fmt.Println("\nDownload Results:")
	for _, result := range results {
		if result.Error != nil {
			fmt.Printf("  [FAIL] %s: %v\n", result.Source.Name, result.Error)
		} else {
			fmt.Printf("  [OK] %s\n", result.Source.Name)
		}
	}

	if err != nil {
		return err
	}

	fmt.Println()
	return nil
}

func mergeDatabases(outputPath string) error {
	fmt.Println("=== Merging Databases ===")

	m, err := merger.New()
	if err != nil {
		return fmt.Errorf("failed to create merger: %w", err)
	}
	defer m.Close()

	if err := m.Merge(); err != nil {
		return fmt.Errorf("failed to merge databases: %w", err)
	}

	fmt.Println("\n=== Writing Output ===")
	if err := writer.WriteToPath(m.Tree(), outputPath); err != nil {
		return fmt.Errorf("failed to write output: %w", err)
	}

	return nil
}
