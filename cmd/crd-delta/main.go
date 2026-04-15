package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/pflag"
	"sigs.k8s.io/yaml"
)

type FileEntry struct {
	Path string
	Size int64
	Hash []byte // computed lazily
}

type ServiceIndex map[string][]FileEntry

// fastExtractService extracts service name from CRD file
func fastExtractService(filePath string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}

	// Handle multi-doc YAML: split by --- and process each document
	docs := strings.Split(string(data), "\n---\n")
	for _, doc := range docs {
		if strings.TrimSpace(doc) == "" {
			continue
		}
		var obj map[string]interface{}
		if err := yaml.Unmarshal([]byte(doc), &obj); err != nil {
			continue // try next doc
		}
		if kind, ok := obj["kind"].(string); ok && kind == "CustomResourceDefinition" {
			// Extract from spec.group
			if spec, ok := obj["spec"].(map[string]interface{}); ok {
				if group, ok := spec["group"].(string); ok && group != "" {
					parts := strings.Split(group, ".")
					if len(parts) > 0 {
						return parts[0], nil
					}
				}
			}
			// Fallback: extract from metadata.name (typically <plural>.<group>)
			if meta, ok := obj["metadata"].(map[string]interface{}); ok {
				if name, ok := meta["name"].(string); ok && name != "" {
					parts := strings.Split(name, ".")
					if len(parts) >= 2 {
						group := strings.Join(parts[1:], ".")
						groupParts := strings.Split(group, ".")
						if len(groupParts) > 0 {
							return groupParts[0], nil
						}
					}
				}
			}
		}
	}

	// Last resort: derive from filename prefix before first dot
	base := filepath.Base(filePath)
	if idx := strings.Index(base, "."); idx > 0 {
		return base[:idx], nil
	}

	return "", fmt.Errorf("could not extract service from %s", filePath)
}

// indexDir builds a service index for the given directory without loading full CRDs
func indexDir(dir string) (ServiceIndex, error) {
	idx := make(ServiceIndex)
	crdDir := filepath.Join(dir, "crds")
	if _, err := os.Stat(crdDir); os.IsNotExist(err) {
		// Directory doesn't exist, treat as empty
		return idx, nil
	}
	err := filepath.WalkDir(crdDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".yaml") {
			return nil
		}
		service, err := fastExtractService(path)
		if err != nil {
			return fmt.Errorf("failed to extract service from %s: %v", path, err)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		entry := FileEntry{
			Path: path,
			Size: info.Size(),
		}
		idx[service] = append(idx[service], entry)
		return nil
	})
	return idx, err
}

// normalizeCRD strips volatile fields for consistent hashing
func normalizeCRD(obj map[string]interface{}) {
	// Strip status entirely
	delete(obj, "status")

	// Strip volatile metadata
	if meta, ok := obj["metadata"].(map[string]interface{}); ok {
		delete(meta, "creationTimestamp")
		delete(meta, "managedFields")
	}
}

// hashNormalized computes a canonical hash of the normalized CRD
func hashNormalized(filePath string) ([]byte, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	var obj map[string]interface{}
	if err := yaml.Unmarshal(data, &obj); err != nil {
		return nil, err
	}
	normalizeCRD(obj)
	canon, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(canon)
	return hash[:], nil
}

// computeDelta computes the list of services with differences
func computeDelta(oldDir, newDir string) ([]string, error) {
	idxOld, err := indexDir(oldDir)
	if err != nil {
		return nil, fmt.Errorf("indexing old dir: %v", err)
	}
	idxNew, err := indexDir(newDir)
	if err != nil {
		return nil, fmt.Errorf("indexing new dir: %v", err)
	}

	// Union of services
	serviceSet := make(map[string]bool)
	for svc := range idxOld {
		serviceSet[svc] = true
	}
	for svc := range idxNew {
		serviceSet[svc] = true
	}
	var services []string
	for svc := range serviceSet {
		services = append(services, svc)
	}
	sort.Strings(services)

	var delta []string
	for _, svc := range services {
		filesOld := idxOld[svc]
		filesNew := idxNew[svc]

		// Compare filename sets (basenames)
		oldNames := make(map[string]FileEntry)
		for _, fe := range filesOld {
			oldNames[filepath.Base(fe.Path)] = fe
		}
		newNames := make(map[string]FileEntry)
		for _, fe := range filesNew {
			newNames[filepath.Base(fe.Path)] = fe
		}
		if len(oldNames) != len(newNames) {
			delta = append(delta, svc)
			continue
		}
		for name := range oldNames {
			if _, exists := newNames[name]; !exists {
				delta = append(delta, svc)
				goto nextService
			}
		}

		// Compare contents for common files
		for name, oldFe := range oldNames {
			newFe := newNames[name]
			oldHash := oldFe.Hash
			if oldHash == nil {
				var err error
				oldHash, err = hashNormalized(oldFe.Path)
				if err != nil {
					return nil, fmt.Errorf("hashing old %s: %v", oldFe.Path, err)
				}
				oldFe.Hash = oldHash
			}
			newHash := newFe.Hash
			if newHash == nil {
				var err error
				newHash, err = hashNormalized(newFe.Path)
				if err != nil {
					return nil, fmt.Errorf("hashing new %s: %v", newFe.Path, err)
				}
				newFe.Hash = newHash
			}
			if string(oldHash) != string(newHash) {
				delta = append(delta, svc)
				break
			}
		}
	nextService:
	}

	return delta, nil
}

func main() {
	var oldDir, newDir string
	pflag.StringVar(&oldDir, "old", ".package.bck", "path to old package directory (default .package.bck)")
	pflag.StringVar(&newDir, "new", "package", "path to new package directory (default package)")
	pflag.Parse()

	if oldDir == "" || newDir == "" {
		fmt.Fprintf(os.Stderr, "Usage: %s --old <old-dir> --new <new-dir>\n", os.Args[0])
		os.Exit(1)
	}

	delta, err := computeDelta(oldDir, newDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error computing delta: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(strings.Join(delta, ","))
}
