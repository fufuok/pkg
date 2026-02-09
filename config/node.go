package config

import (
	"context"
	"net"
	"os"
	"strings"
	"time"

	"github.com/fufuok/utils"
	"github.com/fufuok/utils/myip"
	"github.com/fufuok/utils/pools/timerpool"
	"github.com/imroc/req/v3"

	"github.com/fufuok/pkg/json"
)

// UnknownNodeType 未知节点类型
const UnknownNodeType = -1

var (
	// NodeInfoFile 配置中指定的节点基本信息配置文件路径
	NodeInfoFile string

	// NodeInfoBackupFile 节点基本信息备份文件路径
	NodeInfoBackupFile string

	nodeIPFetcherRunning bool

	// NodeIPFromAPI 已获取成功的节点 IP
	NodeIPFromAPI = ""
)

type NodeConf struct {
	NodeInfoFile string `json:"node_info_file"`
	IPAPI        string `json:"ip_api"`
	NodeInfo     NodeInfo
}

// NodeInfo 节点信息
type NodeInfo struct {
	// 主机名和网卡 IP
	Hostname string `json:"hostname"`
	HostIP   string `json:"host_ip"`

	NodeID   int    `json:"node_id"`
	NodeIP   string `json:"service_ip"`
	NodeName string `json:"node_name"`
	NodeDesc string `json:"node_desc"`
	NodeType int    `json:"node_type"`
}

// 用于加载 node_info.json 文件内容
type nodeInfoFileData struct {
	NodeID        int    `json:"node_id"`
	NodeIP        string `json:"service_ip"`
	NodeName      string `json:"node_name"`
	NodeDesc      string `json:"node_desc"`
	InterNode     string `json:"inter_node"`
	InterNodeType string `json:"inter_node_type"`
	InterNodeCode int    `json:"inter_node_code"`
}

// 解析节点信息
func parseNodeInfoConfig(cfg *MainConf) {
	// 主机名和内网 IPv4, 优先取第一个网卡的 IPv4
	hostname, _ := os.Hostname()
	hostIP := myip.LocalIP("lo")
	if hostIP == "" {
		hostIP = myip.InternalIPv4()
	}

	// 初始化节点信息
	cfg.NodeConf.NodeInfo = NodeInfo{
		Hostname: hostname,
		HostIP:   hostIP,
		NodeType: UnknownNodeType,
	}

	// 首选: 加载节点本地配置文件: node_info.json
	parseNodeInfoJson(cfg)

	// 次选: 加载上次保存的有效节点配置文件: etc/node_info.backup
	if cfg.NodeConf.NodeInfo.NodeIP == "" {
		parseNodeInfoJsonBackup(cfg)
	}

	// 节点 IP 为空时, 以出口 IP 作为节点 IP
	ip := cfg.NodeConf.NodeInfo.NodeIP
	if ip == "" && cfg.NodeConf.IPAPI != "" {
		ip = GetNodeIPFromAPIs(cfg.NodeConf.IPAPI)
		if ip == "" {
			// 节点出口 IP 没有获取成功时, 使用已保存的 NodeIP
			ip = NodeIPFromAPI
			go nodeIPFetcher(cfg.NodeConf.IPAPI)
		}
	}

	// 确保节点 IP 格式正确
	nodeIP := net.ParseIP(ip)
	if nodeIP == nil {
		nodeIP = net.IPv4zero
	}
	cfg.NodeConf.NodeInfo.NodeIP = nodeIP.String()

	// 备份节点配置
	saveNodeInfoBackup(cfg.NodeConf.NodeInfo)
}

func parseNodeInfoJson(cfg *MainConf) {
	NodeInfoFile = cfg.NodeConf.NodeInfoFile
	if NodeInfoFile == "" {
		return
	}
	body, err := os.ReadFile(NodeInfoFile)
	if err != nil {
		return
	}
	var nInfo nodeInfoFileData
	if err := json.Unmarshal(body, &nInfo); err != nil {
		return
	}
	cfg.NodeConf.NodeInfo.NodeID = nInfo.NodeID
	cfg.NodeConf.NodeInfo.NodeIP = nInfo.NodeIP
	cfg.NodeConf.NodeInfo.NodeName = nInfo.NodeName
	cfg.NodeConf.NodeInfo.NodeDesc = nInfo.NodeDesc
	if nInfo.InterNodeType != "" {
		// 新配置文件, 使用标准节点类型字段值: node_base.node_type
		cfg.NodeConf.NodeInfo.NodeType = nInfo.InterNodeCode
	} else {
		// 旧配置, 根据 inter_node 推导节点类型
		switch nInfo.InterNode {
		case "普通节点":
			cfg.NodeConf.NodeInfo.NodeType = 0
		case "海外版_国内用户接入":
			cfg.NodeConf.NodeInfo.NodeType = 2
		case "海外版_海外用户接入":
			cfg.NodeConf.NodeInfo.NodeType = 4
		}
	}
}

func parseNodeInfoJsonBackup(cfg *MainConf) {
	body, err := os.ReadFile(NodeInfoBackupFile)
	if err != nil {
		return
	}
	var nInfo NodeInfo
	if err := json.Unmarshal(body, &nInfo); err != nil {
		return
	}
	cfg.NodeConf.NodeInfo = nInfo
}

func saveNodeInfoBackup(info NodeInfo) {
	if info.NodeIP != net.IPv4zero.String() {
		_ = os.WriteFile(NodeInfoBackupFile, json.MustJSON(info), 0o600)
	}
}

// GetNodeIPFromAPIs 同时请求多个 API, 返回 IP 结果
func GetNodeIPFromAPIs(ipapi string, timeout ...time.Duration) string {
	dur := ReqTimeoutShortDuration
	if len(timeout) > 0 {
		dur = timeout[0]
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	apis := utils.TrimSlice(strings.Split(ipapi, ","))
	ipChan := make(chan string, len(apis))
	for _, api := range apis {
		go func() {
			resp, err := req.DefaultClient().Clone().SetTimeout(dur).R().SetContext(ctx).Get(api)
			if err == nil && resp.IsSuccessState() {
				ip := strings.TrimSpace(resp.String())
				if utils.IsIP(ip) {
					ipChan <- ip
				}
			}
		}()
	}

	timer := timerpool.New(dur)
	defer timerpool.Release(timer)
	select {
	case ip := <-ipChan:
		return ip
	case <-timer.C:
	}
	return ""
}

// 尝试多次获取出口 IP, 填充到 NodeIP
func nodeIPFetcher(api string) {
	if nodeIPFetcherRunning {
		return
	}

	nodeIPFetcherRunning = true
	defer func() {
		nodeIPFetcherRunning = false
	}()

	for i := 1; i <= 10; i++ {
		time.Sleep(time.Duration(i*10) * time.Second)

		if Config().NodeConf.NodeInfo.NodeIP != net.IPv4zero.String() {
			NodeIPFromAPI = Config().NodeConf.NodeInfo.NodeIP
			return
		}

		// 优先使用新配置的接口地址
		if Config().NodeConf.IPAPI != "" {
			api = Config().NodeConf.IPAPI
		}
		ip := GetNodeIPFromAPIs(api, ReqTimeoutDuration)
		if ip == "" {
			continue
		}

		nodeIP := net.ParseIP(ip)
		if nodeIP == nil {
			continue
		}

		// 从 IPAPI 获取到出口 IP, 更新到全局配置项
		NodeIPFromAPI = nodeIP.String()
		cfg := mainConf.Load()
		cfg.NodeConf.NodeInfo.NodeIP = NodeIPFromAPI
		mainConf.Store(cfg)
		return
	}
}
