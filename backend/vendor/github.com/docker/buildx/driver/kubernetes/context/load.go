package context

import (
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/context"
	"github.com/docker/cli/cli/context/store"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// EndpointMeta is a typed wrapper around a context-store generic endpoint describing
// a Kubernetes endpoint, without TLS data
type EndpointMeta struct {
	context.EndpointMetaBase
	DefaultNamespace string                           `json:",omitempty"`
	ProxyURL         string                           `json:",omitempty"`
	AuthProvider     *clientcmdapi.AuthProviderConfig `json:",omitempty"`
	Exec             *clientcmdapi.ExecConfig         `json:",omitempty"`
	UsernamePassword *UsernamePassword                `json:"usernamePassword,omitempty"`
	Token            string                           `json:"token,omitempty"`
}

// UsernamePassword contains username/password auth info
type UsernamePassword struct {
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

var _ command.EndpointDefaultResolver = &EndpointMeta{}

// Endpoint is a typed wrapper around a context-store generic endpoint describing
// a Kubernetes endpoint, with TLS data
type Endpoint struct {
	EndpointMeta
	TLSData *context.TLSData
}

func init() {
	command.RegisterDefaultStoreEndpoints(
		store.EndpointTypeGetter(KubernetesEndpoint, func() any { return &EndpointMeta{} }),
	)
}

// WithTLSData loads TLS materials for the endpoint
func (c *EndpointMeta) WithTLSData(s store.Reader, contextName string) (Endpoint, error) {
	tlsData, err := context.LoadTLSData(s, contextName, KubernetesEndpoint)
	if err != nil {
		return Endpoint{}, err
	}
	return Endpoint{
		EndpointMeta: *c,
		TLSData:      tlsData,
	}, nil
}

// KubernetesConfig creates the kubernetes client config from the endpoint
func (c *Endpoint) KubernetesConfig() clientcmd.ClientConfig {
	cfg := clientcmdapi.NewConfig()
	cluster := clientcmdapi.NewCluster()
	cluster.Server = c.Host
	if c.ProxyURL != "" {
		cluster.ProxyURL = c.ProxyURL
	}
	cluster.InsecureSkipTLSVerify = c.SkipTLSVerify
	authInfo := clientcmdapi.NewAuthInfo()
	if c.TLSData != nil {
		cluster.CertificateAuthorityData = c.TLSData.CA
		authInfo.ClientCertificateData = c.TLSData.Cert
		authInfo.ClientKeyData = c.TLSData.Key
	}
	if c.UsernamePassword != nil {
		authInfo.Username = c.UsernamePassword.Username
		authInfo.Password = c.UsernamePassword.Password
	}
	if c.Token != "" {
		authInfo.Token = c.Token
	}
	authInfo.AuthProvider = c.AuthProvider
	authInfo.Exec = c.Exec
	cfg.Clusters["cluster"] = cluster
	cfg.AuthInfos["authInfo"] = authInfo
	ctx := clientcmdapi.NewContext()
	ctx.AuthInfo = "authInfo"
	ctx.Cluster = "cluster"
	ctx.Namespace = c.DefaultNamespace
	cfg.Contexts["context"] = ctx
	cfg.CurrentContext = "context"
	return clientcmd.NewDefaultClientConfig(*cfg, &clientcmd.ConfigOverrides{})
}

// ResolveDefault returns endpoint metadata for the default Kubernetes
// endpoint, which is derived from the env-based kubeconfig.
func (c *EndpointMeta) ResolveDefault() (any, *store.EndpointTLSData, error) {
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		kubeconfig = filepath.Join(getHomeDir(), ".kube/config")
	}
	kubeEP, err := FromKubeConfig(kubeconfig, "", "")
	if err != nil {
		// We deliberately quash the error here, returning nil
		// for the first argument is sufficient to indicate we weren't able to
		// provide a default
		return nil, nil, nil
	}
	var tls *store.EndpointTLSData
	if kubeEP.TLSData != nil {
		tls = kubeEP.TLSData.ToStoreTLSData()
	}
	return kubeEP.EndpointMeta, tls, nil
}

// EndpointFromContext extracts kubernetes endpoint info from current context
func EndpointFromContext(metadata store.Metadata) *EndpointMeta {
	ep, ok := metadata.Endpoints[KubernetesEndpoint]
	if !ok {
		return nil
	}
	typed, ok := ep.(EndpointMeta)
	if !ok {
		return nil
	}
	return &typed
}

// ConfigFromContext resolves a kubernetes client config for the specified context.
// If kubeconfigOverride is specified, use this config file instead of the context defaults.ConfigFromContext
// if command.ContextDockerHost is specified as the context name, fallsback to the default user's kubeconfig file
func ConfigFromContext(name string, s store.Reader) (clientcmd.ClientConfig, error) {
	ctxMeta, err := s.GetMetadata(name)
	if err != nil {
		return nil, err
	}
	epMeta := EndpointFromContext(ctxMeta)
	if epMeta != nil {
		ep, err := epMeta.WithTLSData(s, name)
		if err != nil {
			return nil, err
		}
		return ep.KubernetesConfig(), nil
	}
	// context has no kubernetes endpoint
	return NewKubernetesConfig(""), nil
}

// NewKubernetesConfig resolves the path to the desired Kubernetes configuration
// file based on the KUBECONFIG environment variable and command line flags.
func NewKubernetesConfig(configPath string) clientcmd.ClientConfig {
	kubeConfig := configPath
	if kubeConfig == "" {
		if config := os.Getenv("KUBECONFIG"); config != "" {
			kubeConfig = config
		} else {
			kubeConfig = filepath.Join(getHomeDir(), ".kube/config")
		}
	}
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeConfig},
		&clientcmd.ConfigOverrides{})
}

// ConfigFromEndpoint loads kubernetes config from endpoint
func ConfigFromEndpoint(endpointName string, s store.Reader) (clientcmd.ClientConfig, error) {
	if strings.HasPrefix(endpointName, "kubernetes://") {
		rules := clientcmd.NewDefaultClientConfigLoadingRules()
		u, _ := url.Parse(endpointName)
		if kubeconfig := u.Query().Get("kubeconfig"); kubeconfig != "" {
			rules.Precedence = append(rules.Precedence, kubeconfig)
			rules.ExplicitPath = kubeconfig
		}
		apiConfig, err := rules.Load()
		if err != nil {
			return nil, err
		}
		return clientcmd.NewDefaultClientConfig(*apiConfig, &clientcmd.ConfigOverrides{}), nil
	}
	return ConfigFromContext(endpointName, s)
}

// getHomeDir returns the home directory of the current user with the help of
// environment variables depending on the target operating system.
// Returned path should be used with "path/filepath" to form new paths.
//
// On non-Windows platforms, it falls back to nss lookups, if the home
// directory cannot be obtained from environment-variables.
//
// If linking statically with cgo enabled against glibc, ensure the
// osusergo build tag is used.
//
// If needing to do nss lookups, do not disable cgo or set osusergo.
//
// It's a local fork of [pkg/homedir].
//
// [pkg/homedir]: https://github.com/moby/moby/blob/v28.3.2/pkg/homedir/homedir.go#L9-L28
func getHomeDir() string {
	home, _ := os.UserHomeDir()
	if home == "" && runtime.GOOS != "windows" {
		if u, err := user.Current(); err == nil {
			return u.HomeDir
		}
	}
	return home
}
