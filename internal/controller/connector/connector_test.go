/*
Copyright 2026 Oracle and/or its affiliates.
*/

package connector

import (
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"

	"github.com/crossplane/crossplane-runtime/v2/pkg/logging"
	resourcefake "github.com/crossplane/crossplane-runtime/v2/pkg/resource/fake"
	"github.com/crossplane/upjet/v2/pkg/terraform"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
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
	c := NewCLIWorkspaceFinalizer(fake.NewClientBuilder().Build(), terraform.NewWorkspaceStore(logging.NewNopLogger())).(*terraform.WorkspaceFinalizer).Store
	if err := c.Remove(&resourcefake.ModernManaged{ObjectMeta: metav1.ObjectMeta{UID: uid}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("orphaned token workspace")
	}
}
