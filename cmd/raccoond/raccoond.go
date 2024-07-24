package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"net"
	"os"

	"github.com/coreos/go-iptables/iptables"
	"github.com/gitlayzer/raccoon/pkg/bridge"
	raccoonConf "github.com/gitlayzer/raccoon/pkg/config"
	"github.com/vishvananda/netlink"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/event"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	corev1 "k8s.io/api/core/v1"
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
	enableIptables bool
}

type Reconciler struct {
	client      client.Client
	clusterCIDR *net.IPNet

	hostLink     netlink.Link
	routes       map[string]netlink.Route
	config       *DaemonConfig
	subnetConfig *raccoonConf.SubnetConfig
}

func (d *DaemonConfig) addFlags() {
	flag.StringVar(&d.clusterCIDR, "cluster-cidr", "", "cluster pod network cidr")
	flag.StringVar(&d.nodeName, "node-name", "", "current node name")
	flag.BoolVar(&d.enableIptables, "enable-iptables", false, "add iptables forward and nat rules")
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

	reconciler, err := NewReconciler(d, mgr)
	if err != nil {
		return err
	}
	log.Info("create reconciler success")

	err = builder.
		ControllerManagedBy(mgr).
		For(&corev1.Node{}).
		WithEventFilter(predicate.Funcs{
			UpdateFunc: func(e event.UpdateEvent) bool {
				old, ok := e.ObjectOld.(*corev1.Node)
				if !ok {
					return true
				}
				new, ok := e.ObjectNew.(*corev1.Node)
				if !ok {
					return true
				}
				return old.Spec.PodCIDR != new.Spec.PodCIDR
			},
		}).
		Complete(reconciler)
	if err != nil {
		log.Error(err, "could not create controller")
		return err
	}

	return mgr.Start(signals.SetupSignalHandler())
}

func NewReconciler(d *DaemonConfig, mgr manager.Manager) (*Reconciler, error) {
	_, cidr, err := net.ParseCIDR(d.clusterCIDR)
	if err != nil {
		return nil, err
	}

	node := &corev1.Node{}
	if err := mgr.GetAPIReader().Get(context.TODO(), types.NamespacedName{Name: d.nodeName}, node); err != nil {
		return nil, err
	}

	hostIP, err := getNodeInternalIP(node)
	if err != nil {
		return nil, fmt.Errorf("failed to get host ip for node %s", d.nodeName)
	}

	_, nodeCIDR, err := net.ParseCIDR(node.Spec.PodCIDR)
	if err != nil {
		return nil, err
	}

	log.Info("get nodeinfo", "host ip", hostIP.String(), "node cidr", nodeCIDR.String())

	subnetConf := &raccoonConf.SubnetConfig{
		Subnet: nodeCIDR.String(),
		Bridge: raccoonConf.DefaultBridgeName,
	}
	if err := raccoonConf.StoreSubnetConfig(subnetConf); err != nil {
		return nil, err
	}

	var hostLink netlink.Link
	linkList, err := netlink.LinkList()
	if err != nil {
		return nil, err
	}
Loop:
	for _, link := range linkList {
		if link.Attrs() != nil {
			addrs, err := netlink.AddrList(link, netlink.FAMILY_V4)
			if err != nil {
				return nil, err
			}
			for _, addr := range addrs {
				if addr.IP.Equal(hostIP) {
					hostLink = link
					break Loop
				}
			}
		}
	}
	if hostLink == nil {
		return nil, fmt.Errorf("failed to get host link device")
	}
	log.Info(fmt.Sprintf("get hostlink success, type: %s, name: %s, index: %d", hostLink.Type(), hostLink.Attrs().Name, hostLink.Attrs().Index))

	_, err = bridge.CreateBridge(subnetConf.Bridge, 1500, net.IPNet{})

	if d.enableIptables {
		if err := addIptables(subnetConf.Bridge, hostLink.Attrs().Name, subnetConf.Subnet); err != nil {
			return nil, err
		}
		log.Info("set iptables success")
	}

	routes := make(map[string]netlink.Route)
	routeList, err := netlink.RouteList(hostLink, netlink.FAMILY_V4)
	for _, route := range routeList {
		if route.Dst != nil && !route.Dst.IP.Equal(nodeCIDR.IP) && cidr.Contains(route.Dst.IP) {
			routes[route.Dst.String()] = route
		}
	}
	log.Info("get local routes", "routes", routes)

	return &Reconciler{
		client:       mgr.GetClient(),
		clusterCIDR:  cidr,
		hostLink:     hostLink,
		routes:       routes,
		config:       d,
		subnetConfig: subnetConf,
	}, nil
}

func (r *Reconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	log.Info("start reconcile", "key", req.NamespacedName.Name)
	result := reconcile.Result{}
	nodes := &corev1.NodeList{}
	if err := r.client.List(ctx, nodes); err != nil {
		return result, err
	}

	cidrs := make(map[string]netlink.Route)
	for _, node := range nodes.Items {
		if node.Name == r.config.nodeName {
			continue
		}

		if len(node.Spec.PodCIDR) == 0 {
			continue
		}

		_, cidr, err := net.ParseCIDR(node.Spec.PodCIDR)
		if err != nil {
			return result, err
		}

		nodeip, err := getNodeInternalIP(&node)
		if err != nil {
			log.Error(err, "failed to get host")
			continue
		}
		route := netlink.Route{
			Dst:        cidr,
			Gw:         nodeip,
			ILinkIndex: r.hostLink.Attrs().Index,
		}
		cidrs[cidr.String()] = route

		if currentRoute, ok := r.routes[cidr.String()]; ok {
			if isRouteEqual(route, currentRoute) {
				continue
			}
			if err := r.ReplaceRoute(currentRoute); err != nil {
				return result, err
			}
		} else {
			if err := r.addRoute(route); err != nil {
				return result, err
			}
		}
	}

	for cidr, route := range r.routes {
		if _, ok := cidrs[cidr]; !ok {
			if err := r.delRoute(route); err != nil {
				return result, err
			}
		}
	}

	return result, nil
}

func (r *Reconciler) addRoute(route netlink.Route) (err error) {
	defer func() {
		if err == nil {
			r.routes[route.Dst.String()] = route
		}
	}()

	log.Info(fmt.Sprintf("add route: %s", route.String()))
	err = netlink.RouteAdd(&route)
	if err != nil {
		log.Error(err, "failed to add route", "route", route.String())
	}
	return
}

func (r *Reconciler) delRoute(route netlink.Route) (err error) {
	defer func() {
		if err == nil {
			delete(r.routes, route.Dst.String())
		}
	}()
	log.Info(fmt.Sprintf("del route: %s", route.String()))
	err = netlink.RouteDel(&route)
	return
}

func (r *Reconciler) ReplaceRoute(route netlink.Route) (err error) {
	defer func() {
		if err == nil {
			r.routes[route.Dst.String()] = route
		}
	}()
	log.Info(fmt.Sprintf("replace route: %s", route.String()))
	err = netlink.RouteReplace(&route)
	return
}

func addIptables(bridgeName, hostDeviceName, nodeCIDR string) error {
	ipt, err := iptables.NewWithProtocol(iptables.ProtocolIPv4)
	if err != nil {
		return err
	}

	if err := ipt.AppendUnique("filter", "FORWARD", "-i", bridgeName, "-j", "ACCEPT"); err != nil {
		return err
	}

	if err := ipt.AppendUnique("filter", "FORWARD", "-i", hostDeviceName, "-j", "ACCEPT"); err != nil {
		return err
	}

	if err := ipt.AppendUnique("nat", "POSTROUTING", "-s", nodeCIDR, "-j", "MASQUERADE"); err != nil {
		return err
	}

	return nil
}

func getNodeInternalIP(node *corev1.Node) (net.IP, error) {
	if node == nil {
		return nil, fmt.Errorf("empty node")
	}

	var ip net.IP
	for _, addr := range node.Status.Addresses {
		if addr.Type == corev1.NodeInternalIP {
			ip = net.ParseIP(addr.Address)
			break
		}
	}

	if len(ip) == 0 {
		return nil, fmt.Errorf("node %s ip is nil", node.Name)
	}

	return ip, nil
}

func isRouteEqual(x, y netlink.Route) bool {
	if x.Dst.IP.Equal(y.Dst.IP) && x.Gw.Equal(y.Gw) && bytes.Equal(x.Dst.Mask, y.Dst.Mask) && x.LinkIndex == y.LinkIndex {
		return true
	}
	return false
}
