package kube

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/handlerutil"
)

const (
	kubePath = "/environments/{id}/podman/kube"
	kubeTag  = "Podman"
)

func RegisterRoutes(api huma.API, h *Handler) {
	handlerutil.RegisterSecured(api,
		handlerutil.Operation("play-podman-kube", http.MethodPost, kubePath+"/play", "Play kube YAML", "Create pods and containers from Kubernetes YAML", kubeTag),
		authz.PermPodmanKube, h.Play)
	handlerutil.RegisterSecured(api,
		handlerutil.Operation("down-podman-kube", http.MethodPost, kubePath+"/down", "Tear down kube YAML", "Remove pods and containers created from Kubernetes YAML", kubeTag),
		authz.PermPodmanKube, h.Down)
	handlerutil.RegisterSecured(api,
		handlerutil.Operation("generate-podman-kube", http.MethodPost, kubePath+"/generate", "Generate kube YAML", "Generate Kubernetes YAML from pods, containers, or volumes", kubeTag),
		authz.PermPodmanKube, h.Generate)
}
