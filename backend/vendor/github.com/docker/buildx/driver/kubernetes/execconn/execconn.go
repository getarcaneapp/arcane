package execconn

import (
	"context"
	"io"
	"net"
	"os"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

func ExecConn(ctx context.Context, restClient rest.Interface, restConfig *rest.Config, namespace, pod, container string, cmd []string) (net.Conn, error) {
	req := restClient.
		Post().
		Namespace(namespace).
		Resource("pods").
		Name(pod).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: container,
			Command:   cmd,
			Stdin:     true,
			Stdout:    true,
			Stderr:    true,
			TTY:       false,
		}, scheme.ParameterCodec)
	exec, err := remotecommand.NewSPDYExecutor(restConfig, "POST", req.URL())
	if err != nil {
		return nil, err
	}
	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()
	kc := &kubeConn{
		stdin:      stdinW,
		stdout:     stdoutR,
		localAddr:  dummyAddr{network: "dummy", s: "dummy-0"},
		remoteAddr: dummyAddr{network: "dummy", s: "dummy-1"},
	}
	go func() {
		serr := exec.StreamWithContext(ctx, remotecommand.StreamOptions{
			Stdin:  stdinR,
			Stdout: stdoutW,
			Stderr: os.Stderr,
			Tty:    false,
		})
		if serr != nil && serr != context.Canceled {
			logrus.Error(serr)
		}
	}()
	return kc, nil
}

type kubeConn struct {
	stdin         io.WriteCloser
	stdout        io.ReadCloser
	stdioClosedMu sync.Mutex // for stdinClosed and stdoutClosed
	stdinClosed   bool
	stdoutClosed  bool
	localAddr     net.Addr
	remoteAddr    net.Addr
}

func (c *kubeConn) Write(p []byte) (int, error) {
	return c.stdin.Write(p)
}

func (c *kubeConn) Read(p []byte) (int, error) {
	return c.stdout.Read(p)
}

func (c *kubeConn) CloseWrite() error {
	err := c.stdin.Close()
	c.stdioClosedMu.Lock()
	c.stdinClosed = true
	c.stdioClosedMu.Unlock()
	return err
}
func (c *kubeConn) CloseRead() error {
	err := c.stdout.Close()
	c.stdioClosedMu.Lock()
	c.stdoutClosed = true
	c.stdioClosedMu.Unlock()
	return err
}

func (c *kubeConn) Close() error {
	var err error
	c.stdioClosedMu.Lock()
	stdinClosed := c.stdinClosed
	c.stdioClosedMu.Unlock()
	if !stdinClosed {
		err = c.CloseWrite()
	}
	c.stdioClosedMu.Lock()
	stdoutClosed := c.stdoutClosed
	c.stdioClosedMu.Unlock()
	if !stdoutClosed {
		err = c.CloseRead()
	}
	return err
}

func (c *kubeConn) LocalAddr() net.Addr {
	return c.localAddr
}
func (c *kubeConn) RemoteAddr() net.Addr {
	return c.remoteAddr
}
func (c *kubeConn) SetDeadline(t time.Time) error {
	return nil
}
func (c *kubeConn) SetReadDeadline(t time.Time) error {
	return nil
}
func (c *kubeConn) SetWriteDeadline(t time.Time) error {
	return nil
}

type dummyAddr struct {
	network string
	s       string
}

func (d dummyAddr) Network() string {
	return d.network
}

func (d dummyAddr) String() string {
	return d.s
}
