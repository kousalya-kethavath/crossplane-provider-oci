/*
Copyright 2026 Oracle and/or its affiliates.
*/

package connector

import (
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"

	"github.com/crossplane/crossplane-runtime/v2/pkg/feature"
	"github.com/crossplane/crossplane-runtime/v2/pkg/logging"
	resourcefake "github.com/crossplane/crossplane-runtime/v2/pkg/resource/fake"
	"github.com/crossplane/upjet/v2/pkg/config"
	tjcontroller "github.com/crossplane/upjet/v2/pkg/controller"
	"github.com/crossplane/upjet/v2/pkg/terraform"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestCLIFinalizerCleansUnregisteredTokenWorkspace(t *testing.T) {
	uid := types.UID("cnpm2805-" + rand.Text())
	dir := filepath.Join(os.TempDir(), string(uid))
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	if err := os.WriteFile(filepath.Join(dir, "token"), []byte("JWT"), 0600); err != nil {
		t.Fatal(err)
	}
	c := cliWorkspaceCleaner{terraform.NewWorkspaceStore(logging.NewNopLogger())}
	if err := c.Remove(&resourcefake.ModernManaged{ObjectMeta: metav1.ObjectMeta{UID: uid}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("orphaned token workspace")
	}
}

type testManager struct {
	ctrl.Manager
	kube client.Client
}

func (m testManager) GetClient() client.Client { return m.kube }

func TestExecutionModeSelectsMatchingConnectorAndFinalizer(t *testing.T) {
	mgr := testManager{kube: fake.NewClientBuilder().Build()}
	for _, cli := range []bool{false, true} {
		o := tjcontroller.Options{}
		o.Features = &feature.Flags{}
		o.Logger = logging.NewNopLogger()
		if cli {
			o.WorkspaceStore = terraform.NewWorkspaceStore(o.Logger)
		}
		c := NewAsyncConnector(mgr, o, &config.Resource{}, nil, nil, nil)
		f := NewFinalizer(mgr.GetClient(), o)
		if cli {
			if _, ok := c.(*tjcontroller.Connector); !ok {
				t.Fatalf("CLI connector: %T", c)
			}
			if _, ok := f.(*terraform.WorkspaceFinalizer); !ok {
				t.Fatalf("CLI finalizer: %T", f)
			}
		} else {
			if _, ok := c.(*tjcontroller.TerraformPluginSDKAsyncConnector); !ok {
				t.Fatalf("embedded connector: %T", c)
			}
			if _, ok := f.(*tjcontroller.OperationTrackerFinalizer); !ok {
				t.Fatalf("embedded finalizer: %T", f)
			}
		}
	}
}
