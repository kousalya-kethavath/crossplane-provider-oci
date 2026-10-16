/*
Copyright 2026 Oracle and/or its affiliates.
*/

package clients

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	xpv1 "github.com/crossplane/crossplane-runtime/v2/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/v2/pkg/resource/fake"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	kubefake "k8s.io/client-go/kubernetes/fake"
	kubetesting "k8s.io/client-go/testing"
	"sigs.k8s.io/controller-runtime/pkg/client"
	clientfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	namespacedv1beta1 "github.com/oracle/provider-oci/apis/namespaced/v1beta1"
)

func tokenForAccount(t *testing.T, key types.NamespacedName, uid types.UID, expiry time.Time) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"exp": expiry.Unix(), "sub": "system:serviceaccount:" + key.Namespace + ":" + key.Name,
		"kubernetes.io": map[string]any{"namespace": key.Namespace, "serviceaccount": map[string]any{"name": key.Name, "uid": uid}}})
	if err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256"}`)) + "." + base64.RawURLEncoding.EncodeToString(payload) + ".test-signature"
}

func serviceAccountConfig(namespace string) resolvedProviderConfig {
	return resolvedProviderConfig{uid: "pc-" + namespace, namespace: namespace, spec: &namespacedv1beta1.ProviderConfigSpec{
		Region: "us-ashburn-1", Credentials: namespacedv1beta1.ProviderCredentials{Source: namespacedv1beta1.CredentialsSourceServiceAccount,
			ServiceAccountRef: &namespacedv1beta1.ServiceAccountReference{Name: "oci-access"}},
	}}
}

func serviceAccountFixture(t *testing.T) (serviceAccountCredentials, *kubefake.Clientset, client.Client) {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	objects := []client.Object{}
	for _, ns := range []string{"tenant-a", "tenant-b"} {
		objects = append(objects, &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "oci-access", UID: types.UID("sa-" + ns)}})
	}
	reader := clientfake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
	tokens := kubefake.NewClientset()
	return serviceAccountCredentials{lifetime: t.Context(), reader: reader, tokens: tokens.CoreV1()}, tokens, reader
}

func TestServiceAccountConfigurationIsolation(t *testing.T) {
	s, _, reader := serviceAccountFixture(t)
	var identities []string
	for _, namespace := range []string{"tenant-a", "tenant-b"} {
		cfg, authentication, err := s.configuration(t.Context(), serviceAccountConfig(namespace))
		if err != nil {
			t.Fatal(err)
		}
		if len(cfg) != 2 || cfg[credentialKeyAuth] != "OKEWorkloadIdentity" || cfg[credentialKeyRegion] != "us-ashburn-1" {
			t.Fatalf("configuration contains unexpected credentials: %v", cfg)
		}
		provider := authentication.options.OKEWorkloadIdentityTokenProvider.(*serviceAccountTokenProvider)
		if provider.key.Namespace != namespace || provider.uid != types.UID("sa-"+namespace) {
			t.Fatalf("wrong tenant identity: %s/%s", provider.key, provider.uid)
		}
		identities = append(identities, authentication.identity)
	}
	if identities[0] == identities[1] {
		t.Fatal("two tenants share an authentication cache identity")
	}
	sa := &corev1.ServiceAccount{}
	key := types.NamespacedName{Namespace: "tenant-a", Name: "oci-access"}
	if err := reader.Get(t.Context(), key, sa); err != nil {
		t.Fatal(err)
	}
	sa.UID = "recreated"
	if err := reader.Update(t.Context(), sa); err != nil {
		t.Fatal(err)
	}
	_, authentication, err := s.configuration(t.Context(), serviceAccountConfig("tenant-a"))
	if err != nil || authentication.identity == identities[0] {
		t.Fatalf("recreated account did not invalidate identity: %v", err)
	}
}

func TestServiceAccountCredentialValidation(t *testing.T) {
	tests := map[string]func(*resolvedProviderConfig){
		"cluster scope":        func(pc *resolvedProviderConfig) { pc.namespace = "" },
		"empty uid":            func(pc *resolvedProviderConfig) { pc.uid = "" },
		"missing reference":    func(pc *resolvedProviderConfig) { pc.spec.Credentials.ServiceAccountRef = nil },
		"namespace in name":    func(pc *resolvedProviderConfig) { pc.spec.Credentials.ServiceAccountRef.Name = "tenant-b/oci-access" },
		"empty region":         func(pc *resolvedProviderConfig) { pc.spec.Region = "" },
		"whitespace region":    func(pc *resolvedProviderConfig) { pc.spec.Region = " " },
		"secret selector":      func(pc *resolvedProviderConfig) { pc.spec.Credentials.SecretRef = &xpv1.SecretKeySelector{} },
		"filesystem selector":  func(pc *resolvedProviderConfig) { pc.spec.Credentials.Fs = &xpv1.FsSelector{} },
		"environment selector": func(pc *resolvedProviderConfig) { pc.spec.Credentials.Env = &xpv1.EnvSelector{} },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			pc := serviceAccountConfig("tenant-a")
			mutate(&pc)
			if err := validateServiceAccountCredentials(pc); err == nil {
				t.Fatal("accepted invalid configuration")
			}
		})
	}
	if err := validateServiceAccountCredentials(serviceAccountConfig("tenant-a")); err != nil {
		t.Fatal(err)
	}
}

func TestServiceAccountTokenRenewalCoalescesRequests(t *testing.T) {
	s, tokens, _ := serviceAccountFixture(t)
	_, authentication, err := s.configuration(t.Context(), serviceAccountConfig("tenant-a"))
	if err != nil {
		t.Fatal(err)
	}
	p := authentication.options.OKEWorkloadIdentityTokenProvider.(*serviceAccountTokenProvider)
	now := time.Now().Truncate(time.Second)
	p.now = func() time.Time { return now }
	p.jitter = func() float64 { return 0.5 }
	var requests atomic.Int64
	tokens.PrependReactor("create", "serviceaccounts", func(action kubetesting.Action) (bool, runtime.Object, error) {
		a := action.(kubetesting.CreateActionImpl)
		r := a.GetObject().(*authenticationv1.TokenRequest)
		if a.GetNamespace() != "tenant-a" || a.Name != "oci-access" || a.GetSubresource() != "token" || r.Spec.BoundObjectRef != nil || len(r.Spec.Audiences) != 0 || r.Spec.ExpirationSeconds == nil || *r.Spec.ExpirationSeconds != 3600 {
			t.Errorf("unexpected TokenRequest: %v %+v", a, r.Spec)
		}
		requests.Add(1)
		return true, &authenticationv1.TokenRequest{Status: authenticationv1.TokenRequestStatus{Token: tokenForAccount(t, p.key, p.uid, now.Add(time.Hour)), ExpirationTimestamp: metav1.NewTime(now.Add(45 * time.Minute))}}, nil
	})
	var wg sync.WaitGroup
	for range 24 {
		wg.Go(func() {
			if _, err := p.ServiceAccountToken(); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if requests.Load() != 1 || !p.expiresAt.Equal(now.Add(45*time.Minute)) {
		t.Fatalf("requests=%d expiry=%v", requests.Load(), p.expiresAt)
	}
	first, err := p.ServiceAccountToken()
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(34 * time.Minute)
	second, err := p.ServiceAccountToken()
	if err != nil {
		t.Fatal(err)
	}
	if first == second || requests.Load() != 2 {
		t.Fatal("token was not renewed before its server-granted expiration")
	}
	now = now.Add(2 * time.Hour)
	if _, err := p.ServiceAccountToken(); err != nil || requests.Load() != 3 {
		t.Fatalf("lazy renewal after expiry failed: %v", err)
	}
}

func TestServiceAccountTokenFailuresAreClosedAndSanitized(t *testing.T) {
	for _, kind := range []string{"forbidden", "malformed", "wrong uid", "missing expiry", "expired", "short lifetime", "recreated", "shutdown"} {
		t.Run(kind, func(t *testing.T) {
			s, tokens, reader := serviceAccountFixture(t)
			lifetime, cancel := context.WithCancel(t.Context())
			defer cancel()
			s.lifetime = lifetime
			_, authentication, err := s.configuration(t.Context(), serviceAccountConfig("tenant-a"))
			if err != nil {
				t.Fatal(err)
			}
			p := authentication.options.OKEWorkloadIdentityTokenProvider.(*serviceAccountTokenProvider)
			now := time.Now().Truncate(time.Second)
			p.now = func() time.Time { return now }
			p.jitter = func() float64 { return 0 }
			requests := 0
			tokens.PrependReactor("create", "serviceaccounts", func(action kubetesting.Action) (bool, runtime.Object, error) {
				requests++
				if kind == "forbidden" {
					return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "serviceaccounts/token"}, p.key.Name, fmt.Errorf("secretJWT"))
				}
				uid := p.uid
				if kind == "wrong uid" {
					uid = "other"
				}
				exp := now.Add(time.Hour)
				if kind == "expired" {
					exp = now.Add(-time.Minute)
				}
				if kind == "short lifetime" {
					exp = now.Add(10 * time.Second)
				}
				token := tokenForAccount(t, p.key, uid, exp)
				if kind == "malformed" {
					token = "secretJWT"
				}
				status := authenticationv1.TokenRequestStatus{Token: token, ExpirationTimestamp: metav1.NewTime(exp)}
				if kind == "missing expiry" {
					status.ExpirationTimestamp = metav1.Time{}
				}
				return true, &authenticationv1.TokenRequest{Status: status}, nil
			})
			if kind == "recreated" {
				sa := &corev1.ServiceAccount{}
				if err := reader.Get(t.Context(), p.key, sa); err != nil {
					t.Fatal(err)
				}
				sa.UID = "new"
				if err := reader.Update(t.Context(), sa); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "shutdown" {
				cancel()
			}
			for range 2 {
				token, err := p.ServiceAccountToken()
				if err == nil || token != "" || strings.Contains(err.Error(), "secretJWT") {
					t.Fatalf("unsafe token failure: %q %v", token, err)
				}
			}
			if requests > 1 {
				t.Fatalf("failed request was not backed off: %d", requests)
			}
		})
	}
}

func TestServiceAccountCannotBorrowAnotherNamespaceAccount(t *testing.T) {
	s, tokens, reader := serviceAccountFixture(t)
	if err := reader.Delete(t.Context(), &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Namespace: "tenant-a", Name: "oci-access"}}); err != nil {
		t.Fatal(err)
	}
	_, _, err := s.configuration(t.Context(), serviceAccountConfig("tenant-a"))
	if err == nil || !strings.Contains(err.Error(), "not found") || len(tokens.Actions()) != 0 {
		t.Fatalf("fell back to another namespace or pod identity: %v", err)
	}
}

func TestServiceAccountRefreshFailureDoesNotReuseCachedToken(t *testing.T) {
	s, tokens, _ := serviceAccountFixture(t)
	_, authentication, err := s.configuration(t.Context(), serviceAccountConfig("tenant-a"))
	if err != nil {
		t.Fatal(err)
	}
	p := authentication.options.OKEWorkloadIdentityTokenProvider.(*serviceAccountTokenProvider)
	now := time.Now().Truncate(time.Second)
	p.now = func() time.Time { return now }
	p.jitter = func() float64 { return 0 }
	requests := 0
	tokens.PrependReactor("create", "serviceaccounts", func(action kubetesting.Action) (bool, runtime.Object, error) {
		requests++
		if requests > 1 {
			return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "serviceaccounts/token"}, p.key.Name, fmt.Errorf("secretJWT"))
		}
		expiry := now.Add(time.Hour)
		return true, &authenticationv1.TokenRequest{Status: authenticationv1.TokenRequestStatus{Token: tokenForAccount(t, p.key, p.uid, expiry), ExpirationTimestamp: metav1.NewTime(expiry)}}, nil
	})
	if token, err := p.ServiceAccountToken(); err != nil || token == "" {
		t.Fatalf("initial token acquisition failed: %v", err)
	}
	now = now.Add(45 * time.Minute)
	for range 2 {
		if token, err := p.ServiceAccountToken(); err == nil || token != "" || strings.Contains(err.Error(), "secretJWT") {
			t.Fatalf("reused cached credentials or exposed API error: %q %v", token, err)
		}
	}
	if requests != 2 {
		t.Fatalf("failed refresh was not backed off: requests=%d", requests)
	}
}

func TestServiceAccountSetupRequiresInProcessRouting(t *testing.T) {
	s, _, kube := serviceAccountFixture(t)
	if err := namespacedv1beta1.SchemeBuilder.AddToScheme(kube.Scheme()); err != nil {
		t.Fatal(err)
	}
	pc := &namespacedv1beta1.ProviderConfig{ObjectMeta: metav1.ObjectMeta{Namespace: "tenant-a", Name: "tenant-oci", UID: "pc-a"}, Spec: *serviceAccountConfig("tenant-a").spec}
	if err := kube.Create(t.Context(), pc); err != nil {
		t.Fatal(err)
	}
	mg := &fake.ModernManaged{
		ObjectMeta:                    metav1.ObjectMeta{Namespace: "tenant-a", Name: "bucket", UID: "managed-a"},
		TypedProviderConfigReferencer: fake.TypedProviderConfigReferencer{Ref: &xpv1.ProviderConfigReference{Name: pc.Name, Kind: namespacedv1beta1.ProviderConfigKind}},
	}
	setup := TerraformSetupBuilder(WithServiceAccountCredentials(t.Context(), s.reader, s.tokens))
	if _, err := setup(t.Context(), kube, mg); err == nil || !strings.Contains(err.Error(), "require an in-process") {
		t.Fatalf("accepted unsupported routing: %v", err)
	}
	setup = TerraformSetupBuilder(WithServiceAccountCredentials(t.Context(), s.reader, s.tokens), WithFrameworkProvider(true))
	configured, err := setup(t.Context(), kube, mg)
	if err != nil || configured.FrameworkProvider == nil || len(configured.Configuration) != 2 {
		t.Fatalf("Framework setup did not retain tenant authentication: %v", err)
	}
}
