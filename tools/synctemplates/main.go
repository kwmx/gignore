// Command synctemplates refreshes the templates bundled into the binary from a
// local checkout of https://github.com/github/gitignore.
//
//	git clone --depth 1 https://github.com/github/gitignore /tmp/gitignore
//	go run ./tools/synctemplates -src /tmp/gitignore
package main

import (
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	src := flag.String("src", "", "path to a checkout of github.com/github/gitignore")
	dst := flag.String("dst", "internal/catalog/builtin/data", "destination directory")
	flag.Parse()
	if *src == "" {
		fmt.Fprintln(os.Stderr, "synctemplates: -src is required")
		os.Exit(2)
	}
	if err := run(*src, *dst); err != nil {
		fmt.Fprintln(os.Stderr, "synctemplates:", err)
		os.Exit(1)
	}
}

func run(src, dst string) error {
	if err := os.RemoveAll(dst); err != nil {
		return err
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	count := 0
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && strings.HasPrefix(d.Name(), ".") && path != src {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".gitignore") {
			return nil
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out := filepath.Join(dst, filepath.ToSlash(rel))
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		count++
		return os.WriteFile(out, data, 0o644)
	})
	if err != nil {
		return err
	}
	license, err := os.ReadFile(filepath.Join(src, "LICENSE"))
	if err != nil {
		return fmt.Errorf("read upstream license: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dst, "LICENSE"), license, 0o644); err != nil {
		return err
	}
	commit := "unknown"
	if out, err := exec.Command("git", "-C", src, "rev-parse", "HEAD").Output(); err == nil {
		commit = strings.TrimSpace(string(out))
	}
	source := fmt.Sprintf("repository=https://github.com/github/gitignore\ncommit=%s\nsynced=%s\n",
		commit, time.Now().UTC().Format("2006-01-02"))
	if err := os.WriteFile(filepath.Join(dst, "SOURCE"), []byte(source), 0o644); err != nil {
		return err
	}
	fmt.Printf("synced %d templates from %s (%s)\n", count, src, commit[:min(12, len(commit))])
	return nil
}
