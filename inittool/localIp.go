//工具类

package inittool

import (
	"log"
	"net"
)

// getLocalIP 获取本机可用的IPv4地址
func GetLocalIP() string {
    interfaces, err := net.Interfaces()
    if err != nil {
        log.Fatal("获取网络接口失败:", err)
    }

    log.Println("正在查找网络接口...")
    // 首先尝试查找 Windows 的 WLAN 或 WSL 的网络接口
    for _, iface := range interfaces {
        log.Printf("检查接口: %s, 标志: %v", iface.Name, iface.Flags)
        // 检查是否为活动接口
        if iface.Flags&net.FlagUp != 0 && iface.Flags&net.FlagLoopback == 0 {
            addrs, err := iface.Addrs()
            if err != nil {
                log.Printf("获取地址失败: %v", err)
                continue
            }
            
            for _, addr := range addrs {
                if ipnet, ok := addr.(*net.IPNet); ok {
                    if ip4 := ipnet.IP.To4(); ip4 != nil {
                        if !ip4.IsLoopback() && !ip4.IsUnspecified() {
                            // 优先选择 192.168.73.x 网段（WLAN）
                            if ip4[0] == 192 && ip4[1] == 168 && ip4[2] == 73 {
                                log.Printf("使用WLAN地址: %s (来自接口: %s)", ip4.String(), iface.Name)
                                return ip4.String()
                            }
                        }
                    }
                }
            }
        }
    }

    // 如果没有找到 WLAN 地址，使用第一个可用的地址
    for _, iface := range interfaces {
        if iface.Flags&net.FlagUp != 0 && iface.Flags&net.FlagLoopback == 0 {
            addrs, err := iface.Addrs()
            if err != nil {
                continue
            }

            for _, addr := range addrs {
                if ipnet, ok := addr.(*net.IPNet); ok {
                    if ip4 := ipnet.IP.To4(); ip4 != nil {
                        if !ip4.IsLoopback() && !ip4.IsUnspecified() {
                            log.Printf("使用备选地址: %s (来自接口: %s)", ip4.String(), iface.Name)
                            return ip4.String()
                        }
                    }
                }
            }
        }
    }

    log.Println("未找到任何可用的IPv4地址，使用localhost")
    return "127.0.0.1"
}