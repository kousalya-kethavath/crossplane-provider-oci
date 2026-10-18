/*
Copyright 2021 Upbound Inc.
*/

package clients

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	xpv1 "github.com/crossplane/crossplane-runtime/v2/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/v2/pkg/meta"
	"github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	ujconfig "github.com/crossplane/upjet/v2/pkg/config"
	upjetterraform "github.com/crossplane/upjet/v2/pkg/terraform"
	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clusterv1beta1 "github.com/oracle/provider-oci/apis/cluster/v1beta1"
	namespacedv1beta1 "github.com/oracle/provider-oci/apis/namespaced/v1beta1"
)

const (
	// error messages
	errNoProviderConfig           = "no providerConfigRef provided"
	errGetProviderConfig          = "cannot get referenced ProviderConfig"
	errTrackUsage                 = "cannot track ProviderConfig usage"
	errExtractCredentials         = "cannot extract credentials"
	errUnmarshalCredentials       = "cannot unmarshal oci credentials as JSON"
	errUnsupportedManaged         = "resource is not a managed"
	errUnsupportedProviderCfgKind = "unsupported providerConfigRef.kind"
)

const (
	credentialKeyTenancyOCID                     = "tenancy_ocid"
	credentialKeyUserOCID                        = "user_ocid"
	credentialKeyPrivateKey                      = "private_key"
	credentialKeyPrivateKeyPassword              = "private_key_password"
	credentialKeyPrivateKeyPath                  = "private_key_path"
	credentialKeyFingerprint                     = "fingerprint"
	credentialKeyRegion                          = "region"
	credentialKeyOKETokenPath                    = "oke_workload_identity_token_path"
	credentialKeyAuth                            = "auth"
	credentialKeyConfigFileProfile               = "config_file_profile"
	credentialKeyWorkloadIdentityTokenPath       = "workload_identity_token_path"
	credentialKeyTokenExchangeDomainURL          = "token_exchange_domain_url"
	credentialKeyTokenExchangeAuth               = "token_exchange_auth"
	credentialKeyTokenExchangeClientID           = "token_exchange_client_id"
	credentialKeyTokenExchangeClientSecret       = "token_exchange_client_secret"
	credentialKeyTokenExchangeRequestedTokenType = "token_exchange_requested_token_type"
	credentialKeyTokenExchangeSubjectTokenType   = "token_exchange_subject_token_type"
	credentialKeyTokenExchangeResourceType       = "token_exchange_resource_type"
	credentialKeyTokenExchangeRPSTExpiration     = "token_exchange_rpst_exp"
	credentialKeyTokenExchangePublicKey          = "token_exchange_public_key"
)

type setupOptions struct {
	isFrameworkResource   func(string) bool
	resources             map[string]*ujconfig.Resource
	isSDKv2Resource       func(string) bool
	providerMetaCacheSize int
	serviceAccounts       serviceAccountCredentials
	cli                   *upjetterraform.Setup
}

// WithTerraformCLIConfiguration supplies requirements for CLI-routed resources.
// It does not change any resource's execution path.
func WithTerraformCLIConfiguration(version, source, providerVersion string) SetupOption {
	return func(o *setupOptions) {
		o.cli = &upjetterraform.Setup{Version: version, Requirement: upjetterraform.ProviderRequirement{Source: source, Version: providerVersion}}
	}
}

// SetupOption customizes Terraform setup behavior.
type SetupOption func(*setupOptions)

// WithResourceConfiguration selects authentication transport from the same
// resource configuration that selects the generated Upjet connectors.
func WithResourceConfiguration(resources map[string]*ujconfig.Resource) SetupOption {
	return func(o *setupOptions) {
		o.resources = resources
		o.isSDKv2Resource = func(name string) bool {
			return resources[name] != nil && resources[name].ShouldUseTerraformPluginSDKClient()
		}
		o.isFrameworkResource = func(name string) bool {
			return resources[name] != nil && resources[name].ShouldUseTerraformPluginFrameworkClient()
		}
	}
}

// WithSDKv2ResourcePredicate controls which Terraform resources receive
// in-process SDKv2 provider meta.
func WithSDKv2ResourcePredicate(predicate func(string) bool) SetupOption {
	return func(o *setupOptions) {
		o.isSDKv2Resource = predicate
	}
}

// WithProviderMetaCacheSize controls the maximum number of configured SDKv2
// provider instances retained by a service process.
func WithProviderMetaCacheSize(size int) SetupOption {
	return func(o *setupOptions) {
		o.providerMetaCacheSize = size
	}
}

func newSetupOptions(opts ...SetupOption) setupOptions {
	options := setupOptions{providerMetaCacheSize: defaultProviderMetaCacheSize}
	for _, opt := range opts {
		opt(&options)
	}
	return options
}

type terraformResourceTyper interface {
	GetTerraformResourceType() string
}

// TerraformSetupBuilder builds a terraform.SetupFn for per-resource SDKv2,
// Framework and CLI connectors. CLI requirements are only used for CLI resources.
// Construct it at startup: it suppresses the process-wide standard Go logger
// before embedded provider operations can log sensitive payloads.
func TerraformSetupBuilder(opts ...SetupOption) upjetterraform.SetupFn {
	configureEmbeddedTerraformLogging()

	options := newSetupOptions(opts...)
	providerMetaCache := newProviderMetaCache(options.providerMetaCacheSize)
	cliTokens := &cliServiceAccountTokens{lifetime: options.serviceAccounts.lifetime, baseDir: os.TempDir()}

	return func(ctx context.Context, kube client.Client, mg resource.Managed) (upjetterraform.Setup, error) {
		ps := upjetterraform.Setup{}

		if options.resources != nil {
			tr, ok := mg.(terraformResourceTyper)
			if !ok || options.resources[tr.GetTerraformResourceType()] == nil {
				return ps, fmt.Errorf("managed resource has no configured Terraform execution route")
			}
		}
		useSDKv2 := options.shouldConfigureSDKv2Provider(mg)
		useFramework := options.shouldConfigureFrameworkProvider(mg)
		if useSDKv2 && useFramework {
			return ps, fmt.Errorf("managed resource has conflicting Terraform execution routes")
		}
		if !useSDKv2 && !useFramework && options.cli == nil {
			return ps, fmt.Errorf("Terraform CLI requirements are missing for a CLI-routed resource")
		}

		pc, err := resolveProviderConfig(ctx, kube, mg)
		if err != nil {
			return ps, errors.Wrap(err, "cannot resolve provider config")
		}

		cfg, authentication, err := options.configuration(ctx, kube, pc)
		if err != nil {
			return ps, err
		}
		if !useSDKv2 && !useFramework {
			ps.Version, ps.Requirement = options.cli.Version, options.cli.Requirement
			if authentication.identity != "" {
				supplier, ok := authentication.options.OKEWorkloadIdentityTokenProvider.(*serviceAccountTokenProvider)
				if !ok {
					return ps, fmt.Errorf("invalid ServiceAccount token supplier for Terraform CLI")
				}
				path, err := cliTokens.ensure(string(mg.GetUID()), authentication.identity, supplier)
				if err != nil {
					return ps, err
				}
				cfg[credentialKeyOKETokenPath] = path
			}
			ps.Configuration = cfg
			return ps, nil
		}
		ps.Configuration = cfg

		if useFramework {
			setFrameworkProvider(&ps, authentication)
		}

		if !useSDKv2 {
			return ps, nil
		}

		if pc.uid == "" {
			return ps, fmt.Errorf("ProviderConfig has empty UID")
		}

		providerMeta, err := providerMetaCache.getOrConfigureProviderMeta(ctx, pc.uid, cfg, authentication)
		if err != nil {
			return ps, fmt.Errorf("cannot get or init OCI provider: %w", err)
		}
		ps.Meta = providerMeta
		ps.Scheduler = upjetterraform.NewNoOpProviderScheduler()

		return ps, nil
	}
}

func providerConfigurationFromCredentials(ociCreds map[string]string) map[string]any {
	config := map[string]any{
		credentialKeyTenancyOCID:                     ociCreds[credentialKeyTenancyOCID],
		credentialKeyUserOCID:                        ociCreds[credentialKeyUserOCID],
		credentialKeyPrivateKey:                      ociCreds[credentialKeyPrivateKey],
		credentialKeyPrivateKeyPassword:              ociCreds[credentialKeyPrivateKeyPassword],
		credentialKeyPrivateKeyPath:                  ociCreds[credentialKeyPrivateKeyPath],
		credentialKeyFingerprint:                     ociCreds[credentialKeyFingerprint],
		credentialKeyRegion:                          ociCreds[credentialKeyRegion],
		credentialKeyAuth:                            ociCreds[credentialKeyAuth],
		credentialKeyConfigFileProfile:               ociCreds[credentialKeyConfigFileProfile],
		credentialKeyWorkloadIdentityTokenPath:       ociCreds[credentialKeyWorkloadIdentityTokenPath],
		credentialKeyTokenExchangeDomainURL:          ociCreds[credentialKeyTokenExchangeDomainURL],
		credentialKeyTokenExchangeAuth:               ociCreds[credentialKeyTokenExchangeAuth],
		credentialKeyTokenExchangeClientID:           ociCreds[credentialKeyTokenExchangeClientID],
		credentialKeyTokenExchangeClientSecret:       ociCreds[credentialKeyTokenExchangeClientSecret],
		credentialKeyTokenExchangeRequestedTokenType: ociCreds[credentialKeyTokenExchangeRequestedTokenType],
		credentialKeyTokenExchangeSubjectTokenType:   ociCreds[credentialKeyTokenExchangeSubjectTokenType],
		credentialKeyTokenExchangeResourceType:       ociCreds[credentialKeyTokenExchangeResourceType],
		credentialKeyTokenExchangeRPSTExpiration:     ociCreds[credentialKeyTokenExchangeRPSTExpiration],
		credentialKeyTokenExchangePublicKey:          ociCreds[credentialKeyTokenExchangePublicKey],
	}

	for key, value := range config {
		if value == "" {
			delete(config, key)
		}
	}

	return config
}

func (o setupOptions) shouldConfigureSDKv2Provider(mg resource.Managed) bool {
	if o.isSDKv2Resource == nil {
		return false
	}
	tr, ok := mg.(terraformResourceTyper)
	if !ok {
		return false
	}
	return o.isSDKv2Resource(tr.GetTerraformResourceType())
}

func (o setupOptions) shouldConfigureFrameworkProvider(mg resource.Managed) bool {
	tr, ok := mg.(terraformResourceTyper)
	return ok && o.isFrameworkResource != nil && o.isFrameworkResource(tr.GetTerraformResourceType())
}

func providerConfigurationHash(cfg map[string]any) (string, error) {
	data, err := json.Marshal(cfg)
	if err != nil {
		return "", fmt.Errorf("cannot hash OCI provider configuration: %w", err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(data)), nil
}

const defaultProviderMetaCacheSize = 32

type providerMetaCacheEntry struct {
	configHash string
	meta       any
	recency    *list.Element
}

type providerMetaCacheCall struct {
	configHash string
	meta       any
	err        error
	done       chan struct{}
}

type providerMetaCache struct {
	mu         sync.Mutex
	maxEntries int
	entries    map[string]*providerMetaCacheEntry
	recency    *list.List
	inflight   map[string]*providerMetaCacheCall
	metrics    *providerMetaCacheMetrics
}

func newProviderMetaCache(maxEntries int) *providerMetaCache {
	return newProviderMetaCacheWithMetrics(maxEntries, defaultProviderMetaCacheMetrics)
}

func newProviderMetaCacheWithMetrics(maxEntries int, metrics *providerMetaCacheMetrics) *providerMetaCache {
	if maxEntries < 1 {
		maxEntries = 1
	}
	metrics.capacity.Set(float64(maxEntries))
	metrics.entries.Set(0)
	metrics.inflight.Set(0)
	metrics.waiting.Set(0)
	return &providerMetaCache{
		maxEntries: maxEntries,
		entries:    make(map[string]*providerMetaCacheEntry, maxEntries),
		recency:    list.New(),
		inflight:   make(map[string]*providerMetaCacheCall),
		metrics:    metrics,
	}
}

func (c *providerMetaCache) getOrCreate(ctx context.Context, uid, configHash string, create func() (any, error)) (any, error) {
	for {
		c.mu.Lock()
		if entry, ok := c.entries[uid]; ok && entry.configHash == configHash {
			c.recency.MoveToFront(entry.recency)
			c.metrics.hits.Inc()
			meta := entry.meta
			c.mu.Unlock()
			return meta, nil
		}

		if call, ok := c.inflight[uid]; ok {
			sameConfiguration := call.configHash == configHash
			c.mu.Unlock()
			c.metrics.waiting.Inc()
			select {
			case <-call.done:
			case <-ctx.Done():
				c.metrics.waiting.Dec()
				c.metrics.waits.WithLabelValues("canceled").Inc()
				return nil, ctx.Err()
			}
			c.metrics.waiting.Dec()
			if sameConfiguration && call.err != nil {
				c.metrics.waits.WithLabelValues("initialization_error").Inc()
				return nil, call.err
			}
			result := "completed"
			if !sameConfiguration {
				result = "configuration_changed"
			}
			c.metrics.waits.WithLabelValues(result).Inc()
			continue
		}

		missReason := "absent"
		if _, ok := c.entries[uid]; ok {
			missReason = "configuration_changed"
		}
		c.metrics.misses.WithLabelValues(missReason).Inc()
		call := &providerMetaCacheCall{configHash: configHash, done: make(chan struct{})}
		c.inflight[uid] = call
		c.metrics.inflight.Set(float64(len(c.inflight)))
		c.mu.Unlock()

		start := time.Now()
		call.meta, call.err = initializeProviderMeta(create)
		result := "success"
		if call.err != nil {
			result = "error"
		}
		c.metrics.initializations.WithLabelValues(result).Inc()
		c.metrics.initializationDuration.WithLabelValues(result).Observe(time.Since(start).Seconds())

		c.mu.Lock()
		if call.err == nil {
			if entry, ok := c.entries[uid]; ok {
				entry.configHash = configHash
				entry.meta = call.meta
				c.recency.MoveToFront(entry.recency)
			} else {
				c.entries[uid] = &providerMetaCacheEntry{
					configHash: configHash,
					meta:       call.meta,
					recency:    c.recency.PushFront(uid),
				}
			}
			c.evictOverflow()
			c.metrics.entries.Set(float64(len(c.entries)))
		}
		delete(c.inflight, uid)
		c.metrics.inflight.Set(float64(len(c.inflight)))
		close(call.done)
		c.mu.Unlock()
		return call.meta, call.err
	}
}

func initializeProviderMeta(create func() (any, error)) (meta any, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			meta = nil
			err = fmt.Errorf("provider metadata initialization panicked: %v", recovered)
		}
	}()
	return create()
}

func (c *providerMetaCache) evictOverflow() {
	for len(c.entries) > c.maxEntries {
		oldest := c.recency.Back()
		if oldest == nil {
			return
		}
		delete(c.entries, oldest.Value.(string))
		c.recency.Remove(oldest)
		c.metrics.evictions.Inc()
	}
}

type resolvedProviderConfig struct {
	spec      *namespacedv1beta1.ProviderConfigSpec
	uid       string
	namespace string
}

func resolveProviderConfig(ctx context.Context, kube client.Client, mg resource.Managed) (resolvedProviderConfig, error) {
	switch managed := mg.(type) {
	case resource.LegacyManaged:
		return resolveLegacyProviderConfig(ctx, kube, managed)
	case resource.ModernManaged:
		if isNamespacedModernManaged(managed) {
			return resolveNamespacedProviderConfig(ctx, kube, managed)
		}
		return resolveClusterProviderConfigForModernMR(ctx, kube, managed)
	default:
		return resolvedProviderConfig{}, errors.New(errUnsupportedManaged)
	}
}

func isNamespacedModernManaged(mg resource.ModernManaged) bool {
	if mg.GetNamespace() != "" {
		return true
	}

	group := mg.GetObjectKind().GroupVersionKind().Group
	return group == namespacedv1beta1.Group || strings.HasSuffix(group, "."+namespacedv1beta1.Group)
}

func resolveLegacyProviderConfig(ctx context.Context, kube client.Client, mg resource.LegacyManaged) (resolvedProviderConfig, error) {
	configRef := mg.GetProviderConfigReference()
	if configRef == nil {
		return resolvedProviderConfig{}, errors.New(errNoProviderConfig)
	}

	pc := &clusterv1beta1.ProviderConfig{}
	if err := kube.Get(ctx, types.NamespacedName{Name: configRef.Name}, pc); err != nil {
		return resolvedProviderConfig{}, errors.Wrap(err, errGetProviderConfig)
	}

	t := resource.NewLegacyProviderConfigUsageTracker(kube, &clusterv1beta1.ProviderConfigUsage{})
	if err := t.Track(ctx, mg); err != nil {
		return resolvedProviderConfig{}, errors.Wrap(err, errTrackUsage)
	}

	spec, err := toSharedPCSpec(pc.Spec)
	return resolvedProviderConfig{spec: spec, uid: string(pc.GetUID())}, err
}

func resolveClusterProviderConfigForModernMR(ctx context.Context, kube client.Client, mg resource.ModernManaged) (resolvedProviderConfig, error) {
	configRef := mg.GetProviderConfigReference()
	if configRef == nil {
		return resolvedProviderConfig{}, errors.New(errNoProviderConfig)
	}
	if configRef.Name == "" {
		return resolvedProviderConfig{}, errors.New(errNoProviderConfig)
	}

	kind := configRef.Kind
	if kind == "" {
		kind = clusterv1beta1.ProviderConfigGroupVersionKind.Kind
	}
	if kind != clusterv1beta1.ProviderConfigGroupVersionKind.Kind && kind != namespacedv1beta1.ClusterProviderConfigKind {
		return resolvedProviderConfig{}, errors.Wrap(errors.New(kind), errUnsupportedProviderCfgKind)
	}

	pc := &clusterv1beta1.ProviderConfig{}
	if err := kube.Get(ctx, types.NamespacedName{Name: configRef.Name}, pc); err != nil {
		return resolvedProviderConfig{}, errors.Wrap(err, errGetProviderConfig)
	}

	if err := trackLegacyProviderConfigUsageForModernMR(ctx, kube, mg, configRef.Name); err != nil {
		return resolvedProviderConfig{}, errors.Wrap(err, errTrackUsage)
	}

	spec, err := toSharedPCSpec(pc.Spec)
	return resolvedProviderConfig{spec: spec, uid: string(pc.GetUID())}, err
}

func resolveNamespacedProviderConfig(ctx context.Context, kube client.Client, mg resource.ModernManaged) (resolvedProviderConfig, error) {
	configRef := mg.GetProviderConfigReference()
	if configRef == nil {
		return resolvedProviderConfig{}, errors.New(errNoProviderConfig)
	}
	if configRef.Name == "" {
		return resolvedProviderConfig{}, errors.New(errNoProviderConfig)
	}

	kind := configRef.Kind
	if kind == "" {
		kind = namespacedv1beta1.ClusterProviderConfigKind
	}
	switch kind {
	case namespacedv1beta1.ProviderConfigKind, namespacedv1beta1.ClusterProviderConfigKind:
	default:
		return resolvedProviderConfig{}, errors.Wrap(errors.New(kind), errUnsupportedProviderCfgKind)
	}

	if configRef.Kind != kind {
		mg.SetProviderConfigReference(&xpv1.ProviderConfigReference{Name: configRef.Name, Kind: kind})
	}

	pcRuntimeObj, err := kube.Scheme().New(namespacedv1beta1.SchemeGroupVersion.WithKind(kind))
	if err != nil {
		return resolvedProviderConfig{}, errors.Wrap(err, errUnsupportedProviderCfgKind)
	}
	pcObj, ok := pcRuntimeObj.(client.Object)
	if !ok {
		return resolvedProviderConfig{}, errors.New(errUnsupportedProviderCfgKind)
	}

	key := types.NamespacedName{Name: configRef.Name}
	if kind == namespacedv1beta1.ProviderConfigKind {
		key.Namespace = mg.GetNamespace()
	}
	if err := kube.Get(ctx, key, pcObj); err != nil {
		return resolvedProviderConfig{}, errors.Wrap(err, errGetProviderConfig)
	}

	var pcSpec namespacedv1beta1.ProviderConfigSpec
	switch pc := pcObj.(type) {
	case *namespacedv1beta1.ProviderConfig:
		pcSpec = *pc.Spec.DeepCopy()
		if pcSpec.Credentials.SecretRef != nil {
			pcSpec.Credentials.SecretRef.Namespace = mg.GetNamespace()
		}
	case *namespacedv1beta1.ClusterProviderConfig:
		shared, err := toSharedPCSpec(pc.Spec)
		if err != nil {
			return resolvedProviderConfig{}, err
		}
		pcSpec = *shared
	default:
		return resolvedProviderConfig{}, errors.New(errUnsupportedProviderCfgKind)
	}

	t := resource.NewProviderConfigUsageTracker(kube, &namespacedv1beta1.ProviderConfigUsage{})
	if err := t.Track(ctx, mg); err != nil {
		return resolvedProviderConfig{}, errors.Wrap(err, errTrackUsage)
	}

	return resolvedProviderConfig{spec: &pcSpec, uid: string(pcObj.GetUID()), namespace: pcObj.GetNamespace()}, nil
}

func toSharedPCSpec(spec any) (*namespacedv1beta1.ProviderConfigSpec, error) {
	data, err := json.Marshal(spec)
	if err != nil {
		return nil, err
	}
	out := &namespacedv1beta1.ProviderConfigSpec{}
	if err := json.Unmarshal(data, out); err != nil {
		return nil, err
	}
	return out, nil
}

func trackLegacyProviderConfigUsageForModernMR(ctx context.Context, kube client.Client, mg resource.ModernManaged, providerConfigName string) error {
	pcu := &clusterv1beta1.ProviderConfigUsage{}
	gvk := mg.GetObjectKind().GroupVersionKind()

	pcu.SetName(string(mg.GetUID()))
	pcu.SetLabels(map[string]string{xpv1.LabelKeyProviderName: providerConfigName})
	pcu.SetOwnerReferences([]metav1.OwnerReference{meta.AsController(meta.TypedReferenceTo(mg, gvk))})
	pcu.SetProviderConfigReference(xpv1.Reference{Name: providerConfigName})
	pcu.SetResourceReference(xpv1.TypedReference{
		APIVersion: gvk.GroupVersion().String(),
		Kind:       gvk.Kind,
		Name:       mg.GetName(),
	})

	err := resource.NewAPIUpdatingApplicator(kube).Apply(ctx, pcu,
		resource.MustBeControllableBy(mg.GetUID()),
		resource.AllowUpdateIf(func(current, _ kruntime.Object) bool {
			return current.(*clusterv1beta1.ProviderConfigUsage).GetProviderConfigReference() != pcu.GetProviderConfigReference()
		}),
	)
	return errors.Wrap(resource.Ignore(resource.IsNotAllowed, err), errTrackUsage)
}
