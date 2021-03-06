package hosts

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/docker/docker/client"
	"github.com/rancher/rke/docker"
	"github.com/sirupsen/logrus"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/terminal"
)

const (
	DockerAPIVersion = "1.24"
	K8sVersion       = "1.8"
)

func (h *Host) TunnelUp(dialerFactory DialerFactory) error {
	if h.DClient != nil {
		return nil
	}
	logrus.Infof("[dialer] Setup tunnel for host [%s]", h.Address)
	httpClient, err := h.newHTTPClient(dialerFactory)
	if err != nil {
		return fmt.Errorf("Can't establish dialer connection: %v", err)
	}

	// set Docker client
	logrus.Debugf("Connecting to Docker API for host [%s]", h.Address)
	h.DClient, err = client.NewClient("unix:///var/run/docker.sock", DockerAPIVersion, httpClient, nil)
	if err != nil {
		return fmt.Errorf("Can't initiate NewClient: %v", err)
	}
	info, err := h.DClient.Info(context.Background())
	if err != nil {
		return fmt.Errorf("Can't retrieve Docker Info: %v", err)
	}
	logrus.Debugf("Docker Info found: %#v", info)
	isvalid, err := docker.IsSupportedDockerVersion(info, K8sVersion)
	if err != nil {
		return fmt.Errorf("Error while determining supported Docker version [%s]: %v", info.ServerVersion, err)
	}

	if !isvalid && h.EnforceDockerVersion {
		return fmt.Errorf("Unsupported Docker version found [%s], supported versions are %v", info.ServerVersion, docker.K8sDockerVersions[K8sVersion])
	} else if !isvalid {
		logrus.Warnf("Unsupported Docker version found [%s], supported versions are %v", info.ServerVersion, docker.K8sDockerVersions[K8sVersion])
	}

	return nil
}

func parsePrivateKey(keyBuff string) (ssh.Signer, error) {
	return ssh.ParsePrivateKey([]byte(keyBuff))
}

func parsePrivateKeyWithPassPhrase(keyBuff string, passphrase []byte) (ssh.Signer, error) {
	return ssh.ParsePrivateKeyWithPassphrase([]byte(keyBuff), passphrase)
}

func makeSSHConfig(user string, signer ssh.Signer) (*ssh.ClientConfig, error) {
	config := ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	return &config, nil
}

func checkEncryptedKey(sshKey, sshKeyPath string) (ssh.Signer, error) {
	logrus.Debugf("[ssh] Checking private key")
	var err error
	var key ssh.Signer
	if len(sshKey) > 0 {
		key, err = parsePrivateKey(sshKey)
	} else {
		key, err = parsePrivateKey(privateKeyPath(sshKeyPath))
	}
	if err == nil {
		return key, nil
	}

	// parse encrypted key
	if strings.Contains(err.Error(), "decode encrypted private keys") {
		fmt.Printf("Passphrase for Private SSH Key: ")
		passphrase, err := terminal.ReadPassword(int(syscall.Stdin))
		fmt.Printf("\n")
		if err != nil {
			return nil, err
		}
		if len(sshKey) > 0 {
			key, err = parsePrivateKeyWithPassPhrase(sshKey, passphrase)
		} else {
			key, err = parsePrivateKeyWithPassPhrase(privateKeyPath(sshKeyPath), passphrase)
		}
		if err != nil {
			return nil, err
		}
	}
	return key, err
}

func privateKeyPath(sshKeyPath string) string {
	if sshKeyPath[:2] == "~/" {
		sshKeyPath = filepath.Join(os.Getenv("HOME"), sshKeyPath[2:])
	}
	buff, _ := ioutil.ReadFile(sshKeyPath)
	return string(buff)
}
