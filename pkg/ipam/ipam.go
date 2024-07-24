package ipam

import (
	"errors"
	"fmt"
	"log"
	"net"

	cip "github.com/containernetworking/plugins/pkg/ip"
	"github.com/gitlayzer/raccoon/pkg/config"
	"github.com/gitlayzer/raccoon/pkg/store"
)

var (
	IPOverflowError = "IP地址已用完"
)

// IPAddressManagement 是IP地址管理器
type IPAddressManagement struct {
	subnet  *net.IPNet   // 子网掩码
	gateway net.IP       // 网关
	store   *store.Store // 存储器
}

// NewIpAddressManagement 创建一个新的IP地址管理器
func NewIPAddressManagement(c *config.CNIConfig, s *store.Store) (*IPAddressManagement, error) {
	_, ipSet, err := net.ParseCIDR(c.Subnet)
	if err != nil {
		return nil, err
	}

	im := &IPAddressManagement{
		subnet: ipSet,
		store:  s,
	}

	// 获取网关IP地址
	im.gateway, err = im.NextIP(im.subnet.IP)
	if err != nil {
		return nil, err
	}

	return im, nil
}

// Mask 获取子网掩码
func (im *IPAddressManagement) Mask() net.IPMask {
	return im.subnet.Mask
}

// Gateway 获取网关
func (im *IPAddressManagement) Gateway() net.IP {
	return im.gateway
}

// IpNet 获取IP网段
func (im *IPAddressManagement) IpNet(ip net.IP) *net.IPNet {
	return &net.IPNet{IP: ip, Mask: im.Mask()}
}

// NextIP 获取下一个可用的IP地址
func (im *IPAddressManagement) NextIP(ip net.IP) (net.IP, error) {
	next := cip.NextIP(ip)
	if !im.subnet.Contains(next) {
		return nil, errors.New(IPOverflowError)
	}

	return next, nil
}

// AllocateIP 分配IP地址
func (im *IPAddressManagement) AllocateIP(id, ifName string) (net.IP, error) {
	// 加锁
	im.store.Lock()
	// 解锁
	defer im.store.Unlock()

	// 从本地数据中获取IP地址
	if err := im.store.LocalData(); err != nil {
		return nil, err
	}

	// 先尝试获取已分配的IP地址
	ip, _ := im.store.GetIPByContainerID(id)
	if len(ip) > 0 {
		// 已分配，直接返回
		return ip, nil
	}

	// 获取最后一个IP地址
	last := im.store.Last()
	if len(last) == 0 {
		// 将 gateway 作为第一个IP地址
		last = im.gateway
	}

	// 复制IP地址
	start := make(net.IP, len(last))
	copy(start, last)

	// 计算下一个IP地址
	for {
		next, err := im.NextIP(start)
		if errors.Is(errors.New(IPOverflowError), err) && !last.Equal(im.gateway) {
			start = im.gateway
			continue
		} else if err != nil {
			return nil, err
		}

		// 检查 IP 是否是未分配的
		if !im.store.Contain(next) {
			err = im.store.Add(next, id, ifName) // 调用存储器添加IP地址
			return next, err
		}

		start = next // 继续搜索下一个IP地址

		// 已经遍历完所有IP地址, 则跳出循环
		if start.Equal(last) {
			break
		}

		log.Printf("IP Address: %s", next)
	}

	return nil, fmt.Errorf("no available IP address")
}

// ReleaseIP 释放IP地址
func (im *IPAddressManagement) ReleaseIP(id string) error {
	im.store.Lock()
	defer im.store.Unlock()

	if err := im.store.LocalData(); err != nil {
		return err
	}

	// 从存储中删除IP地址
	return im.store.Del(id)
}

// CheckIP 检查IP地址是否可用
func (im *IPAddressManagement) CheckIP(id string) (net.IP, error) {
	im.store.Lock()
	defer im.store.Unlock()

	if err := im.store.LocalData(); err != nil {
		return nil, err
	}

	ip, ok := im.store.GetIPByContainerID(id)
	if !ok {
		return nil, fmt.Errorf("failed to find container %s ip address", id)
	}

	return ip, nil
}
