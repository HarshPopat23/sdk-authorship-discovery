//go:build integration

package main

import (
	"context"
	"testing"
	"time"
)

func TestEnvtest(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	mapping, profile, evidence, err := run(ctx, "../kubernetes-go-demo/inputs/swagger.json")
	if err != nil {
		t.Fatal(err)
	} // No skip and no fake-client substitution.
	if evidence.Kubernetes != kubernetesVersion || !evidence.SetupTrafficExcluded {
		t.Fatal("wrong test environment")
	}
	if len(evidence.Observations) != 2 || len(mapping.Rules) != 5 || len(profile["conditions"].([]any)) != 2 {
		t.Fatal("incomplete live evidence")
	}
	expected := []struct{ path, id, receiver string }{
		{"/api/v1/namespaces/demo/pods", "listCoreV1NamespacedPod", "PodInterface"},
		{"/api/v1/namespaces/demo/configmaps/settings", "readCoreV1NamespacedConfigMap", "ConfigMapInterface"},
	}
	for i, want := range expected {
		o := evidence.Observations[i]
		if o.SDKError || o.SDK.Receiver.Name != want.receiver || len(o.Requests) != 1 || o.Requests[0].Method != "GET" || o.Requests[0].Path != want.path || o.Requests[0].Status != 200 || evidence.Matches[i].OperationID != want.id {
			t.Fatalf("unexpected live request: %+v", o)
		}
	}
	if !evidence.FailedCall.SDKError || evidence.FailedCall.Requests[0].Status != 404 || evidence.FailedCallRejection == "" {
		t.Fatal("missing failed-call control")
	}
	if err = writeResults(t.TempDir(), mapping, profile, evidence); err != nil {
		t.Fatal(err)
	}
	t.Log("LIVE ENVTEST VERIFIED: two real GET operations; setup excluded; real 404 rejected")
}
