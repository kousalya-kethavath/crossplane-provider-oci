// Package noforkscope prepares the patched Terraform OCI provider for
// compile-time service scoping and generates one provider entrypoint per
// Crossplane service.
package noforkscope

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

const providerModule = "github.com/oracle/terraform-provider-oci"

// Scope describes the generated upstream dependencies for one Crossplane
// service provider binary.
type Scope struct {
	Service          string   `json:"service"`
	Resources        []string `json:"resources"`
	UpstreamPackages []string `json:"upstreamPackages"`
	ClientRegistrars []string `json:"clientRegistrars"`
}

type constructor struct {
	Function   string
	ImportPath string
}

type clientModel struct {
	methods         map[string]struct{}
	methodRegistrar map[string]string
	methodCalls     map[string][]string
	registrars      []string
}

// PrepareProvider removes global registration roots from scoped builds and
// adds the common factories used by generated service entrypoints.
func PrepareProvider(providerDir string) error {
	if err := rewriteProviderBase(filepath.Join(providerDir, "internal", "provider", "provider.go")); err != nil {
		return err
	}
	if err := writeScopedProviderBase(providerDir); err != nil {
		return err
	}
	if err := writeFullProviderAliases(providerDir); err != nil {
		return err
	}
	if err := excludeScopedRegistrationRoots(providerDir); err != nil {
		return err
	}
	registrars, err := rewriteClientRegistrations(providerDir)
	if err != nil {
		return err
	}
	if err := writeClientRegistrationSet(providerDir, registrars); err != nil {
		return err
	}
	if err := writeFullClientRegistrationInit(providerDir); err != nil {
		return err
	}
	return writeFullProviderEntrypoint(providerDir)
}

// Generate writes a build-tag-selected OCI provider entrypoint for every
// service and returns the deterministic dependency report.
func Generate(providerDir, reportPath string, serviceResources map[string][]string) ([]Scope, error) {
	constructors, err := discoverResourceConstructors(providerDir)
	if err != nil {
		return nil, err
	}
	clients, err := discoverClientModel(providerDir)
	if err != nil {
		return nil, err
	}

	stale, err := filepath.Glob(filepath.Join(providerDir, "oci", "provider_scoped_*.go"))
	if err != nil {
		return nil, fmt.Errorf("find stale scoped provider files: %w", err)
	}
	for _, path := range stale {
		if err := os.Remove(path); err != nil {
			return nil, fmt.Errorf("remove stale scoped provider %s: %w", path, err)
		}
	}

	services := make([]string, 0, len(serviceResources))
	for service := range serviceResources {
		services = append(services, service)
	}
	sort.Strings(services)

	report := make([]Scope, 0, len(services))
	for _, service := range services {
		if !validBuildTagComponent(service) {
			return nil, fmt.Errorf("service %q cannot be used in a Go build tag", service)
		}

		resources := slices.Clone(serviceResources[service])
		sort.Strings(resources)
		resources = slices.Compact(resources)

		selected := make(map[string]constructor, len(resources))
		packageSet := map[string]struct{}{}
		for _, resource := range resources {
			c, ok := constructors[resource]
			if !ok {
				return nil, fmt.Errorf("service %s resource %s has no SDKv2 constructor", service, resource)
			}
			selected[resource] = c
			packageSet[c.ImportPath] = struct{}{}
		}

		packages := make([]string, 0, len(packageSet))
		registrarSet := map[string]struct{}{}
		for importPath := range packageSet {
			packages = append(packages, importPath)
			packageDir := filepath.Join(providerDir, strings.TrimPrefix(importPath, providerModule+"/"))
			registrars, err := clientRegistrarsForPackage(packageDir, clients)
			if err != nil {
				return nil, fmt.Errorf("discover clients for service %s package %s: %w", service, importPath, err)
			}
			for _, registrar := range registrars {
				registrarSet[registrar] = struct{}{}
			}
		}
		sort.Strings(packages)

		registrars := make([]string, 0, len(registrarSet))
		for registrar := range registrarSet {
			registrars = append(registrars, registrar)
		}
		sort.Strings(registrars)

		if err := writeScopedProviderEntrypoint(providerDir, service, resources, selected, packages, registrars); err != nil {
			return nil, err
		}
		report = append(report, Scope{
			Service:          service,
			Resources:        resources,
			UpstreamPackages: packages,
			ClientRegistrars: registrars,
		})
	}

	if reportPath != "" {
		content, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("marshal service scope report: %w", err)
		}
		content = append(content, '\n')
		if err := os.WriteFile(reportPath, content, 0o644); err != nil {
			return nil, fmt.Errorf("write service scope report %s: %w", reportPath, err)
		}
	}
	return report, nil
}

func rewriteProviderBase(path string) error {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("parse provider base %s: %w", path, err)
	}

	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.IMPORT {
			continue
		}
		filtered := gen.Specs[:0]
		for _, spec := range gen.Specs {
			imp := spec.(*ast.ImportSpec)
			importPath, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				return fmt.Errorf("unquote provider import %s: %w", imp.Path.Value, err)
			}
			if importPath == providerModule+"/internal/service/core" ||
				importPath == providerModule+"/internal/service/load_balancer" ||
				importPath == providerModule+"/internal/tfresource" {
				continue
			}
			filtered = append(filtered, spec)
		}
		gen.Specs = filtered
	}

	rewritten := map[string]bool{"DataSourcesMap": false, "ResourcesMap": false}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil {
			continue
		}
		var field string
		switch fn.Name.Name {
		case "DataSourcesMap":
			field = "OciDatasources"
		case "ResourcesMap":
			field = "OciResources"
		default:
			continue
		}
		fn.Body = &ast.BlockStmt{List: []ast.Stmt{&ast.ReturnStmt{Results: []ast.Expr{&ast.SelectorExpr{X: ast.NewIdent("globalvar"), Sel: ast.NewIdent(field)}}}}}
		rewritten[fn.Name.Name] = true
	}
	for name, ok := range rewritten {
		if !ok {
			return fmt.Errorf("provider base %s does not declare %s", path, name)
		}
	}
	return writeFormattedAST(path, fset, file)
}

func writeScopedProviderBase(providerDir string) error {
	const source = `// Copyright (c) 2026, Oracle and/or its affiliates.
// Licensed under the Mozilla Public License Version 2.0

package provider

import "github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

// ProviderWithResources returns a provider with the common OCI configuration
// behavior and only the supplied SDKv2 resource constructors.
func ProviderWithResources(resources map[string]*schema.Resource) *schema.Provider {
	return &schema.Provider{
		DataSourcesMap: map[string]*schema.Resource{},
		Schema:         SchemaMap(),
		ResourcesMap:   resources,
		ConfigureFunc:  ProviderConfig,
	}
}
`
	return writeFormattedSource(filepath.Join(providerDir, "internal", "provider", "provider_nofork_scoped.go"), []byte(source))
}

func writeFullProviderAliases(providerDir string) error {
	const source = `//go:build !nofork_scoped

// Copyright (c) 2026, Oracle and/or its affiliates.
// Licensed under the Mozilla Public License Version 2.0

package provider

import (
	core "github.com/oracle/terraform-provider-oci/internal/service/core"
	loadbalancer "github.com/oracle/terraform-provider-oci/internal/service/load_balancer"
	"github.com/oracle/terraform-provider-oci/internal/tfresource"
)

func init() {
	tfresource.RegisterDatasource("oci_core_listing_resource_version", core.CoreAppCatalogListingResourceVersionDataSource())
	tfresource.RegisterDatasource("oci_core_listing_resource_versions", core.CoreAppCatalogListingResourceVersionsDataSource())
	tfresource.RegisterDatasource("oci_core_shape", core.CoreShapesDataSource())
	tfresource.RegisterDatasource("oci_core_virtual_networks", core.CoreVcnsDataSource())
	tfresource.RegisterDatasource("oci_load_balancers", loadbalancer.LoadBalancerLoadBalancersDataSource())
	tfresource.RegisterDatasource("oci_load_balancer_backendsets", loadbalancer.LoadBalancerBackendSetsDataSource())

	tfresource.RegisterResource("oci_core_virtual_network", core.CoreVcnResource())
	tfresource.RegisterResource("oci_load_balancer", loadbalancer.LoadBalancerLoadBalancerResource())
	tfresource.RegisterResource("oci_load_balancer_backendset", loadbalancer.LoadBalancerBackendSetResource())
}
`
	return writeFormattedSource(filepath.Join(providerDir, "internal", "provider", "provider_nofork_full_aliases.go"), []byte(source))
}

func excludeScopedRegistrationRoots(providerDir string) error {
	providerFiles, err := filepath.Glob(filepath.Join(providerDir, "internal", "provider", "register_*.go"))
	if err != nil {
		return fmt.Errorf("find provider registration files: %w", err)
	}
	for _, path := range providerFiles {
		hasInit, err := hasTopLevelInit(path)
		if err != nil {
			return err
		}
		if hasInit {
			if err := addBuildConstraint(path, "!nofork_scoped"); err != nil {
				return err
			}
		}
	}

	serviceRoot := filepath.Join(providerDir, "internal", "service")
	return filepath.WalkDir(serviceRoot, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		hasInit, err := hasTopLevelInit(path)
		if err != nil {
			return err
		}
		if !hasInit {
			return nil
		}
		if !strings.HasSuffix(path, "_export.go") {
			rel, _ := filepath.Rel(providerDir, path)
			return fmt.Errorf("unexpected scoped service init root in %s", rel)
		}
		return addBuildConstraint(path, "!nofork_scoped")
	})
}

func rewriteClientRegistrations(providerDir string) ([]string, error) {
	clientDir := filepath.Join(providerDir, "internal", "client")
	entries, err := os.ReadDir(clientDir)
	if err != nil {
		return nil, fmt.Errorf("read client directory: %w", err)
	}

	var registrars []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), "_clients.go") || entry.Name() == "provider_clients.go" {
			continue
		}
		path := filepath.Join(clientDir, entry.Name())
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			return nil, fmt.Errorf("parse client registration %s: %w", path, err)
		}

		registrar := "Register" + exportedIdentifier(strings.TrimSuffix(entry.Name(), "_clients.go")) + "Clients"
		found := false
		changed := false
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil {
				continue
			}
			if fn.Name.Name == "init" {
				fn.Name.Name = registrar
				found = true
				changed = true
			} else if fn.Name.Name == registrar {
				found = true
			}
		}
		if !found {
			continue
		}
		if changed {
			if err := writeFormattedAST(path, fset, file); err != nil {
				return nil, err
			}
		}
		registrars = append(registrars, registrar)
	}
	if len(registrars) == 0 {
		return nil, errors.New("no OCI client registration functions found")
	}
	sort.Strings(registrars)
	return registrars, nil
}

func writeClientRegistrationSet(providerDir string, registrars []string) error {
	var source strings.Builder
	source.WriteString(`// Copyright (c) 2026, Oracle and/or its affiliates.
// Licensed under the Mozilla Public License Version 2.0

package client

// ConfigureClientRegistrations installs an immutable-at-runtime client set.
// Provider entrypoints call it once during process startup.
func ConfigureClientRegistrations(registrars ...func()) {
	OracleClientRegistrationsVar = &OracleClientRegistrations{
		RegisteredClients: make(map[string]*OracleClient),
	}
	for _, register := range registrars {
		register()
	}
}

// RegisterAllClients preserves the complete upstream provider behavior.
func RegisterAllClients() {
	ConfigureClientRegistrations(
`)
	for _, registrar := range registrars {
		fmt.Fprintf(&source, "\t\t%s,\n", registrar)
	}
	source.WriteString("\t)\n}\n")
	return writeFormattedSource(filepath.Join(providerDir, "internal", "client", "nofork_client_registrations.go"), []byte(source.String()))
}

func writeFullClientRegistrationInit(providerDir string) error {
	const source = `//go:build !nofork_scoped

// Copyright (c) 2026, Oracle and/or its affiliates.
// Licensed under the Mozilla Public License Version 2.0

package client

func init() {
	RegisterAllClients()
}
`
	return writeFormattedSource(filepath.Join(providerDir, "internal", "client", "nofork_client_registrations_full.go"), []byte(source))
}

func writeFullProviderEntrypoint(providerDir string) error {
	const source = `//go:build !nofork_scoped

// Copyright (c) 2026, Oracle and/or its affiliates.
// Licensed under the Mozilla Public License Version 2.0

package oci

import (
	fwprovider "github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	internalprovider "github.com/oracle/terraform-provider-oci/internal/provider"
)

// Provider returns a fresh complete SDKv2 provider instance.
func Provider() *schema.Provider {
	return internalprovider.Provider()
}

// New returns a fresh complete Plugin Framework provider instance.
func New() fwprovider.Provider {
	return internalprovider.New()
}
`
	return writeFormattedSource(filepath.Join(providerDir, "oci", "provider.go"), []byte(source))
}

func discoverResourceConstructors(providerDir string) (map[string]constructor, error) {
	root := filepath.Join(providerDir, "internal", "service")
	constructors := map[string]constructor{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || entry.Name() != "register_resource.go" {
			return nil
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return fmt.Errorf("parse resource registry %s: %w", path, err)
		}
		relDir, err := filepath.Rel(providerDir, filepath.Dir(path))
		if err != nil {
			return err
		}
		importPath := providerModule + "/" + filepath.ToSlash(relDir)
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || len(call.Args) != 2 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "RegisterResource" {
				return true
			}
			name, ok := call.Args[0].(*ast.BasicLit)
			if !ok || name.Kind != token.STRING {
				return true
			}
			resource, err := strconv.Unquote(name.Value)
			if err != nil {
				return true
			}
			factoryCall, ok := call.Args[1].(*ast.CallExpr)
			if !ok {
				return true
			}
			factory, ok := factoryCall.Fun.(*ast.Ident)
			if !ok {
				return true
			}
			constructors[resource] = constructor{Function: factory.Name, ImportPath: importPath}
			return true
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("discover resource constructors: %w", err)
	}
	if err := discoverAliasConstructors(filepath.Join(providerDir, "internal", "provider", "provider_nofork_full_aliases.go"), constructors); err != nil {
		return nil, err
	}
	if len(constructors) == 0 {
		return nil, errors.New("no SDKv2 resource constructors found")
	}
	return constructors, nil
}

func discoverAliasConstructors(path string, constructors map[string]constructor) error {
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		return fmt.Errorf("parse full provider aliases %s: %w", path, err)
	}
	imports := map[string]string{}
	for _, imp := range file.Imports {
		importPath, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			return err
		}
		name := filepath.Base(importPath)
		if imp.Name != nil {
			name = imp.Name.Name
		}
		imports[name] = importPath
	}
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || len(call.Args) != 2 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "RegisterResource" {
			return true
		}
		name, ok := call.Args[0].(*ast.BasicLit)
		if !ok || name.Kind != token.STRING {
			return true
		}
		resource, err := strconv.Unquote(name.Value)
		if err != nil {
			return true
		}
		factoryCall, ok := call.Args[1].(*ast.CallExpr)
		if !ok {
			return true
		}
		factory, ok := factoryCall.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := factory.X.(*ast.Ident)
		if !ok {
			return true
		}
		importPath, ok := imports[pkg.Name]
		if !ok || !strings.HasPrefix(importPath, providerModule+"/internal/service/") {
			return true
		}
		constructors[resource] = constructor{
			Function:   factory.Sel.Name,
			ImportPath: importPath,
		}
		return true
	})
	return nil
}

func discoverClientModel(providerDir string) (clientModel, error) {
	model := clientModel{
		methods:         map[string]struct{}{},
		methodRegistrar: map[string]string{},
		methodCalls:     map[string][]string{},
	}
	clientDir := filepath.Join(providerDir, "internal", "client")
	entries, err := os.ReadDir(clientDir)
	if err != nil {
		return model, fmt.Errorf("read client directory: %w", err)
	}

	fileRegistrars := map[string]string{}
	fileMethods := map[string][]string{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(clientDir, entry.Name())
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return model, fmt.Errorf("parse client file %s: %w", path, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			if fn.Recv == nil {
				if strings.HasPrefix(fn.Name.Name, "Register") && strings.HasSuffix(fn.Name.Name, "Clients") && fn.Name.Name != "RegisterAllClients" {
					fileRegistrars[path] = fn.Name.Name
				}
				continue
			}
			receiverName, ok := oracleClientsReceiver(fn)
			if !ok {
				continue
			}
			model.methods[fn.Name.Name] = struct{}{}
			fileMethods[path] = append(fileMethods[path], fn.Name.Name)
			if fn.Body != nil {
				ast.Inspect(fn.Body, func(node ast.Node) bool {
					sel, ok := node.(*ast.SelectorExpr)
					if !ok {
						return true
					}
					id, ok := sel.X.(*ast.Ident)
					if ok && id.Name == receiverName {
						model.methodCalls[fn.Name.Name] = append(model.methodCalls[fn.Name.Name], sel.Sel.Name)
					}
					return true
				})
			}
		}
	}
	for path, registrar := range fileRegistrars {
		model.registrars = append(model.registrars, registrar)
		for _, method := range fileMethods[path] {
			model.methodRegistrar[method] = registrar
		}
	}
	sort.Strings(model.registrars)
	if len(model.registrars) == 0 {
		return model, errors.New("no rewritten client registrars found")
	}
	return model, nil
}

func clientRegistrarsForPackage(packageDir string, model clientModel) ([]string, error) {
	selectedMethods := map[string]struct{}{}
	err := filepath.WalkDir(packageDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || strings.HasSuffix(path, "_export.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(node ast.Node) bool {
			sel, ok := node.(*ast.SelectorExpr)
			if ok {
				if _, known := model.methods[sel.Sel.Name]; known {
					selectedMethods[sel.Sel.Name] = struct{}{}
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		return nil, err
	}

	registrars := map[string]struct{}{}
	visited := map[string]struct{}{}
	var visit func(string)
	visit = func(method string) {
		if _, ok := visited[method]; ok {
			return
		}
		visited[method] = struct{}{}
		if registrar, ok := model.methodRegistrar[method]; ok {
			registrars[registrar] = struct{}{}
		}
		for _, dependency := range model.methodCalls[method] {
			visit(dependency)
		}
	}
	for method := range selectedMethods {
		visit(method)
	}

	result := make([]string, 0, len(registrars))
	for registrar := range registrars {
		result = append(result, registrar)
	}
	sort.Strings(result)
	return result, nil
}

func writeScopedProviderEntrypoint(providerDir, service string, resources []string, constructors map[string]constructor, packages, registrars []string) error {
	aliases := make(map[string]string, len(packages))
	for i, importPath := range packages {
		aliases[importPath] = fmt.Sprintf("service%d", i)
	}

	var source strings.Builder
	fmt.Fprintf(&source, "//go:build nofork_scoped && oci_service_%s\n\n", service)
	source.WriteString(`// Copyright (c) 2026, Oracle and/or its affiliates.
// Licensed under the Mozilla Public License Version 2.0

// Code generated by crossplane-provider-oci noforkscope. DO NOT EDIT.

package oci

import (
	"sync"

	fwprovider "github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/oracle/terraform-provider-oci/internal/client"
	internalprovider "github.com/oracle/terraform-provider-oci/internal/provider"
`)
	for _, importPath := range packages {
		fmt.Fprintf(&source, "\t%s %q\n", aliases[importPath], importPath)
	}
	source.WriteString(")\n\nvar registerScopedClients sync.Once\n\n")
	source.WriteString("// Provider returns a fresh service-scoped SDKv2 provider instance.\nfunc Provider() *schema.Provider {\n")
	source.WriteString("\tregisterScopedClients.Do(func() {\n\t\tclient.ConfigureClientRegistrations(\n")
	for _, registrar := range registrars {
		fmt.Fprintf(&source, "\t\t\tclient.%s,\n", registrar)
	}
	source.WriteString("\t\t)\n\t})\n\treturn internalprovider.ProviderWithResources(map[string]*schema.Resource{\n")
	for _, resource := range resources {
		c := constructors[resource]
		fmt.Fprintf(&source, "\t\t%q: %s.%s(),\n", resource, aliases[c.ImportPath], c.Function)
	}
	source.WriteString("\t})\n}\n\n")
	source.WriteString("// New returns no Framework provider because OCI Framework resources are not routed by Crossplane.\nfunc New() fwprovider.Provider {\n\treturn nil\n}\n")

	path := filepath.Join(providerDir, "oci", "provider_scoped_"+service+".go")
	return writeFormattedSource(path, []byte(source.String()))
}

func oracleClientsReceiver(fn *ast.FuncDecl) (string, bool) {
	if fn.Recv == nil || len(fn.Recv.List) != 1 || len(fn.Recv.List[0].Names) != 1 {
		return "", false
	}
	typeExpr := fn.Recv.List[0].Type
	if star, ok := typeExpr.(*ast.StarExpr); ok {
		typeExpr = star.X
	}
	ident, ok := typeExpr.(*ast.Ident)
	if !ok || ident.Name != "OracleClients" {
		return "", false
	}
	return fn.Recv.List[0].Names[0].Name, true
}

func hasTopLevelInit(path string) (bool, error) {
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		return false, fmt.Errorf("parse %s: %w", path, err)
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Recv == nil && fn.Name.Name == "init" {
			return true, nil
		}
	}
	return false, nil
}

func addBuildConstraint(path, expression string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	constraint := "//go:build " + expression
	if bytes.HasPrefix(content, []byte(constraint+"\n")) {
		return nil
	}
	if bytes.Contains(content[:min(len(content), 512)], []byte("//go:build ")) {
		return fmt.Errorf("%s already has a different build constraint", path)
	}
	updated := append([]byte(constraint+"\n\n"), content...)
	if err := os.WriteFile(path, updated, 0o644); err != nil {
		return fmt.Errorf("add build constraint to %s: %w", path, err)
	}
	return nil
}

func writeFormattedAST(path string, fset *token.FileSet, file *ast.File) error {
	var content bytes.Buffer
	if err := format.Node(&content, fset, file); err != nil {
		return fmt.Errorf("format %s: %w", path, err)
	}
	content.WriteByte('\n')
	return os.WriteFile(path, content.Bytes(), 0o644)
}

func writeFormattedSource(path string, source []byte) error {
	formatted, err := format.Source(source)
	if err != nil {
		return fmt.Errorf("format generated source %s: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, formatted, 0o644); err != nil {
		return fmt.Errorf("write generated source %s: %w", path, err)
	}
	return nil
}

func exportedIdentifier(value string) string {
	var result strings.Builder
	upper := true
	for _, r := range value {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			upper = true
			continue
		}
		if upper {
			r = unicode.ToUpper(r)
			upper = false
		}
		result.WriteRune(r)
	}
	return result.String()
}

func validBuildTagComponent(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if !unicode.IsLower(r) && !unicode.IsDigit(r) && r != '_' {
			return false
		}
	}
	return true
}
