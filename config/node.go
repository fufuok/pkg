package config

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/fufuok/pkg/myip"
	"github.com/fufuok/pkg/pools/timerpool"
	"github.com/fufuok/pkg/utils"
	"github.com/imroc/req/v3"

	"github.com/fufuok/pkg/json"
)

var (
	// NodeInfoFile 配置中指定的节点基本信息配置文件路径
	NodeInfoFile string

	// NodeInfoBackupFile 节点基本信息备份文件路径
	NodeInfoBackupFile string

	// NodeInfoBackupEnabledEnvName 节点信息备份功能开关环境变量名
	// 节点身份信息要求实时准确, 默认关闭: 不使用上次保存的 backup 回退, 也不回写 backup 文件.
	// 仅在明确设置 NODE_INFO_BACKUP_ENABLED=1/true 时, 启用 backup 读写兜底 (兼容旧行为).
	NodeInfoBackupEnabledEnvName = "NODE_INFO_BACKUP_ENABLED"

	nodeIPFetcherRunning bool

	// NodeIPFromAPI 已获取成功的节点 IP
	NodeIPFromAPI = ""

	// nodeIPFetcherSleep 控制重试间隔.
	// 默认 time.Sleep; 包内测试可替换, 避免 10s 起步等待, 不支持并行改写.
	nodeIPFetcherSleep = time.Sleep
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
	}

	// 首选: 加载节点本地配置文件: node_info.json
	if err := parseNodeInfoJSON(cfg); err != nil && !errors.Is(err, os.ErrNotExist) {
		_, _ = fmt.Fprintf(os.Stderr, "Warning: Load node info file failed: %v\n", err)
	}

	// 次选: 加载上次保存的有效节点配置文件: etc/node_info.backup (默认关闭, 需环境变量显式启用)
	backupEnabled := nodeInfoBackupEnabled()
	if backupEnabled && cfg.NodeConf.NodeInfo.NodeIP == "" {
		if err := parseNodeInfoJSONBackup(cfg); err != nil && !errors.Is(err, os.ErrNotExist) {
			_, _ = fmt.Fprintf(os.Stderr, "Warning: Load node info backup file failed: %v\n", err)
		}
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

	// 备份节点配置 (默认关闭, 需环境变量显式启用)
	if backupEnabled {
		saveNodeInfoBackup(cfg.NodeConf.NodeInfo)
	}
}

// nodeInfoBackupEnabled 是否启用节点信息备份读写兜底 (默认关闭)
func nodeInfoBackupEnabled() bool {
	enabled, _ := strconv.ParseBool(os.Getenv(NodeInfoBackupEnabledEnvName))
	return enabled
}

// parseNodeInfoJSON 只加载跨项目通用的节点基础字段.
// 项目专用扩展字段由 JSON 解码器忽略; 解码失败时不发布部分结果.
func parseNodeInfoJSON(cfg *MainConf) error {
	NodeInfoFile = cfg.NodeConf.NodeInfoFile
	if NodeInfoFile == "" {
		return nil
	}
	body, err := os.ReadFile(NodeInfoFile)
	if err != nil {
		return fmt.Errorf("read node info file %q: %w", NodeInfoFile, err)
	}

	nInfo := cfg.NodeConf.NodeInfo
	hostname, hostIP := nInfo.Hostname, nInfo.HostIP
	if err := json.Unmarshal(body, &nInfo); err != nil {
		return fmt.Errorf("parse node info file %q: %w", NodeInfoFile, err)
	}
	nInfo.Hostname, nInfo.HostIP = hostname, hostIP
	cfg.NodeConf.NodeInfo = nInfo
	return nil
}

// parseNodeInfoJSONBackup 严格加载当前 NodeInfo 契约的备份数据.
// 读取或解码失败时不发布部分结果, 由调用方决定是否记录错误.
func parseNodeInfoJSONBackup(cfg *MainConf) error {
	body, err := os.ReadFile(NodeInfoBackupFile)
	if err != nil {
		return fmt.Errorf("read node info backup file %q: %w", NodeInfoBackupFile, err)
	}
	var nInfo NodeInfo
	if err := json.Unmarshal(body, &nInfo); err != nil {
		return fmt.Errorf("parse node info backup file %q: %w", NodeInfoBackupFile, err)
	}
	cfg.NodeConf.NodeInfo = nInfo
	return nil
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
			// Clone 只拷当时默认客户端, 再覆盖为短超时; 本次探测不跟随后续 loadReq 热更新.
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
		nodeIPFetcherSleep(time.Duration(i*10) * time.Second)

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

		// 从 IPAPI 获取到出口 IP, 发布新配置指针, 不改已发布的 MainConf.
		NodeIPFromAPI = nodeIP.String()
		cfg := mainConf.Load()
		if cfg == nil {
			return
		}
		next := *cfg
		next.NodeConf.NodeInfo.NodeIP = NodeIPFromAPI
		mainConf.Store(&next)
		return
	}
}
