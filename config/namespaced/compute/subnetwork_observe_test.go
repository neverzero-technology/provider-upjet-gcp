// SPDX-FileCopyrightText: 2026 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: CC0-1.0

package compute_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/crossplane/crossplane-runtime/v2/pkg/logging"
	xpresource "github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	"github.com/crossplane/upjet/v2/pkg/controller"
	"github.com/crossplane/upjet/v2/pkg/metrics"
	"github.com/crossplane/upjet/v2/pkg/terraform"
	"github.com/hashicorp/terraform-provider-google/google/fwprovider"
	"github.com/hashicorp/terraform-provider-google/google/provider"
	"github.com/hashicorp/terraform-provider-google/google/registry"
	transporttpg "github.com/hashicorp/terraform-provider-google/google/transport"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/upbound/provider-gcp/v2/apis/namespaced/compute/v1beta1"
	"github.com/upbound/provider-gcp/v2/config"
)

// An existing Subnetwork observed with the full management policies must not
// panic. google_compute_subnetwork's CustomizeDiff reads
// ResourceDiff.GetRawState() for every existing resource; when upjet left
// RawState unset, cty panicked ("value is not an object") on every observe,
// controller-runtime recovered it silently, and the Subnetwork stayed in
// Creating forever after its create succeeded.
func TestObserveExistingSubnetworkWithoutRecordedState(t *testing.T) {
	const self = "projects/p/regions/europe-west2/subnetworks/dev-europe-west2"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !strings.HasSuffix(r.URL.Path, "/"+self) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"code":404,"message":"not found"}}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"kind": "compute#subnetwork", "id": "123", "name": "dev-europe-west2", "ipCidrRange": "10.10.0.0/20",
			"network": "https://www.googleapis.com/compute/v1/projects/p/global/networks/vpc-dev", "region": "europe-west2",
			"selfLink": "https://www.googleapis.com/compute/v1/" + self, "fingerprint": "abc", "stackType": "IPV4_ONLY",
			"privateIpGoogleAccess": true, "purpose": "PRIVATE",
		})
	}))
	defer srv.Close()

	sdk := provider.Provider()
	p, err := config.GetNamespacedProvider(context.Background(), sdk, fwprovider.New(sdk), false)
	if err != nil {
		t.Fatal(err)
	}
	cfg := p.Resources["google_compute_subnetwork"]
	meta := &transporttpg.Config{Project: "p", Client: srv.Client(), UserAgent: "test",
		CustomEndpoints: map[string]string{registry.GetProduct("compute").CustomEndpointField: srv.URL + "/compute/v1/"}}
	setup := func(context.Context, client.Client, xpresource.Managed) (terraform.Setup, error) {
		return terraform.Setup{Meta: meta, Configuration: map[string]any{"project": "p"}}, nil
	}

	mg := &v1beta1.Subnetwork{}
	if err := json.Unmarshal([]byte(`{
		"apiVersion": "compute.gcp.m.upbound.io/v1beta1", "kind": "Subnetwork",
		"metadata": {"name": "network-dev-subnet", "namespace": "team", "uid": "u1",
			"annotations": {"crossplane.io/external-name": "dev-europe-west2"}},
		"spec": {"managementPolicies": ["Observe", "Create", "Update", "LateInitialize"],
			"forProvider": {"project": "p", "region": "europe-west2", "ipCidrRange": "10.10.0.0/20", "network": "vpc-dev"}}
	}`), mg); err != nil {
		t.Fatal(err)
	}
	scheme := runtime.NewScheme()
	if err := v1beta1.SchemeBuilder.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mg).Build()
	connector := controller.NewTerraformPluginSDKConnector(kube, setup, cfg, controller.NewOperationStore(logging.NewNopLogger()),
		controller.WithTerraformPluginSDKManagementPolicies(true), controller.WithTerraformPluginSDKLogger(logging.NewNopLogger()),
		controller.WithTerraformPluginSDKMetricRecorder(metrics.NewMetricRecorder(v1beta1.Subnetwork_GroupVersionKind, nil, 0)))

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("observing an existing Subnetwork panicked: %v\n%s", r, debug.Stack())
		}
	}()
	ext, err := connector.Connect(context.Background(), mg)
	if err != nil {
		t.Fatal(err)
	}
	obs, err := ext.Observe(context.Background(), mg)
	if err != nil {
		t.Fatalf("observe: %v", err)
	}
	if !obs.ResourceExists {
		t.Fatal("the existing Subnetwork was observed as absent")
	}
	got := mg.Status.AtProvider.ID
	if got == nil || !strings.HasSuffix(*got, self) {
		t.Fatalf("observed state was not recorded: id %v", ptrString(got))
	}
}

func ptrString(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}
