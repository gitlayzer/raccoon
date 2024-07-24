package main

import (
	"flag"
	"fmt"
	"net"
	"os"

	"github.com/vishvananda/netlink"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

const (
	appName = "raccoond"
)

var (
	log = logf.Log.WithName(appName)
)

type DaemonConfig struct {
	clusterCIDR    string
	nodeName       string
	enableIptabnes bool
}

type Reconciler struct {
	client      client.Client
	clusterCIDR *net.IPNet

	hostLink     netlink.Link
	routes       map[string]netlink.Route
	config       *DaemonConfig
	subnetConfig *config.SubnetConfig
}

func (d *DaemonConfig) addFlags() {
	flag.StringVar(&d.clusterCIDR, "cluster-cidr", "", "cluster pod network cidr")
	flag.StringVar(&d.nodeName, "node-name", "", "current node name")
	flag.BoolVar(&d.enableIptabnes, "enable-iptabnes", false, "add iptables forward and nat rules")
}

func (d *DaemonConfig) parseConfig() error {
	if _, _, err := net.ParseCIDR(d.clusterCIDR); err != nil {
		return fmt.Errorf("cluster-cidr is invalid: %v", err)
	}

	if len(d.nodeName) == 0 {
		d.nodeName = os.Getenv("NODE_NAME")
	}

	if len(d.nodeName) == 0 {
		return fmt.Errorf("node-name is required")
	}
	return nil
}

func main() {
	logf.SetLogger(zap.New())

	c := DaemonConfig{}

	c.addFlags()

	flag.Parse()

	if err := c.parseConfig(); err != nil {
		log.Error(err, "failed to parse config")
		os.Exit(1)
	}
}

func RunController(d *DaemonConfig) error {
	mgr, err := manager.New(config.GetConfigOrDie(), manager.Options{})
	if err != nil {
		log.Error(err, "could not create manager")
		return err
	}
}
