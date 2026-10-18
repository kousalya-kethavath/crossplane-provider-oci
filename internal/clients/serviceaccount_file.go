/*
Copyright 2026 Oracle and/or its affiliates.
*/

package clients

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// CLI token files belong to the managed resource's Upjet workspace. They remain
// renewable during asynchronous operations and deletion. Workspace finalization
// removes them; process shutdown also removes every file owned by this instance.
type cliServiceAccountTokens struct {
	lifetime context.Context
	baseDir  string
	interval time.Duration
	mu       sync.Mutex
	entries  map[string]*cliServiceAccountToken
}

type cliServiceAccountToken struct {
	owner    *cliServiceAccountTokens
	path     string
	supplier *serviceAccountTokenProvider
	mu       sync.Mutex
	last     string
}

func (m *cliServiceAccountTokens) ensure(uid, identity string, supplier *serviceAccountTokenProvider) (string, error) {
	if uid == "" || uid == "." || uid == ".." || filepath.Base(uid) != uid {
		return "", fmt.Errorf("Terraform CLI ServiceAccount authentication requires a managed resource UID")
	}
	if err := m.lifetime.Err(); err != nil {
		return "", fmt.Errorf("ServiceAccount token provider is shutting down")
	}
	dir := filepath.Join(m.baseDir, uid)
	if info, err := os.Lstat(dir); err == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
		return "", fmt.Errorf("invalid ServiceAccount token workspace")
	} else if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("cannot inspect the ServiceAccount token workspace")
	}
	path := filepath.Join(dir, ".oci-serviceaccount-"+identity+".jwt")
	m.mu.Lock()
	e, exists := m.entries[path]
	m.mu.Unlock()
	if !exists {
		// Do not leave a workspace or renewal loop when initial authorization
		// fails before Upjet has registered its workspace finalizer state.
		if _, err := supplier.ServiceAccountToken(); err != nil {
			return "", err
		}
		if err := os.Mkdir(dir, 0700); err != nil && !os.IsExist(err) {
			return "", fmt.Errorf("cannot create the private ServiceAccount token workspace")
		}
		info, err := os.Lstat(dir)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("invalid ServiceAccount token workspace")
		}
		if err := os.Chmod(dir, 0700); err != nil {
			return "", fmt.Errorf("cannot secure the ServiceAccount token workspace")
		}
		m.mu.Lock()
		if m.entries == nil {
			m.entries = map[string]*cliServiceAccountToken{}
		}
		e, exists = m.entries[path]
		if !exists {
			e = &cliServiceAccountToken{owner: m, path: path, supplier: supplier}
			m.entries[path] = e
		}
		m.mu.Unlock()
		if !exists {
			go e.run()
		}
	}
	if err := e.refresh(); err != nil {
		return "", err
	}
	return path, nil
}

func (e *cliServiceAccountToken) refresh() (err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	defer func() {
		if err != nil {
			_ = os.Remove(e.path)
			e.last = ""
		}
	}()
	token, err := e.supplier.ServiceAccountToken()
	if err != nil {
		_ = os.Remove(e.path)
		e.last = ""
		return err
	}
	if token == e.last {
		if _, err := os.Stat(e.path); err == nil {
			return nil
		}
	}
	f, err := os.CreateTemp(filepath.Dir(e.path), ".oci-token-rotation-")
	if err != nil {
		return fmt.Errorf("cannot rotate the ServiceAccount token file")
	}
	defer os.Remove(f.Name())
	_, writeErr := f.WriteString(token)
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		_ = os.Remove(e.path)
		e.last = ""
		return fmt.Errorf("cannot write the ServiceAccount token file")
	}
	if err := os.Rename(f.Name(), e.path); err != nil {
		return fmt.Errorf("cannot replace the ServiceAccount token file")
	}
	e.last = token
	return nil
}

func (e *cliServiceAccountToken) run() {
	interval := e.owner.interval
	if interval <= 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	defer func() {
		e.mu.Lock()
		_ = os.Remove(e.path)
		e.last = ""
		e.mu.Unlock()
		e.owner.mu.Lock()
		delete(e.owner.entries, e.path)
		e.owner.mu.Unlock()
	}()
	for {
		select {
		case <-e.owner.lifetime.Done():
			return
		case <-ticker.C:
			// Do not recreate a workspace after its finalizer has removed it.
			if _, err := os.Stat(filepath.Dir(e.path)); os.IsNotExist(err) {
				return
			}
			// Refresh errors remove the file and are reported by the next setup
			// or SDK file read. Never log API errors or the JWT here.
			_ = e.refresh()
		}
	}
}
