package context

import (
	"os"

	"github.com/docker/cli/cli/context"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// FromKubeConfig creates a Kubernetes endpoint from a Kubeconfig file
func FromKubeConfig(kubeconfig, kubeContext, namespaceOverride string) (Endpoint, error) {
	cfg := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfig},
		&clientcmd.ConfigOverrides{CurrentContext: kubeContext, Context: clientcmdapi.Context{Namespace: namespaceOverride}})
	ns, _, err := cfg.Namespace()
	if err != nil {
		return Endpoint{}, err
	}
	clientcfg, err := cfg.ClientConfig()
	if err != nil {
		return Endpoint{}, err
	}

	var proxyURLString string
	if clientcfg.Proxy != nil {
		proxyURL, err := clientcfg.Proxy(nil)
		if err != nil {
			return Endpoint{}, err
		}

		proxyURLString = proxyURL.String()
	}

	var ca, key, cert []byte
	if ca, err = readFileOrDefault(clientcfg.CAFile, clientcfg.CAData); err != nil {
		return Endpoint{}, err
	}
	if key, err = readFileOrDefault(clientcfg.KeyFile, clientcfg.KeyData); err != nil {
		return Endpoint{}, err
	}
	if cert, err = readFileOrDefault(clientcfg.CertFile, clientcfg.CertData); err != nil {
		return Endpoint{}, err
	}
	var tlsData *context.TLSData
	if ca != nil || cert != nil || key != nil {
		tlsData = &context.TLSData{
			CA:   ca,
			Cert: cert,
			Key:  key,
		}
	}
	var usernamePassword *UsernamePassword
	if clientcfg.Username != "" || clientcfg.Password != "" {
		usernamePassword = &UsernamePassword{
			Username: clientcfg.Username,
			Password: clientcfg.Password,
		}
	}
	return Endpoint{
		EndpointMeta: EndpointMeta{
			EndpointMetaBase: context.EndpointMetaBase{
				Host:          clientcfg.Host,
				SkipTLSVerify: clientcfg.Insecure,
			},
			DefaultNamespace: ns,
			ProxyURL:         proxyURLString,
			AuthProvider:     clientcfg.AuthProvider,
			Exec:             clientcfg.ExecProvider,
			UsernamePassword: usernamePassword,
			Token:            clientcfg.BearerToken,
		},
		TLSData: tlsData,
	}, nil
}

func readFileOrDefault(path string, defaultValue []byte) ([]byte, error) {
	if path != "" {
		return os.ReadFile(path)
	}
	return defaultValue, nil
}
