package writer

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/maxmind/mmdbwriter"
)

// Writer handles writing the merged database to a file
type Writer struct {
	tree *mmdbwriter.Tree
	path string
}

// New creates a new Writer
func New(tree *mmdbwriter.Tree, path string) *Writer {
	return &Writer{
		tree: tree,
		path: path,
	}
}

// Write writes the mmdb tree to the output file
func (w *Writer) Write() error {
	fmt.Printf("Writing merged database to %s...\n", w.path)

	if err := os.MkdirAll(filepath.Dir(w.path), 0755); err != nil {
		if !os.IsExist(err) {
			return fmt.Errorf("failed to create output directory: %w", err)
		}
	}

	tmpPath := w.path + ".tmp"
	file, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}

	written, err := w.tree.WriteTo(file)
	if closeErr := file.Close(); closeErr != nil && err == nil {
		err = closeErr
	}

	if err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to write database: %w", err)
	}

	if err := os.Rename(tmpPath, w.path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to rename output file: %w", err)
	}

	fileInfo, err := os.Stat(w.path)
	if err != nil {
		return fmt.Errorf("failed to stat output file: %w", err)
	}

	fmt.Printf("Database written successfully:\n")
	fmt.Printf("  Path: %s\n", w.path)
	fmt.Printf("  Size: %d bytes (%.2f MB)\n", written, float64(written)/1024/1024)
	fmt.Printf("  File size: %d bytes (%.2f MB)\n", fileInfo.Size(), float64(fileInfo.Size())/1024/1024)

	return nil
}

// WriteToPath is a convenience function to write a tree to a path
func WriteToPath(tree *mmdbwriter.Tree, path string) error {
	w := New(tree, path)
	return w.Write()
}
