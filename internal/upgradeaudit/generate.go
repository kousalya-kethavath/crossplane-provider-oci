package upgradeaudit

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"sigs.k8s.io/yaml"
)

// GenerateReport builds a diff report between the CRDs located at oldRoot and newRoot.
func GenerateReport(oldRoot, newRoot string) (*Report, error) {
	oldDir, err := resolveCRDDir(oldRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve old dir: %w", err)
	}
	newDir, err := resolveCRDDir(newRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve new dir: %w", err)
	}

	oldFiles, err := collectCRDFiles(oldDir)
	if err != nil {
		return nil, fmt.Errorf("collect old files: %w", err)
	}
	newFiles, err := collectCRDFiles(newDir)
	if err != nil {
		return nil, fmt.Errorf("collect new files: %w", err)
	}

	report := &Report{
		OldPath:     oldDir,
		NewPath:     newDir,
		GeneratedAt: time.Now().UTC(),
		Services:    make(map[string]*ServiceReport),
	}

	for rel, nf := range newFiles {
		sr := report.ensureService(nf.ServiceName)
		if of, ok := oldFiles[rel]; !ok {
			ref := ResourceRef{
				Resource:     nf.Resource,
				Scope:        nf.Scope,
				RelativePath: rel,
			}
			sr.AddedResources = append(sr.AddedResources, ref)
			report.Summary.Added++
		} else if nf.Hash != of.Hash {
			diff, diffErr := calculateSchemaDiff(of.Absolute, nf.Absolute)
			fieldSummary := FieldChangeSummary{
				Resource:       nf.Resource,
				Scope:          nf.Scope,
				RelativePath:   rel,
				AddedRequired:  diff.addedRequired,
				AddedOptional:  diff.addedOptional,
				RemovedFields:  diff.removed,
				BecameRequired: diff.becameRequired,
				BecameOptional: diff.becameOptional,
				TypeChanges:    diff.typeChanges,
			}
			if diffErr != nil {
				fieldSummary.Notes = append(fieldSummary.Notes, diffErr.Error())
				report.Notes = append(report.Notes, fmt.Sprintf("%s: %v", rel, diffErr))
			}
			if !fieldSummary.HasChanges() && diffErr == nil {
				fieldSummary.Notes = append(fieldSummary.Notes, "content changed but no schema differences detected")
			}
			sr.Modified = append(sr.Modified, fieldSummary)
			report.Summary.Modified++
		}
	}

	for rel, of := range oldFiles {
		if _, ok := newFiles[rel]; ok {
			continue
		}
		sr := report.ensureService(of.ServiceName)
		ref := ResourceRef{
			Resource:     of.Resource,
			Scope:        of.Scope,
			RelativePath: rel,
		}
		sr.RemovedResources = append(sr.RemovedResources, ref)
		report.Summary.Removed++
	}

	report.sortServiceEntries()
	return report, nil
}

type fileRecord struct {
	Absolute    string
	Relative    string
	Hash        string
	ServiceHost string
	ServiceName string
	Resource    string
	Scope       string
}

func resolveCRDDir(root string) (string, error) {
	info, err := os.Stat(root)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", root)
	}
	if filepath.Base(root) == "crds" {
		return root, nil
	}
	candidate := filepath.Join(root, "crds")
	if stat, err := os.Stat(candidate); err == nil && stat.IsDir() {
		return candidate, nil
	}
	return root, nil
}

func collectCRDFiles(root string) (map[string]fileRecord, error) {
	files := make(map[string]fileRecord)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".yaml") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		hash, err := fileHash(path)
		if err != nil {
			return fmt.Errorf("hash %s: %w", path, err)
		}
		serviceHost, resource := splitCRDName(d.Name())
		serviceName := shortServiceName(serviceHost)
		scope := detectScope(serviceHost)

		files[filepath.ToSlash(rel)] = fileRecord{
			Absolute:    path,
			Relative:    filepath.ToSlash(rel),
			Hash:        hash,
			ServiceHost: serviceHost,
			ServiceName: serviceName,
			Resource:    resource,
			Scope:       scope,
		}
		return nil
	})
	return files, err
}

func fileHash(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func splitCRDName(filename string) (serviceHost, resource string) {
	name := strings.TrimSuffix(filename, ".yaml")
	if idx := strings.Index(name, "_"); idx >= 0 {
		serviceHost = name[:idx]
		resource = name[idx+1:]
	} else {
		serviceHost = name
		resource = name
	}
	return serviceHost, resource
}

func shortServiceName(host string) string {
	if host == "" {
		return "unknown"
	}
	segments := strings.Split(host, ".")
	if len(segments) == 0 {
		return host
	}
	return segments[0]
}

func detectScope(host string) string {
	switch {
	case strings.Contains(host, ".oci.m.upbound.io"):
		return "namespaced"
	case strings.Contains(host, ".oci.upbound.io"):
		return "cluster"
	default:
		return "unknown"
	}
}

func (r *Report) ensureService(serviceID string) *ServiceReport {
	if serviceID == "" {
		serviceID = "unknown"
	}
	if r.Services == nil {
		r.Services = make(map[string]*ServiceReport)
	}
	if sr, ok := r.Services[serviceID]; ok {
		return sr
	}
	sr := &ServiceReport{ServiceID: serviceID}
	r.Services[serviceID] = sr
	return sr
}

func (r *Report) sortServiceEntries() {
	for _, sr := range r.Services {
		sort.Slice(sr.AddedResources, func(i, j int) bool {
			return sr.AddedResources[i].Resource < sr.AddedResources[j].Resource
		})
		sort.Slice(sr.RemovedResources, func(i, j int) bool {
			return sr.RemovedResources[i].Resource < sr.RemovedResources[j].Resource
		})
		sort.Slice(sr.Modified, func(i, j int) bool {
			if sr.Modified[i].Resource == sr.Modified[j].Resource {
				return sr.Modified[i].Scope < sr.Modified[j].Scope
			}
			return sr.Modified[i].Resource < sr.Modified[j].Resource
		})
		for i := range sr.Modified {
			m := &sr.Modified[i]
			sort.Strings(m.AddedRequired)
			sort.Strings(m.AddedOptional)
			sort.Strings(m.RemovedFields)
			sort.Strings(m.BecameRequired)
			sort.Strings(m.BecameOptional)
			sort.Slice(m.TypeChanges, func(i, j int) bool {
				return m.TypeChanges[i].Field < m.TypeChanges[j].Field
			})
		}
	}
}

type propertyInfo struct {
	Type     string
	Required bool
}

type schemaDiff struct {
	addedRequired  []string
	addedOptional  []string
	removed        []string
	becameRequired []string
	becameOptional []string
	typeChanges    []TypeChange
}

func calculateSchemaDiff(oldFile, newFile string) (schemaDiff, error) {
	oldProps, err := extractProperties(oldFile)
	if err != nil {
		return schemaDiff{}, err
	}
	newProps, err := extractProperties(newFile)
	if err != nil {
		return schemaDiff{}, err
	}
	diff := schemaDiff{}
	for path, newInfo := range newProps {
		if oldInfo, ok := oldProps[path]; !ok {
			if newInfo.Required {
				diff.addedRequired = append(diff.addedRequired, path)
			} else {
				diff.addedOptional = append(diff.addedOptional, path)
			}
		} else {
			if !oldInfo.Required && newInfo.Required {
				diff.becameRequired = append(diff.becameRequired, path)
			}
			if oldInfo.Required && !newInfo.Required {
				diff.becameOptional = append(diff.becameOptional, path)
			}
			if normalizeType(oldInfo.Type) != normalizeType(newInfo.Type) {
				diff.typeChanges = append(diff.typeChanges, TypeChange{
					Field:   path,
					OldType: oldInfo.Type,
					NewType: newInfo.Type,
				})
			}
		}
	}
	for path := range oldProps {
		if _, ok := newProps[path]; !ok {
			diff.removed = append(diff.removed, path)
		}
	}
	sort.Strings(diff.addedRequired)
	sort.Strings(diff.addedOptional)
	sort.Strings(diff.removed)
	sort.Strings(diff.becameRequired)
	sort.Strings(diff.becameOptional)
	sort.Slice(diff.typeChanges, func(i, j int) bool {
		return diff.typeChanges[i].Field < diff.typeChanges[j].Field
	})
	return diff, nil
}

func extractProperties(path string) (map[string]propertyInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	docs := splitYAMLDocuments(data)
	properties := make(map[string]propertyInfo)
	for _, doc := range docs {
		if len(strings.TrimSpace(string(doc))) == 0 {
			continue
		}
		var crd apiextensionsv1.CustomResourceDefinition
		if err := yaml.Unmarshal(doc, &crd); err != nil {
			return nil, fmt.Errorf("unmarshal CRD %s: %w", path, err)
		}
		for _, version := range crd.Spec.Versions {
			if version.Schema == nil || version.Schema.OpenAPIV3Schema == nil {
				continue
			}
			required := toSet(version.Schema.OpenAPIV3Schema.Required)
			collectProperties(version.Schema.OpenAPIV3Schema, version.Name, required, properties)
		}
		// Only first CRD document is needed.
		break
	}
	return properties, nil
}

func collectProperties(schema *apiextensionsv1.JSONSchemaProps, prefix string, requiredSet map[string]struct{}, out map[string]propertyInfo) {
	if schema == nil {
		return
	}
	for name, propSchema := range schema.Properties {
		prop := propSchema
		path := joinPath(prefix, name)
		info := propertyInfo{
			Type:     describeType(&prop),
			Required: has(requiredSet, name),
		}
		out[path] = info
		childRequired := toSet(prop.Required)
		collectProperties(&prop, path, childRequired, out)
	}
	if schema.AdditionalProperties != nil && schema.AdditionalProperties.Schema != nil {
		apSchema := schema.AdditionalProperties.Schema
		path := prefix + "{}"
		out[path] = propertyInfo{
			Type:     "map<" + describeType(apSchema) + ">",
			Required: false,
		}
		collectProperties(apSchema, path, toSet(apSchema.Required), out)
	}
	if schema.Items != nil && schema.Items.Schema != nil {
		itemSchema := schema.Items.Schema
		path := prefix + "[]"
		out[path] = propertyInfo{
			Type:     "array<" + describeType(itemSchema) + ">",
			Required: false,
		}
		collectProperties(itemSchema, path, toSet(itemSchema.Required), out)
	}
}

func joinPath(prefix, name string) string {
	if prefix == "" {
		return name
	}
	if name == "" {
		return prefix
	}
	return prefix + "." + name
}

func describeType(schema *apiextensionsv1.JSONSchemaProps) string {
	if schema == nil {
		return ""
	}
	t := strings.TrimSpace(schema.Type)
	if t == "" {
		if schema.Ref != nil {
			return fmt.Sprintf("ref(%s)", *schema.Ref)
		}
		if len(schema.Properties) > 0 || schema.AdditionalProperties != nil {
			return "object"
		}
		return ""
	}
	if t == "array" {
		if schema.Items != nil && schema.Items.Schema != nil {
			return "array<" + describeType(schema.Items.Schema) + ">"
		}
		return "array"
	}
	return t
}

func toSet(list []string) map[string]struct{} {
	set := make(map[string]struct{}, len(list))
	for _, item := range list {
		set[item] = struct{}{}
	}
	return set
}

func has(set map[string]struct{}, key string) bool {
	_, ok := set[key]
	return ok
}

func normalizeType(t string) string {
	return strings.TrimSpace(strings.ToLower(t))
}

func splitYAMLDocuments(data []byte) [][]byte {
	parts := bytes.Split(data, []byte("\n---"))
	docs := make([][]byte, 0, len(parts))
	for _, part := range parts {
		trimmed := bytes.TrimSpace(part)
		if len(trimmed) == 0 {
			continue
		}
		// Restore trailing newline to keep YAML parser happy.
		if trimmed[len(trimmed)-1] != '\n' {
			trimmed = append(trimmed, '\n')
		}
		docs = append(docs, trimmed)
	}
	if len(docs) == 0 && len(bytes.TrimSpace(data)) > 0 {
		docs = append(docs, data)
	}
	return docs
}
