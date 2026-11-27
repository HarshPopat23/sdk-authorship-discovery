package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

const kubernetesVersion = "v1.36.2"

type Evidence struct {
	Kubernetes           string        `json:"kubernetes"`
	ClientGo             string        `json:"clientGo"`
	ControllerRuntime    string        `json:"controllerRuntime"`
	Observer             string        `json:"observer"`
	SetupTrafficExcluded bool          `json:"setupTrafficExcluded"`
	OpenAPISHA256        string        `json:"openApiSha256"`
	Observations         []Observation `json:"observations"`
	Matches              []Match       `json:"matches"`
	FailedCall           Observation   `json:"failedCall"`
	FailedCallRejection  string        `json:"failedCallRejection"`
}

func run(ctx context.Context, swagger string) (mapping Mapping, profile map[string]any, evidence Evidence, err error) {
	document, err := loadDocument(swagger)
	if err != nil {
		return mapping, nil, evidence, err
	}
	assets := os.Getenv("KUBEBUILDER_ASSETS")
	if assets == "" {
		return mapping, nil, evidence, fmt.Errorf("set KUBEBUILDER_ASSETS using the explicit setup step; no fake-client fallback")
	}
	no := false
	environment := &envtest.Environment{BinaryAssetsDirectory: assets, AttachControlPlaneOutput: os.Getenv("ENVTEST_DEBUG") == "1", UseExistingCluster: &no, DownloadBinaryAssets: false, ControlPlaneStartTimeout: 60 * time.Second, ControlPlaneStopTimeout: 30 * time.Second}
	// A local-only test control plane should not depend on a host default route.
	environment.ControlPlane.GetAPIServer().Configure().Set("advertise-address", "127.0.0.1").Set("bind-address", "127.0.0.1")
	cfg, err := environment.Start()
	if err != nil {
		return mapping, nil, evidence, errors.Join(fmt.Errorf("start real envtest: %w", err), environment.Stop())
	}
	defer func() {
		if stop := environment.Stop(); stop != nil && err == nil {
			err = fmt.Errorf("stop envtest: %w", stop)
		}
	}()
	setup, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return mapping, nil, evidence, err
	}
	version, err := setup.Discovery().ServerVersion()
	if err != nil {
		return mapping, nil, evidence, err
	}
	if version.GitVersion != kubernetesVersion {
		return mapping, nil, evidence, fmt.Errorf("expected API server %s, got %s", kubernetesVersion, version.GitVersion)
	}
	if _, err = setup.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "demo"}}, metav1.CreateOptions{}); err != nil {
		return mapping, nil, evidence, err
	}
	if _, err = setup.CoreV1().ConfigMaps("demo").Create(ctx, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "settings"}, Data: map[string]string{"source": "envtest-fixture"}}, metav1.CreateOptions{}); err != nil {
		return mapping, nil, evidence, err
	}
	evidence = Evidence{Kubernetes: version.GitVersion, ClientGo: "v0.36.2", ControllerRuntime: "v0.24.1", Observer: "rest.Config.WrapTransport (before TLS)", SetupTrafficExcluded: true, OpenAPISHA256: swaggerSHA256}
	for _, f := range fixtures() {
		o, e := observe(ctx, cfg, f)
		if e != nil {
			return mapping, nil, evidence, e
		}
		evidence.Observations = append(evidence.Observations, o)
	}
	mapping, evidence.Matches, err = generateMapping(evidence.Observations, document)
	if err != nil {
		return mapping, nil, evidence, err
	}
	missing := fixtures()[1]
	missing.Name = "configmaps-get-missing"
	missing.Invoke = func(ctx context.Context, c *kubernetes.Clientset) error {
		_, e := c.CoreV1().ConfigMaps("demo").Get(ctx, "missing", metav1.GetOptions{})
		return e
	}
	evidence.FailedCall, err = observe(ctx, cfg, missing)
	if err != nil {
		return mapping, nil, evidence, err
	}
	_, rejection := classify(document, evidence.FailedCall)
	if rejection == nil || !evidence.FailedCall.SDKError || len(evidence.FailedCall.Requests) != 1 || evidence.FailedCall.Requests[0].Status != 404 {
		return mapping, nil, evidence, fmt.Errorf("failed-call control did not produce and reject one real 404")
	}
	evidence.FailedCallRejection = rejection.Error()
	conditions := []any{}
	for _, m := range evidence.Matches {
		conditions = append(conditions, condition(m))
	}
	profile = map[string]any{"apiVersion": "runtimeconditions.io/v1alpha1", "kind": "RuntimeConditionsProfile", "metadata": map[string]string{"name": "client-go-envtest-observation"}, "workload": map[string]string{"uri": "https://example.test/client-go-envtest-observation", "version": "dev"}, "extensions": []string{extensionID}, "conditions": conditions}
	return mapping, profile, evidence, nil
}
func writeResults(out string, m Mapping, p map[string]any, e Evidence) error {
	files := map[string][]byte{}
	b, err := yaml.Marshal(m)
	if err != nil {
		return err
	}
	files["runtimeconditions.sdk-mapping.yaml"] = b
	b, err = yaml.Marshal(p)
	if err != nil {
		return err
	}
	files["profile.yaml"] = b
	b, err = json.MarshalIndent(e, "", "  ")
	if err != nil {
		return err
	}
	files["observations.json"] = append(b, '\n')
	if err = os.MkdirAll(out, 0755); err != nil {
		return err
	}
	for name, b := range files {
		if err = os.WriteFile(filepath.Join(out, name), b, 0644); err != nil {
			return err
		}
	}
	return nil
}
func main() {
	swagger := flag.String("swagger", "../kubernetes-go-demo/inputs/swagger.json", "pinned local Kubernetes OpenAPI document")
	out := flag.String("out", "results", "output directory")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	m, p, e, err := run(ctx, *swagger)
	if err == nil {
		err = writeResults(*out, m, p, e)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
