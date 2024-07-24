package bridge

import (
	"fmt"
	"net"
	"os"

	current "github.com/containernetworking/cni/pkg/types/100"
	"github.com/containernetworking/plugins/pkg/ip"
	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/vishvananda/netlink"
)

// CreateBridge 创建一个桥接设备
func CreateBridge(bridge string, mtu int, gateway net.IPNet) (netlink.Link, error) {
	// 检查是否存在同名桥接
	if l, _ := netlink.LinkByName(bridge); l != nil {
		return l, nil
	}

	br := &netlink.Bridge{
		LinkAttrs: netlink.LinkAttrs{
			Name:   bridge, // 设定桥接名称
			MTU:    mtu,    // 设置MTU
			TxQLen: -1,     // 设置队列长度
		},
	}

	// 创建桥接
	if err := netlink.LinkAdd(br); err != nil {
		return nil, err
	}

	// 获取桥接设备
	dev, err := netlink.LinkByName(bridge)
	if err != nil {
		return nil, err
	}

	// 设置网关
	if err = netlink.AddrAdd(dev, &netlink.Addr{IPNet: &gateway}); err != nil {
		return nil, err
	}

	// 启动桥接
	if err = netlink.LinkSetUp(dev); err != nil {
		return nil, err
	}

	return dev, nil
}

// SetupVethPair 创建一个 veth pair
func SetupVethPair(netns ns.NetNS, br netlink.Link, mtu int, ifName string, podIP *net.IPNet, gateway net.IP) error {
	hostInterface := &current.Interface{}

	err := netns.Do(func(hostNS ns.NetNS) error {
		hostVeth, containerVeth, err := ip.SetupVeth(ifName, mtu, "", hostNS)
		if err != nil {
			return err
		}
		hostInterface.Name = hostVeth.Name

		conLink, err := netlink.LinkByName(containerVeth.Name)
		if err != nil {
			return err
		}
		if err := netlink.AddrAdd(conLink, &netlink.Addr{IPNet: podIP}); err != nil {
			return err
		}

		if err = netlink.LinkSetUp(conLink); err != nil {
			return err
		}

		if err = ip.AddDefaultRoute(gateway, conLink); err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		return err
	}

	hostVeth, err := netlink.LinkByName(hostInterface.Name)
	if err != nil {
		return fmt.Errorf("failed to lookup %q: %v", hostInterface.Name, err)
	}

	if hostVeth == nil {
		return fmt.Errorf("nil hostveth")
	}

	if err = netlink.LinkSetMaster(hostVeth, br); err != nil {
		return fmt.Errorf("failed to connect %q to bridge %v: %v", hostVeth.Attrs().Name, br.Attrs().Name, err)
	}

	return nil
}

// DelVethPair 删除一个 veth pair
func DelVethPair(netns ns.NetNS, ifName string) error {
	return netns.Do(func(ns.NetNS) error {
		l, err := netlink.LinkByName(ifName)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		return netlink.LinkDel(l)
	})
}

// CheckVethPair 检查一个 veth pair 是否存在
func CheckVethPair(netns ns.NetNS, ifName string, ip net.IP) error {
	return netns.Do(func(ns.NetNS) error {
		l, err := netlink.LinkByName(ifName)
		if err != nil {
			return err
		}

		ips, err := netlink.AddrList(l, netlink.FAMILY_V4)
		if err != nil {
			return err
		}

		for _, addr := range ips {
			if addr.IP.Equal(ip) {
				return nil
			}
		}

		return fmt.Errorf("failed to find ip %s for %s", ip, ifName)
	})
}
