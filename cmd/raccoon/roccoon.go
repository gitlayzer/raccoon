package main

import (
	"fmt"
	"net"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	current "github.com/containernetworking/cni/pkg/types/100"
	"github.com/containernetworking/cni/pkg/version"
	"github.com/containernetworking/plugins/pkg/ns"
	bv "github.com/containernetworking/plugins/pkg/utils/buildversion"
	"github.com/gitlayzer/raccoon/pkg/bridge"
	"github.com/gitlayzer/raccoon/pkg/config"
	"github.com/gitlayzer/raccoon/pkg/ipam"
	"github.com/gitlayzer/raccoon/pkg/store"
)

const (
	pluginName = "raccoon"
)

func main() {
	skel.PluginMain(cmdAdd, cmdCheck, cmdDel, version.All, bv.BuildString(pluginName))
}

// 实现 cmdAdd 函数
func cmdAdd(args *skel.CmdArgs) error {
	// 加载配置文件
	c, err := config.LoadCNIConfig(args.StdinData)
	if err != nil {
		return err
	}

	// 获取存储器
	s, err := store.NewStore(c.DataDir, c.Name)
	if err != nil {
		return err
	}
	defer s.Close()

	// 创建 IPAM 管理器
	ipam, err := ipam.NewIPAddressManagement(c, s)
	if err != nil {
		return fmt.Errorf("failed to create IPAM manager: %v", err)
	}

	gateway := ipam.Gateway()

	// 获取分配的 IP 地址
	ip, err := ipam.AllocateIP(args.ContainerID, args.IfName)
	if err != nil {
		return fmt.Errorf("failed to allocate IP address: %v", err)
	}

	mtu := 1500

	br, err := bridge.CreateBridge(c.Bridge, mtu, *ipam.IpNet(gateway))
	if err != nil {
		return fmt.Errorf("failed to create bridge: %v", err)
	}

	netns, err := ns.GetNS(args.Netns)
	if err != nil {
		return fmt.Errorf("failed to open netns: %v", err)
	}

	defer netns.Close()

	if err := bridge.SetupVethPair(netns, br, mtu, args.IfName, ipam.IpNet(ip), gateway); err != nil {
		return fmt.Errorf("failed to setup veth pair: %v", err)
	}

	result := &current.Result{
		CNIVersion: current.ImplementedSpecVersion,
		IPs: []*current.IPConfig{
			{
				Address: net.IPNet{IP: ip, Mask: ipam.Mask()},
				Gateway: gateway,
			},
		},
	}

	return types.PrintResult(result, c.CNIVersion)
}

// 实现 cmdDel 函数
func cmdDel(args *skel.CmdArgs) error {
	c, err := config.LoadCNIConfig(args.StdinData)
	if err != nil {
		return err
	}

	s, err := store.NewStore(c.DataDir, c.Name)
	if err != nil {
		return err
	}
	defer s.Close()

	ipam, err := ipam.NewIPAddressManagement(c, s)
	if err != nil {
		return fmt.Errorf("failed to create IPAM manager: %v", err)
	}

	if err := ipam.ReleaseIP(args.ContainerID); err != nil {
		return fmt.Errorf("failed to release IP address: %v", err)
	}

	netns, err := ns.GetNS(args.Netns)
	if err != nil {
		return fmt.Errorf("failed to open netns: %v", err)
	}
	defer netns.Close()

	return bridge.DelVethPair(netns, args.IfName)
}

// 实现 cmdCheck 函数
func cmdCheck(args *skel.CmdArgs) error {
	c, err := config.LoadCNIConfig(args.StdinData)
	if err != nil {
		return err
	}

	s, err := store.NewStore(c.DataDir, c.Name)
	if err != nil {
		return err
	}
	defer s.Close()

	ipam, err := ipam.NewIPAddressManagement(c, s)
	if err != nil {
		return fmt.Errorf("failed to create IPAM manager: %v", err)
	}

	ip, err := ipam.CheckIP(args.ContainerID)
	if err != nil {
		return fmt.Errorf("failed to check IP address: %v", err)
	}

	netns, err := ns.GetNS(args.Netns)
	if err != nil {
		return fmt.Errorf("failed to open netns: %v", err)
	}
	defer netns.Close()

	return bridge.CheckVethPair(netns, args.IfName, ip)
}
