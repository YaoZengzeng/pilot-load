package kube

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/howardjohn/pilot-load/pkg/simulation/util"
	authenticationv1 "k8s.io/api/authentication/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/retry"

	"istio.io/client-go/pkg/apis/networking/v1alpha3"
	istioscheme "istio.io/client-go/pkg/clientset/versioned/scheme"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/pkg/log"
)

type Client struct {
	ClusterName string
	dynamic     dynamic.Interface
	Kubernetes  kubernetes.Interface
}

func NewClient(kubeconfig string, qps int) (*Client, error) {
	var config *rest.Config
	var err error
	var clusterName string
	if _, err := os.Stat(kubeconfig); err == nil {
		loader := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfig}, nil)
		config, err = loader.ClientConfig()
		if err != nil {
			return nil, err
		}
		rc, err := loader.RawConfig()
		if err != nil {
			return nil, err
		}
		clusterName = rc.Contexts[rc.CurrentContext].Cluster
	} else {
		log.Infof("using in cluster kubeconfig")
		// creates the in-cluster config
		config, err = rest.InClusterConfig()
		if err != nil {
			return nil, err
		}
	}
	config.QPS = float32(qps)
	config.Burst = qps * 2
	d, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	k, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	return &Client{
		ClusterName: clusterName,
		dynamic:     d,
		Kubernetes:  k,
	}, nil
}

func (c *Client) Informers() informers.SharedInformerFactory {
	inf := informers.NewSharedInformerFactory(c.Kubernetes, 0)
	return inf
}

func (c *Client) Finalize(ns *v1.Namespace) error {
	scope.Debugf("finalizing namespace: %v", ns.Name)
	_, err := c.Kubernetes.CoreV1().Namespaces().Finalize(context.TODO(), ns, metav1.UpdateOptions{})
	return err
}

func (c *Client) Delete(o runtime.Object) error {
	us := toUnstructured(o)
	if us == nil {
		return fmt.Errorf("bad object %v", o)
	}
	gvr, kind := toGvr(o)
	cl := c.dynamic.Resource(gvr).Namespace(us.GetNamespace())
	us.SetGroupVersionKind(gvr.GroupVersion().WithKind(kind))
	scope.Debugf("deleting resource: %s/%s/%s", us.GetKind(), us.GetName(), us.GetNamespace())
	var policy = metav1.DeletePropagationBackground
	return cl.Delete(context.TODO(), us.GetName(), metav1.DeleteOptions{PropagationPolicy: &policy})
}

var scope = log.RegisterScope("kube", "", 0)

func init() {
	if err := istioscheme.AddToScheme(scheme.Scheme); err != nil {
		panic(err.Error())
	}
}

// TODO make this generic
func toGvr(o runtime.Object) (schema.GroupVersionResource, string) {
	switch o.(type) {
	case *v1.Pod:
		return v1.SchemeGroupVersion.WithResource("pods"), "Pod"
	case *v1.Node:
		return v1.SchemeGroupVersion.WithResource("nodes"), "Node"
	case *v1.Service:
		return v1.SchemeGroupVersion.WithResource("services"), "Service"
	case *v1.ServiceAccount:
		return v1.SchemeGroupVersion.WithResource("serviceaccounts"), "ServiceAccount"
	case *v1.Namespace:
		return v1.SchemeGroupVersion.WithResource("namespaces"), "Namespace"
	case *v1.Secret:
		return v1.SchemeGroupVersion.WithResource("secrets"), "Secret"
	case *v1.Endpoints:
		return v1.SchemeGroupVersion.WithResource("endpoints"), "Endpoints"
	case *v1alpha3.VirtualService:
		return v1alpha3.SchemeGroupVersion.WithResource("virtualservices"), "VirtualService"
	case *v1alpha3.Sidecar:
		return v1alpha3.SchemeGroupVersion.WithResource("sidecars"), "Sidecar"
	case *v1alpha3.Gateway:
		return v1alpha3.SchemeGroupVersion.WithResource("gateways"), "Gateway"
	case *v1alpha3.DestinationRule:
		return v1alpha3.SchemeGroupVersion.WithResource("destinationrules"), "DestinationRule"
	case *v1alpha3.ServiceEntry:
		return v1alpha3.SchemeGroupVersion.WithResource("serviceentries"), "ServiceEntry"
	default:
		panic(fmt.Sprintf("unsupported type %T", o))
	}
}

func (c *Client) Apply(o runtime.Object) error {
	return c.internalApply(o, false)
}

func (c *Client) ApplyFast(o runtime.Object) error {
	return c.internalApply(o, true)
}

func hasStatus(us *unstructured.Unstructured) bool {
	ifc, f := us.Object["status"]
	if !f {
		return false
	}
	cst, ok := ifc.(map[string]interface{})
	if ok && len(cst) == 0 {
		return false
	}
	return true
}

func (c *Client) internalApply(o runtime.Object, skipGet bool) error {
	us := toUnstructured(o)
	if us == nil {
		return fmt.Errorf("bad object %v", o)
	}
	gvr, kind := toGvr(o)
	backoff := wait.Backoff{Duration: time.Millisecond * 10, Factor: 2, Steps: 3}
	cl := c.dynamic.Resource(gvr).Namespace(us.GetNamespace())
	us.SetGroupVersionKind(gvr.GroupVersion().WithKind(kind))

	if skipGet {
		err := retry.RetryOnConflict(backoff, func() error {
			scope.Debugf("fast creating resource: %s/%s/%s", us.GetKind(), us.GetName(), us.GetNamespace())
			if _, err := cl.Create(context.TODO(), us, metav1.CreateOptions{}); err != nil {
				return err
			}
			if hasStatus(us) {
				scope.Debugf("fast updating resource status: %s/%s.%s", us.GetKind(), us.GetName(), us.GetNamespace())
				if _, err := cl.UpdateStatus(context.TODO(), us, metav1.UpdateOptions{}); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("failed to create %s/%s/%s: %v", us.GetKind(), us.GetName(), us.GetNamespace(), err)
		}
		return nil
	}
	err := retry.RetryOnConflict(backoff, func() error {
		cur, err := cl.Get(context.TODO(), us.GetName(), metav1.GetOptions{})
		switch {
		case errors.IsNotFound(err):
			scope.Debugf("creating resource: %s/%s/%s", us.GetKind(), us.GetName(), us.GetNamespace())
			if _, err = cl.Create(context.TODO(), us, metav1.CreateOptions{}); err != nil {
				return err
			}
			if hasStatus(us) {
				scope.Debugf("updating resource status: %s/%s.%s", us.GetKind(), us.GetName(), us.GetNamespace())
				if _, err := cl.UpdateStatus(context.TODO(), us, metav1.UpdateOptions{}); err != nil {
					return err
				}
			}
			return nil
		case err == nil:
			scope.Debugf("patching resource: %s/%s/%s", us.GetKind(), us.GetName(), us.GetNamespace())
			us.SetResourceVersion(cur.GetResourceVersion())
			bytes, err := us.MarshalJSON()
			if err != nil {
				return fmt.Errorf("json error for %s/%s/%s: %v", us.GetKind(), us.GetName(), us.GetNamespace(), err)
			}
			_, err = cl.Patch(context.TODO(), us.GetName(), types.ApplyPatchType, bytes, metav1.PatchOptions{
				FieldManager: "pilot-load",
				Force:        util.BoolPointer(true),
			})
			return err
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to apply %s/%s/%s: %v", us.GetKind(), us.GetName(), us.GetNamespace(), err)
	}
	return nil
}

func (c *Client) FetchRootCert() (string, error) {
	cm, err := c.Kubernetes.CoreV1().ConfigMaps("istio-system").Get(context.TODO(), "istio-ca-root-cert", metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	return cm.Data["root-cert.pem"], nil
}

// 7 days
var saTokenExpiration int64 = 60 * 60 * 24 * 7

func (c *Client) CreateServiceAccountToken(aud, ns, serviceAccount string) (string, time.Time, error) {
	scopes.Framework.Debugf("Creating service account token for: %s/%s", ns, serviceAccount)

	token, err := c.Kubernetes.CoreV1().ServiceAccounts(ns).CreateToken(context.TODO(), serviceAccount,
		&authenticationv1.TokenRequest{
			Spec: authenticationv1.TokenRequestSpec{
				Audiences:         []string{aud},
				ExpirationSeconds: &saTokenExpiration,
			},
		}, metav1.CreateOptions{})
	if err != nil {
		return "", time.Time{}, err
	}
	return token.Status.Token, token.Status.ExpirationTimestamp.Time, nil
}

func toUnstructured(o runtime.Object) *unstructured.Unstructured {
	unsObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(o)
	if err != nil {
		return nil
	}
	return &unstructured.Unstructured{Object: unsObj}
}
