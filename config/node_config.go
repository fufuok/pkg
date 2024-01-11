package config

import (
	"net"
	"os"
	"strings"

	"github.com/fufuok/utils/myip"
	"github.com/imroc/req/v3"

	"github.com/fufuok/pkg/json"
)

const (
	// UnknownNodeType 未知节点类型
	UnknownNodeType = -1
)

var (
	NodeInfoFile = ""
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

// 最新节点信息
func setNodeInfo(nConf *NodeConf) error {
	// 主机名和内网 IPv4, 优先取第一个网卡的 IPv4
	hostname, _ := os.Hostname()
	hostIP := myip.LocalIP("lo")
	if hostIP == "" {
		hostIP = myip.InternalIPv4()
	}

	// 初始化节点信息
	nConf.NodeInfo = NodeInfo{
		Hostname: hostname,
		HostIP:   hostIP,
		NodeType: UnknownNodeType,
	}

	// 加载节点本地文件: node_info.json
	NodeInfoFile = nConf.NodeInfoFile
	if NodeInfoFile != "" {
		if body, err := os.ReadFile(NodeInfoFile); err == nil {
			var nInfo nodeInfoFileData
			if err := json.Unmarshal(body, &nInfo); err == nil {
				nConf.NodeInfo.NodeID = nInfo.NodeID
				nConf.NodeInfo.NodeIP = nInfo.NodeIP
				nConf.NodeInfo.NodeName = nInfo.NodeName
				nConf.NodeInfo.NodeDesc = nInfo.NodeDesc
				if nInfo.InterNodeType != "" {
					// 新配置文件, 使用标准节点类型字段值: node_base.node_type
					nConf.NodeInfo.NodeType = nInfo.InterNodeCode
				} else {
					// 旧配置, 根据 inter_node 推导节点类型
					switch nInfo.InterNode {
					case "普通节点":
						nConf.NodeInfo.NodeType = 0
					case "海外版_国内用户接入":
						nConf.NodeInfo.NodeType = 2
					case "海外版_海外用户接入":
						nConf.NodeInfo.NodeType = 4
					}
				}
			}
		}
	}

	// 节点 IP 为空时, 以出口 IP 作为节点 IP
	if nConf.NodeInfo.NodeIP == "" && nConf.IPAPI != "" {
		resp, err := req.DefaultClient().Clone().SetTimeout(ReqTimeoutShortDuration).R().Get(nConf.IPAPI)
		if err == nil && resp.IsSuccessState() {
			nConf.NodeInfo.NodeIP = strings.TrimSpace(resp.String())
		}
	}

	// 确保节点 IP 格式正确
	nodeIP := net.ParseIP(nConf.NodeInfo.NodeIP)
	if nodeIP == nil {
		nodeIP = net.IPv4zero
	}
	nConf.NodeInfo.NodeIP = nodeIP.String()
	return nil
}
