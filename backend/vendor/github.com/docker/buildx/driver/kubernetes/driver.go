package kubernetes

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/docker/buildx/driver"
	"github.com/docker/buildx/driver/kubernetes/execconn"
	"github.com/docker/buildx/driver/kubernetes/manifest"
	"github.com/docker/buildx/driver/kubernetes/podchooser"
	"github.com/docker/buildx/store"
	"github.com/docker/buildx/util/platformutil"
	"github.com/docker/buildx/util/progress"
	"github.com/docker/go-units"
	"github.com/moby/buildkit/client"
	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	clientappsv1 "k8s.io/client-go/kubernetes/typed/apps/v1"
	clientcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
)

const (
	DriverName = "kubernetes"
)

const (
	// valid values for driver-opt loadbalance
	LoadbalanceRandom = "random"
	LoadbalanceSticky = "sticky"
)

type Driver struct {
	driver.InitConfig
	factory      driver.Factory
	clientConfig ClientConfig

	// if you add fields, remember to update docs:
	// https://github.com/docker/docs/blob/main/content/build/drivers/kubernetes.md
	minReplicas      int
	deployment       *appsv1.Deployment
	configMaps       []*corev1.ConfigMap
	clientset        *kubernetes.Clientset
	deploymentClient clientappsv1.DeploymentInterface
	podClient        clientcorev1.PodInterface
	configMapClient  clientcorev1.ConfigMapInterface
	podChooser       podchooser.PodChooser
	defaultLoad      bool
	timeout          time.Duration
}

func (d *Driver) IsMobyDriver() bool {
	return false
}

func (d *Driver) Config() driver.InitConfig {
	return d.InitConfig
}

func (d *Driver) Bootstrap(ctx context.Context, l progress.Logger) error {
	return progress.Wrap("[internal] booting buildkit", l, func(sub progress.SubLogger) error {
		_, err := d.deploymentClient.Get(ctx, d.deployment.Name, metav1.GetOptions{})
		if err != nil {
			if !apierrors.IsNotFound(err) {
				return errors.Wrapf(err, "error for bootstrap %q", d.deployment.Name)
			}

			for _, cfg := range d.configMaps {
				// create ConfigMap first if exists
				_, err = d.configMapClient.Create(ctx, cfg, metav1.CreateOptions{})
				if err != nil {
					if !apierrors.IsAlreadyExists(err) {
						return errors.Wrapf(err, "error while calling configMapClient.Create for %q", cfg.Name)
					}
					_, err = d.configMapClient.Update(ctx, cfg, metav1.UpdateOptions{})
					if err != nil {
						return errors.Wrapf(err, "error while calling configMapClient.Update for %q", cfg.Name)
					}
				}
			}

			_, err = d.deploymentClient.Create(ctx, d.deployment, metav1.CreateOptions{})
			if err != nil {
				return errors.Wrapf(err, "error while calling deploymentClient.Create for %q", d.deployment.Name)
			}
		}
		return sub.Wrap(
			fmt.Sprintf("waiting for %d pods to be ready, timeout: %s", d.minReplicas, units.HumanDuration(d.timeout)),
			func() error {
				return d.wait(ctx)
			})
	})
}

func (d *Driver) wait(ctx context.Context) error {
	// TODO: use watch API
	var (
		err  error
		depl *appsv1.Deployment
	)

	timeoutChan := time.After(d.timeout)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		case <-timeoutChan:
			return err
		case <-ticker.C:
			depl, err = d.deploymentClient.Get(ctx, d.deployment.Name, metav1.GetOptions{})
			if err == nil {
				if depl.Status.ReadyReplicas >= int32(d.minReplicas) {
					return nil
				}
				err = errors.Errorf("expected %d replicas to be ready, got %d", d.minReplicas, depl.Status.ReadyReplicas)
			}
		}
	}
}

func (d *Driver) Info(ctx context.Context) (*driver.Info, error) {
	depl, err := d.deploymentClient.Get(ctx, d.deployment.Name, metav1.GetOptions{})
	if err != nil {
		// TODO: return err if err != ErrNotFound
		return &driver.Info{
			Status: driver.Inactive,
		}, nil
	}
	if depl.Status.ReadyReplicas <= 0 {
		return &driver.Info{
			Status: driver.Stopped,
		}, nil
	}
	pods, err := podchooser.ListRunningPods(ctx, d.podClient, depl)
	if err != nil {
		return nil, err
	}
	var dynNodes []store.Node
	for _, p := range pods {
		node := store.Node{
			Name: p.Name,
			// Other fields are unset (TODO: detect real platforms)
		}

		if p.Annotations != nil {
			if p, ok := p.Annotations[manifest.AnnotationPlatform]; ok {
				ps, err := platformutil.Parse(strings.Split(p, ","))
				if err == nil {
					node.Platforms = ps
				}
			}
		}

		dynNodes = append(dynNodes, node)
	}
	return &driver.Info{
		Status:       driver.Running,
		DynamicNodes: dynNodes,
	}, nil
}

func (d *Driver) Version(ctx context.Context) (string, error) {
	return "", nil
}

func (d *Driver) Stop(ctx context.Context, force bool) error {
	// future version may scale the replicas to zero here
	return nil
}

func (d *Driver) Rm(ctx context.Context, force, rmVolume, rmDaemon bool) error {
	if !rmDaemon {
		return nil
	}

	if err := d.deploymentClient.Delete(ctx, d.deployment.Name, metav1.DeleteOptions{}); err != nil {
		if !apierrors.IsNotFound(err) {
			return errors.Wrapf(err, "error while calling deploymentClient.Delete for %q", d.deployment.Name)
		}
	}
	for _, cfg := range d.configMaps {
		if err := d.configMapClient.Delete(ctx, cfg.Name, metav1.DeleteOptions{}); err != nil {
			if !apierrors.IsNotFound(err) {
				return errors.Wrapf(err, "error while calling configMapClient.Delete for %q", cfg.Name)
			}
		}
	}
	return nil
}

func (d *Driver) Dial(ctx context.Context) (net.Conn, error) {
	restClient := d.clientset.CoreV1().RESTClient()
	restClientConfig, err := d.clientConfig.ClientConfig()
	if err != nil {
		return nil, err
	}
	pod, err := d.podChooser.ChoosePod(ctx)
	if err != nil {
		return nil, err
	}
	if len(pod.Spec.Containers) == 0 {
		return nil, errors.Errorf("pod %s does not have any container", pod.Name)
	}
	containerName := pod.Spec.Containers[0].Name
	cmd := []string{"buildctl", "dial-stdio"}
	conn, err := execconn.ExecConn(ctx, restClient, restClientConfig, pod.Namespace, pod.Name, containerName, cmd)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

func (d *Driver) Client(ctx context.Context, opts ...client.ClientOpt) (*client.Client, error) {
	opts = append([]client.ClientOpt{
		client.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return d.Dial(ctx)
		}),
	}, opts...)
	return client.New(ctx, "", opts...)
}

func (d *Driver) Factory() driver.Factory {
	return d.factory
}

func (d *Driver) Features(_ context.Context) map[driver.Feature]bool {
	return map[driver.Feature]bool{
		driver.OCIExporter:    true,
		driver.DockerExporter: d.DockerAPI != nil,
		driver.CacheExport:    true,
		driver.MultiPlatform:  true, // Untested (needs multiple Driver instances)
		driver.DirectPush:     true,
		driver.DefaultLoad:    d.defaultLoad,
	}
}

func (d *Driver) HostGatewayIP(_ context.Context) (net.IP, error) {
	return nil, errors.New("host-gateway is not supported by the kubernetes driver")
}
