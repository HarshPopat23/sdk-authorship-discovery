package main

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"sync"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
)

type fixture struct {
	Name             string
	Receiver, Getter reflect.Type
	Method           string
	Invoke           func(context.Context, *kubernetes.Clientset) error
}

// These calls are deliberately authored fixtures, not an automatic SDK enumerator.
func listPods(ctx context.Context, c *kubernetes.Clientset) error {
	pods, err := c.CoreV1().Pods("demo").List(ctx, metav1.ListOptions{})
	if err == nil && len(pods.Items) != 0 {
		return fmt.Errorf("unexpected fixture Pods")
	}
	return err
}
func getConfigMap(ctx context.Context, c *kubernetes.Clientset) error {
	cm, err := c.CoreV1().ConfigMaps("demo").Get(ctx, "settings", metav1.GetOptions{})
	if err == nil && cm.Data["source"] != "envtest-fixture" {
		return fmt.Errorf("unexpected fixture ConfigMap")
	}
	return err
}
func fixtures() []fixture {
	return []fixture{
		{"pods-list", reflect.TypeFor[corev1.PodInterface](), reflect.TypeFor[corev1.PodsGetter](), "List", listPods},
		{"configmaps-get", reflect.TypeFor[corev1.ConfigMapInterface](), reflect.TypeFor[corev1.ConfigMapsGetter](), "Get", getConfigMap},
	}
}

type recorder struct {
	base     http.RoundTripper
	mu       sync.Mutex
	requests []Request
}

func (r *recorder) RoundTrip(req *http.Request) (*http.Response, error) {
	response, err := r.base.RoundTrip(req)
	// Deliberately exclude host/port, headers, credentials, bodies and raw errors.
	event := Request{Method: req.Method, Path: req.URL.EscapedPath(), HasQuery: req.URL.RawQuery != "", TransportError: err != nil}
	if response != nil {
		event.Status = response.StatusCode
	}
	r.mu.Lock()
	r.requests = append(r.requests, event)
	r.mu.Unlock()
	return response, err
}
func observe(ctx context.Context, cfg *rest.Config, f fixture) (Observation, error) {
	sdk, err := identity(f.Receiver, f.Getter, f.Method)
	if err != nil {
		return Observation{}, err
	}
	var capture *recorder
	config := rest.CopyConfig(cfg)
	config.Wrap(func(base http.RoundTripper) http.RoundTripper {
		capture = &recorder{base: base, requests: []Request{}}
		return capture
	})
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return Observation{}, err
	}
	// No discovery/setup requests are made with this fresh measured client.
	err = f.Invoke(ctx, client)
	capture.mu.Lock()
	requests := append([]Request{}, capture.requests...)
	capture.mu.Unlock()
	return Observation{Case: f.Name, SDK: sdk, Requests: requests, SDKError: err != nil}, nil
}
