package fixture

import (
	"context"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func init() { panic("this workload must never execute") }
func Inspect(ctx context.Context, clientset *kubernetes.Clientset) {
	_, _ = clientset.CoreV1().Pods("demo").List(ctx, metav1.ListOptions{})
}
