/*
Copyright 2026 Oracle and/or its affiliates.
*/

package clients

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	xpv1 "github.com/crossplane/crossplane-runtime/v2/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/v2/pkg/resource/fake"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kubetesting "k8s.io/client-go/testing"

	namespacedv1beta1 "github.com/oracle/provider-oci/apis/namespaced/v1beta1"
)

func TestCLITokenBackgroundRenewalWithoutReconciliation(t *testing.T) {
	s, tokens, _ := serviceAccountFixture(t)
	var now atomic.Int64
	now.Store(time.Now().Unix())
	var requests atomic.Int64
	tokens.PrependReactor("create", "serviceaccounts", func(kubetesting.Action) (bool, runtime.Object, error) {
		requests.Add(1)
		exp := time.Unix(now.Load(), 0).Add(time.Hour)
		return true, &authenticationv1.TokenRequest{Status: authenticationv1.TokenRequestStatus{Token: tokenForAccount(t, types.NamespacedName{Namespace: "tenant-a", Name: "oci-access"}, "sa-tenant-a", exp), ExpirationTimestamp: metav1.NewTime(exp)}}, nil
	})
	_, a, err := s.configuration(t.Context(), serviceAccountConfig("tenant-a"))
	if err != nil {
		t.Fatal(err)
	}
	p := a.options.OKEWorkloadIdentityTokenProvider.(*serviceAccountTokenProvider)
	p.now = func() time.Time { return time.Unix(now.Load(), 0) }
	p.jitter = func() float64 { return 0 }
	m := &cliServiceAccountTokens{lifetime: t.Context(), baseDir: t.TempDir(), interval: time.Millisecond}
	path, err := m.ensure("resource", a.identity, p)
	if err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	now.Add(int64((45 * time.Minute) / time.Second))
	deadline := time.Now().Add(2 * time.Second)
	for {
		data, err := os.ReadFile(path)
		if err == nil && string(data) != string(first) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("background did not rotate JWT")
		}
		time.Sleep(time.Millisecond)
	}
	if requests.Load() != 2 {
		t.Fatalf("unexpected TokenRequests %d", requests.Load())
	}
}

func TestCLIInitialAuthorizationFailureLeavesNoWorkspace(t *testing.T) {
	s, tokens, _ := serviceAccountFixture(t)
	tokens.PrependReactor("create", "serviceaccounts", func(kubetesting.Action) (bool, runtime.Object, error) { return true, nil, context.DeadlineExceeded })
	_, a, err := s.configuration(t.Context(), serviceAccountConfig("tenant-a"))
	if err != nil {
		t.Fatal(err)
	}
	m := &cliServiceAccountTokens{lifetime: t.Context(), baseDir: t.TempDir()}
	if _, err := m.ensure("resource", a.identity, a.options.OKEWorkloadIdentityTokenProvider.(*serviceAccountTokenProvider)); err == nil {
		t.Fatal("expected authorization failure")
	}
	if _, err := os.Stat(filepath.Join(m.baseDir, "resource")); !os.IsNotExist(err) {
		t.Fatal("failed initial authentication left workspace")
	}
	if len(m.entries) != 0 {
		t.Fatal("failed initial authentication retained a renewal loop")
	}
}

func TestCLIServiceAccountTokenFileRotationAndIsolation(t *testing.T) {
	s, tokens, _ := serviceAccountFixture(t)
	now := time.Now().Truncate(time.Second)
	m := &cliServiceAccountTokens{lifetime: t.Context(), baseDir: t.TempDir()}
	tokens.PrependReactor("create", "serviceaccounts", func(a kubetesting.Action) (bool, runtime.Object, error) {
		pc := serviceAccountConfig(a.GetNamespace())
		exp := now.Add(time.Hour)
		return true, &authenticationv1.TokenRequest{Status: authenticationv1.TokenRequestStatus{Token: tokenForAccount(t, types.NamespacedName{Namespace: pc.namespace, Name: "oci-access"}, types.UID("sa-"+pc.namespace), exp), ExpirationTimestamp: metav1.NewTime(exp)}}, nil
	})
	paths := []string{}
	for _, tenant := range []string{"tenant-a", "tenant-b"} {
		_, a, err := s.configuration(t.Context(), serviceAccountConfig(tenant))
		if err != nil {
			t.Fatal(err)
		}
		p := a.options.OKEWorkloadIdentityTokenProvider.(*serviceAccountTokenProvider)
		p.now = func() time.Time { return now }
		p.jitter = func() float64 { return 0 }
		path, err := m.ensure("resource-"+tenant, a.identity, p)
		if err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
		for _, item := range []struct {
			path string
			mode os.FileMode
		}{{path, 0600}, {filepath.Dir(path), 0700}} {
			info, err := os.Stat(item.path)
			if err != nil || info.Mode().Perm() != item.mode {
				t.Fatalf("unsafe permissions %v %v", info, err)
			}
		}
	}
	first, _ := os.ReadFile(paths[0])
	other, _ := os.ReadFile(paths[1])
	if string(first) == string(other) {
		t.Fatal("tenants share a token")
	}
	now = now.Add(45 * time.Minute)
	m.mu.Lock()
	e := m.entries[paths[0]]
	m.mu.Unlock()
	if err := e.refresh(); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(paths[0])
	if string(first) == string(second) {
		t.Fatal("JWT file was not rotated")
	}
	tokens.PrependReactor("create", "serviceaccounts", func(kubetesting.Action) (bool, runtime.Object, error) { return true, nil, context.DeadlineExceeded })
	now = now.Add(time.Hour)
	if err := e.refresh(); err == nil {
		t.Fatal("expected refresh failure")
	}
	if _, err := os.Stat(paths[0]); !os.IsNotExist(err) {
		t.Fatal("failed refresh left a usable token file")
	}
}

func TestCLITokenWorkspaceRejectsPathTraversalAndSymlink(t *testing.T) {
	m := &cliServiceAccountTokens{lifetime: t.Context(), baseDir: t.TempDir()}
	for _, uid := range []string{"", "..", "../escape"} {
		if _, err := m.ensure(uid, "identity", nil); err == nil {
			t.Fatal("accepted unsafe UID")
		}
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(m.baseDir, "symlink")); err != nil {
		t.Fatal(err)
	}
	if _, err := m.ensure("symlink", "identity", nil); err == nil {
		t.Fatal("accepted symlink workspace")
	}
}

func TestServiceAccountCLISetupKeepsJWTOutOfConfiguration(t *testing.T) {
	s, tokens, kube := serviceAccountFixture(t)
	if err := namespacedv1beta1.SchemeBuilder.AddToScheme(kube.Scheme()); err != nil {
		t.Fatal(err)
	}
	pc := &namespacedv1beta1.ProviderConfig{ObjectMeta: metav1.ObjectMeta{Namespace: "tenant-a", Name: "tenant-oci", UID: "pc-a"}, Spec: *serviceAccountConfig("tenant-a").spec}
	if err := kube.Create(t.Context(), pc); err != nil {
		t.Fatal(err)
	}
	uid := types.UID("cnpm2805-" + rand.Text())
	defer os.RemoveAll(filepath.Join(os.TempDir(), string(uid)))
	mg := &fake.ModernManaged{ObjectMeta: metav1.ObjectMeta{Namespace: "tenant-a", Name: "bucket", UID: uid}, TypedProviderConfigReferencer: fake.TypedProviderConfigReferencer{Ref: &xpv1.ProviderConfigReference{Name: pc.Name, Kind: "ProviderConfig"}}}
	tokens.PrependReactor("create", "serviceaccounts", func(kubetesting.Action) (bool, runtime.Object, error) {
		exp := time.Now().Add(time.Hour)
		return true, &authenticationv1.TokenRequest{Status: authenticationv1.TokenRequestStatus{Token: tokenForAccount(t, types.NamespacedName{Namespace: "tenant-a", Name: "oci-access"}, "sa-tenant-a", exp), ExpirationTimestamp: metav1.NewTime(exp)}}, nil
	})
	lifetime, cancel := context.WithCancel(t.Context())
	defer cancel()
	setup := TerraformSetupBuilder(WithServiceAccountCredentials(lifetime, s.reader, s.tokens), WithTerraformCLI("1.4.6", "oracle/oci", "9.0.0"))
	configured, err := setup(t.Context(), kube, mg)
	if err != nil {
		t.Fatal(err)
	}
	path := configured.Configuration[credentialKeyOKETokenPath].(string)
	token, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(configured.Configuration)
	if strings.Contains(string(data), string(token)) || len(configured.Configuration) != 3 || configured.Meta != nil || configured.FrameworkProvider != nil || configured.Requirement.Source != "oracle/oci" {
		t.Fatalf("invalid CLI configuration: %s", data)
	}
	cancel()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("shutdown left token file")
		}
		time.Sleep(time.Millisecond)
	}
}
