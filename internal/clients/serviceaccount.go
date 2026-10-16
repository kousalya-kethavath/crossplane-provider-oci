/*
Copyright 2026 Oracle and/or its affiliates.
*/

package clients

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"sync"
	"time"

	"github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	ociprovider "github.com/oracle/terraform-provider-oci/oci"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	coreclient "k8s.io/client-go/kubernetes/typed/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	namespacedv1beta1 "github.com/oracle/provider-oci/apis/namespaced/v1beta1"
)

const serviceAccountRequestTimeout = 10 * time.Second

type serviceAccountCredentials struct {
	lifetime context.Context
	reader   client.Reader
	tokens   coreclient.CoreV1Interface
}

// WithServiceAccountCredentials enables namespace-local TokenRequests using an
// uncached reader and the provider's lifecycle context. No additional permissions
// are exercised until a ProviderConfig opts in to ServiceAccount credentials.
func WithServiceAccountCredentials(lifetime context.Context, reader client.Reader, tokens coreclient.CoreV1Interface) SetupOption {
	return func(o *setupOptions) {
		o.serviceAccounts = serviceAccountCredentials{lifetime: lifetime, reader: reader, tokens: tokens}
	}
}

type providerAuthentication struct {
	options  ociprovider.InProcessOptions
	identity string
}

func (o setupOptions) configuration(ctx context.Context, kube client.Client, pc resolvedProviderConfig) (map[string]any, providerAuthentication, error) {
	if pc.spec.Credentials.Source == namespacedv1beta1.CredentialsSourceServiceAccount {
		return o.serviceAccounts.configuration(ctx, pc)
	}
	if pc.spec.Credentials.ServiceAccountRef != nil || pc.spec.Region != "" {
		return nil, providerAuthentication{}, fmt.Errorf("serviceAccountRef and spec.region are only supported with ServiceAccount credentials")
	}
	data, err := resource.CommonCredentialExtractor(ctx, pc.spec.Credentials.Source, kube, pc.spec.Credentials.CommonCredentialSelectors)
	if err != nil {
		return nil, providerAuthentication{}, fmt.Errorf("%s: %w", errExtractCredentials, err)
	}
	ociCreds := map[string]string{}
	if err := json.Unmarshal(data, &ociCreds); err != nil {
		return nil, providerAuthentication{}, fmt.Errorf("%s: %w", errUnmarshalCredentials, err)
	}
	return providerConfigurationFromCredentials(ociCreds), providerAuthentication{}, nil
}

func validateServiceAccountCredentials(pc resolvedProviderConfig) error {
	if pc.namespace == "" {
		return fmt.Errorf("ServiceAccount credentials require a namespaced ProviderConfig")
	}
	if pc.uid == "" {
		return fmt.Errorf("ProviderConfig has empty UID")
	}
	c := pc.spec.Credentials
	if c.ServiceAccountRef == nil || len(validation.IsDNS1123Subdomain(c.ServiceAccountRef.Name)) != 0 {
		return fmt.Errorf("ServiceAccount credentials require a valid name-only serviceAccountRef")
	}
	if c.SecretRef != nil || c.Env != nil || c.Fs != nil {
		return fmt.Errorf("ServiceAccount credentials cannot include secretRef, env or fs")
	}
	if len(validation.IsDNS1123Label(pc.spec.Region)) != 0 {
		return fmt.Errorf("ServiceAccount credentials require a valid OCI spec.region")
	}
	return nil
}

func (s serviceAccountCredentials) configuration(ctx context.Context, pc resolvedProviderConfig) (map[string]any, providerAuthentication, error) {
	if err := validateServiceAccountCredentials(pc); err != nil {
		return nil, providerAuthentication{}, err
	}
	if s.lifetime == nil || s.reader == nil || s.tokens == nil {
		return nil, providerAuthentication{}, fmt.Errorf("ServiceAccount token client is not configured")
	}
	key := types.NamespacedName{Namespace: pc.namespace, Name: pc.spec.Credentials.ServiceAccountRef.Name}
	sa := &corev1.ServiceAccount{}
	readCtx, cancel := context.WithTimeout(ctx, serviceAccountRequestTimeout)
	defer cancel()
	if err := s.reader.Get(readCtx, key, sa); err != nil {
		return nil, providerAuthentication{}, serviceAccountAPIError("read", key, err)
	}
	if sa.UID == "" || sa.DeletionTimestamp != nil {
		return nil, providerAuthentication{}, fmt.Errorf("ServiceAccount %s is missing a UID or is being deleted", key)
	}
	p := &serviceAccountTokenProvider{
		credentials: s, key: key, uid: sa.UID, now: time.Now,
		jitter: func() float64 { return rand.Float64() },
	}
	identity, err := providerConfigurationHash(map[string]any{
		"providerConfigUID": pc.uid, "namespace": key.Namespace, "serviceAccount": key.Name, "serviceAccountUID": string(sa.UID),
	})
	if err != nil {
		return nil, providerAuthentication{}, err
	}
	return map[string]any{credentialKeyAuth: "OKEWorkloadIdentity", credentialKeyRegion: pc.spec.Region},
		providerAuthentication{identity: identity, options: ociprovider.InProcessOptions{OKEWorkloadIdentityTokenProvider: p}}, nil
}

// serviceAccountTokenProvider is retained by cached OCI clients. Refreshes are
// lazy and serialized per identity; raw JWTs never enter Terraform configuration.
type serviceAccountTokenProvider struct {
	credentials serviceAccountCredentials
	key         types.NamespacedName
	uid         types.UID
	now         func() time.Time
	jitter      func() float64
	mu          sync.Mutex
	token       string
	refreshAt   time.Time
	expiresAt   time.Time
	nextAttempt time.Time
	backoff     time.Duration
	lastError   error
}

func (p *serviceAccountTokenProvider) ServiceAccountToken() (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.credentials.lifetime.Err(); err != nil {
		return "", fmt.Errorf("ServiceAccount token provider is shutting down")
	}
	now := p.now()
	if p.token != "" && now.Before(p.refreshAt) && now.Before(p.expiresAt) {
		return p.token, nil
	}
	if now.Before(p.nextAttempt) {
		return "", p.lastError
	}
	token, expiry, err := p.requestToken()
	if err != nil {
		// Fail closed, including when a refresh fails while the previous token
		// remains valid. Bound repeated failures without retaining API bodies.
		p.backoff = min(max(p.backoff*2, time.Second), 30*time.Second)
		p.nextAttempt = p.now().Add(p.backoff)
		p.lastError = err
		return "", err
	}
	now = p.now()
	lifetime := expiry.Sub(now)
	p.token, p.expiresAt = token, expiry
	// Refresh between 70 and 80 percent of the granted lifetime. OCI renewal
	// may ask later; requestToken then obtains a new JWT before the exchange.
	p.refreshAt = now.Add(time.Duration(float64(lifetime) * (0.7 + 0.1*p.jitter())))
	p.nextAttempt, p.backoff, p.lastError = time.Time{}, 0, nil
	return token, nil
}

func (p *serviceAccountTokenProvider) requestToken() (string, time.Time, error) {
	ctx, cancel := context.WithTimeout(p.credentials.lifetime, serviceAccountRequestTimeout)
	defer cancel()
	sa := &corev1.ServiceAccount{}
	if err := p.credentials.reader.Get(ctx, p.key, sa); err != nil {
		return "", time.Time{}, serviceAccountAPIError("read", p.key, err)
	}
	if sa.UID != p.uid || sa.DeletionTimestamp != nil {
		return "", time.Time{}, fmt.Errorf("ServiceAccount %s was replaced or is being deleted; resolve its ProviderConfig again", p.key)
	}
	// Match the API-server audience used by the existing OKE CCM flow. Tokens
	// are requested for the tenant account, not bound to the provider's Pod.
	request := &authenticationv1.TokenRequest{Spec: authenticationv1.TokenRequestSpec{ExpirationSeconds: new(int64(3600))}}
	response, err := p.credentials.tokens.ServiceAccounts(p.key.Namespace).CreateToken(ctx, p.key.Name, request, metav1.CreateOptions{})
	if err != nil {
		return "", time.Time{}, serviceAccountAPIError("request token for", p.key, err)
	}
	if response == nil {
		return "", time.Time{}, fmt.Errorf("ServiceAccount token request returned an empty response")
	}
	jwtExpiry, err := p.validateTokenIdentity(response.Status.Token)
	if err != nil {
		return "", time.Time{}, err
	}
	expiry := response.Status.ExpirationTimestamp.Time
	if expiry.IsZero() {
		return "", time.Time{}, fmt.Errorf("ServiceAccount token response is missing its expiration")
	}
	if jwtExpiry.Before(expiry) {
		expiry = jwtExpiry
	}
	if expiry.Sub(p.now()) < time.Minute {
		return "", time.Time{}, fmt.Errorf("ServiceAccount token lifetime is too short")
	}
	return response.Status.Token, expiry, nil
}

// TLS and Kubernetes authenticate the TokenRequest response. These claim checks
// bind it to the selected UID across a deletion/recreation race; they are not a
// substitute for OCI's signature and audience verification.
func (p *serviceAccountTokenProvider) validateTokenIdentity(token string) (time.Time, error) {
	invalid := fmt.Errorf("ServiceAccount token response has invalid identity or expiration claims")
	if len(token) > 128*1024 {
		return time.Time{}, invalid
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Time{}, invalid
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}, invalid
	}
	var claims struct {
		Subject    string `json:"sub"`
		Expiry     int64  `json:"exp"`
		Kubernetes struct {
			Namespace      string `json:"namespace"`
			ServiceAccount struct {
				Name string    `json:"name"`
				UID  types.UID `json:"uid"`
			} `json:"serviceaccount"`
		} `json:"kubernetes.io"`
	}
	if json.Unmarshal(data, &claims) != nil || claims.Expiry <= 0 ||
		claims.Subject != "system:serviceaccount:"+p.key.Namespace+":"+p.key.Name ||
		claims.Kubernetes.Namespace != p.key.Namespace || claims.Kubernetes.ServiceAccount.Name != p.key.Name || claims.Kubernetes.ServiceAccount.UID != p.uid {
		return time.Time{}, invalid
	}
	return time.Unix(claims.Expiry, 0), nil
}

func serviceAccountAPIError(action string, key types.NamespacedName, err error) error {
	reason := "API request failed"
	switch {
	case apierrors.IsForbidden(err):
		reason = "access denied; check namespace-local ServiceAccount RBAC"
	case apierrors.IsNotFound(err):
		reason = "not found"
	case apierrors.IsTooManyRequests(err):
		reason = "API request throttled"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		reason = "API request canceled or timed out"
	}
	return fmt.Errorf("cannot %s ServiceAccount %s: %s", action, key, reason)
}
