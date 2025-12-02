package manifest

import (
	"fmt"
	"path"
	"strings"

	"github.com/docker/buildx/util/platformutil"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type DeploymentOpt struct {
	Namespace          string
	Name               string
	Image              string
	Replicas           int
	ServiceAccountName string
	SchedulerName      string

	// Qemu
	Qemu struct {
		// when true, will install binfmt
		Install bool
		Image   string
	}

	BuildkitFlags []string
	// files mounted at /etc/buildkitd
	ConfigFiles map[string][]byte

	BuildKitRootVolumeMemory string
	Rootless                 bool
	NodeSelector             map[string]string
	CustomAnnotations        map[string]string
	CustomLabels             map[string]string
	Tolerations              []corev1.Toleration
	RequestsCPU              string
	RequestsMemory           string
	RequestsEphemeralStorage string
	LimitsCPU                string
	LimitsMemory             string
	LimitsEphemeralStorage   string
	Platforms                []ocispecs.Platform
	Env                      []corev1.EnvVar // injected into main buildkitd container
}

const (
	containerName      = "buildkitd"
	AnnotationPlatform = "buildx.docker.com/platform"
	LabelApp           = "app"
	rootVolumeName     = "buildkit-memory"
	rootVolumePath     = "/var/lib/buildkit"
)

type ErrReservedAnnotationPlatform struct{}

func (ErrReservedAnnotationPlatform) Error() string {
	return fmt.Sprintf("the annotation %q is reserved and cannot be customized", AnnotationPlatform)
}

type ErrReservedLabelApp struct{}

func (ErrReservedLabelApp) Error() string {
	return fmt.Sprintf("the label %q is reserved and cannot be customized", LabelApp)
}

func NewDeployment(opt *DeploymentOpt) (d *appsv1.Deployment, c []*corev1.ConfigMap, err error) {
	labels := map[string]string{
		LabelApp: opt.Name,
	}
	annotations := map[string]string{}
	replicas := int32(opt.Replicas)
	privileged := true
	args := opt.BuildkitFlags

	if len(opt.Platforms) > 0 {
		annotations[AnnotationPlatform] = strings.Join(platformutil.Format(opt.Platforms), ",")
	}

	for k, v := range opt.CustomAnnotations {
		if k == AnnotationPlatform {
			return nil, nil, ErrReservedAnnotationPlatform{}
		}
		annotations[k] = v
	}

	for k, v := range opt.CustomLabels {
		if k == LabelApp {
			return nil, nil, ErrReservedLabelApp{}
		}
		labels[k] = v
	}

	d = &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: appsv1.SchemeGroupVersion.String(),
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   opt.Namespace,
			Name:        opt.Name,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      labels,
					Annotations: annotations,
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: opt.ServiceAccountName,
					SchedulerName:      opt.SchedulerName,
					Containers: []corev1.Container{
						{
							Name:  containerName,
							Image: opt.Image,
							Args:  args,
							SecurityContext: &corev1.SecurityContext{
								Privileged: &privileged,
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									Exec: &corev1.ExecAction{
										Command: []string{"buildctl", "debug", "workers"},
									},
								},
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{},
								Limits:   corev1.ResourceList{},
							},
						},
					},
				},
			},
		},
	}
	for _, cfg := range splitConfigFiles(opt.ConfigFiles) {
		cc := &corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				APIVersion: corev1.SchemeGroupVersion.String(),
				Kind:       "ConfigMap",
			},
			ObjectMeta: metav1.ObjectMeta{
				Namespace:   opt.Namespace,
				Name:        opt.Name + "-" + cfg.name,
				Annotations: annotations,
			},
			Data: cfg.files,
		}

		d.Spec.Template.Spec.Containers[0].VolumeMounts = append(d.Spec.Template.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{
			Name:      cfg.name,
			MountPath: path.Join("/etc/buildkit", cfg.path),
		})

		d.Spec.Template.Spec.Volumes = append(d.Spec.Template.Spec.Volumes, corev1.Volume{
			Name: cfg.name,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: cc.Name,
					},
				},
			},
		})
		c = append(c, cc)
	}

	if opt.Qemu.Install {
		d.Spec.Template.Spec.InitContainers = []corev1.Container{
			{
				Name:  "qemu",
				Image: opt.Qemu.Image,
				Args:  []string{"--install", "all"},
				SecurityContext: &corev1.SecurityContext{
					Privileged: &privileged,
				},
			},
		}
	}

	if opt.Rootless {
		if err := toRootless(d); err != nil {
			return nil, nil, err
		}
	}

	if len(opt.NodeSelector) > 0 {
		d.Spec.Template.Spec.NodeSelector = opt.NodeSelector
	}

	if len(opt.Tolerations) > 0 {
		d.Spec.Template.Spec.Tolerations = opt.Tolerations
	}

	if opt.RequestsCPU != "" {
		reqCPU, err := resource.ParseQuantity(opt.RequestsCPU)
		if err != nil {
			return nil, nil, err
		}
		d.Spec.Template.Spec.Containers[0].Resources.Requests[corev1.ResourceCPU] = reqCPU
	}

	if opt.RequestsMemory != "" {
		reqMemory, err := resource.ParseQuantity(opt.RequestsMemory)
		if err != nil {
			return nil, nil, err
		}
		d.Spec.Template.Spec.Containers[0].Resources.Requests[corev1.ResourceMemory] = reqMemory
	}

	if opt.RequestsEphemeralStorage != "" {
		reqEphemeralStorage, err := resource.ParseQuantity(opt.RequestsEphemeralStorage)
		if err != nil {
			return nil, nil, err
		}
		d.Spec.Template.Spec.Containers[0].Resources.Requests[corev1.ResourceEphemeralStorage] = reqEphemeralStorage
	}

	if opt.LimitsCPU != "" {
		limCPU, err := resource.ParseQuantity(opt.LimitsCPU)
		if err != nil {
			return nil, nil, err
		}
		d.Spec.Template.Spec.Containers[0].Resources.Limits[corev1.ResourceCPU] = limCPU
	}

	if opt.LimitsMemory != "" {
		limMemory, err := resource.ParseQuantity(opt.LimitsMemory)
		if err != nil {
			return nil, nil, err
		}
		d.Spec.Template.Spec.Containers[0].Resources.Limits[corev1.ResourceMemory] = limMemory
	}

	if opt.LimitsEphemeralStorage != "" {
		limEphemeralStorage, err := resource.ParseQuantity(opt.LimitsEphemeralStorage)
		if err != nil {
			return nil, nil, err
		}
		d.Spec.Template.Spec.Containers[0].Resources.Limits[corev1.ResourceEphemeralStorage] = limEphemeralStorage
	}

	if opt.BuildKitRootVolumeMemory != "" {
		buildKitRootVolumeMemory, err := resource.ParseQuantity(opt.BuildKitRootVolumeMemory)
		if err != nil {
			return nil, nil, err
		}
		d.Spec.Template.Spec.Volumes = append(d.Spec.Template.Spec.Volumes, corev1.Volume{
			Name: rootVolumeName,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{
					Medium:    "Memory",
					SizeLimit: &buildKitRootVolumeMemory,
				},
			},
		})
		d.Spec.Template.Spec.Containers[0].VolumeMounts = append(d.Spec.Template.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{
			Name:      rootVolumeName,
			MountPath: rootVolumePath,
		})
	}

	if len(opt.Env) > 0 {
		d.Spec.Template.Spec.Containers[0].Env = append(d.Spec.Template.Spec.Containers[0].Env, opt.Env...)
	}

	return
}

func toRootless(d *appsv1.Deployment) error {
	d.Spec.Template.Spec.Containers[0].Args = append(
		d.Spec.Template.Spec.Containers[0].Args,
		"--oci-worker-no-process-sandbox",
	)
	d.Spec.Template.Spec.Containers[0].SecurityContext = &corev1.SecurityContext{
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeUnconfined,
		},
	}
	if d.Spec.Template.Annotations == nil {
		d.Spec.Template.Annotations = make(map[string]string, 1)
	}
	d.Spec.Template.Annotations["container.apparmor.security.beta.kubernetes.io/"+containerName] = "unconfined"

	// Dockerfile has `VOLUME /home/user/.local/share/buildkit` by default too,
	// but the default VOLUME does not work with rootless on Google's Container-Optimized OS
	// as it is mounted with `nosuid,nodev`.
	// https://github.com/moby/buildkit/issues/879#issuecomment-1240347038
	// https://github.com/moby/buildkit/pull/3097
	const emptyDirVolName = "buildkitd"
	d.Spec.Template.Spec.Containers[0].VolumeMounts = append(d.Spec.Template.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{
		Name:      emptyDirVolName,
		MountPath: "/home/user/.local/share/buildkit",
	})
	d.Spec.Template.Spec.Volumes = append(d.Spec.Template.Spec.Volumes, corev1.Volume{
		Name: emptyDirVolName,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	})

	return nil
}

type config struct {
	name  string
	path  string
	files map[string]string
}

func splitConfigFiles(m map[string][]byte) []config {
	var c []config
	idx := map[string]int{}
	nameIdx := 0
	for k, v := range m {
		dir := path.Dir(k)
		i, ok := idx[dir]
		if !ok {
			idx[dir] = len(c)
			i = len(c)
			name := "config"
			if dir != "." {
				nameIdx++
				name = fmt.Sprintf("%s-%d", name, nameIdx)
			}
			c = append(c, config{
				path:  dir,
				name:  name,
				files: map[string]string{},
			})
		}
		c[i].files[path.Base(k)] = string(v)
	}
	return c
}
