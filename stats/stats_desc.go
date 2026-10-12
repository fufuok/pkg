package stats

// SYSStatsDesc 系统信息指标注释
func SYSStatsDesc() map[string]any {
	return map[string]any{
		"App": map[string]string{
			"AppName":    "应用名称",
			"Version":    "应用版本",
			"GitCommit":  "Git提交信息",
			"GoVersion":  "Go语言版本",
			"DebVersion": "Deb包版本",
			"JSON":       "使用的JSON库",
		},
		"Config": map[string]string{
			"LogLevel":       "日志级别",
			"ConfigModTime":  "配置文件修改时间",
			"ConfigLoadTime": "配置加载时间",
			"Debug":          "是否开启调试模式",
		},
		"Time": map[string]string{
			"Uptime":      "应用运行时间",
			"StartTime":   "应用启动时间",
			"ClockOffset": "时钟偏移量",
			"GlobalTime":  "全局时间(RFC3339Nano格式)",
			"LocalTime":   "本地时间",
		},
		"Host": map[string]string{
			"InternalIPv4":      "内网IPv4地址",
			"ExternalIPv4":      "外网IPv4地址",
			"NumCpus":           "CPU核心数",
			"LoadAvg":           "系统负载(1分钟, 5分钟, 15分钟)",
			"CPUPercent":        "系统CPU使用率, Linux为两次成功读取间均值, 窗口可超过30秒, 首次为0",
			"MemTotal":          "总内存大小",
			"MemAvailable":      "可用内存大小",
			"MemUsed":           "已使用内存估计值(总量减可用量)",
			"MemUsedPercent":    "内存使用百分比",
			"Load1":             "系统1分钟负载原始值",
			"Load5":             "系统5分钟负载原始值",
			"Load15":            "系统15分钟负载原始值",
			"MemTotalBytes":     "总内存字节数",
			"MemAvailableBytes": "可用内存字节数",
			"FileHandlesUsed":   "系统已使用文件句柄数",
			"FileHandlesLimit":  "系统文件句柄上限",
			"CollectedAt":       "主机资源本机采集时间, 最多30秒按需刷新",
			"ResourceErrors":    "主机资源采集失败项",
		},
	}
}

// MetricStatsDesc 主程序运行指标注释
func MetricStatsDesc() map[string]any {
	return map[string]any{
		"Main": map[string]string{
			"ProcessPid":     "进程ID",
			"NumThreads":     "线程数量(int32), 失败或未采集时为0占位",
			"CPUPercent":     "进程CPU使用率(单核100%), 成功读取间均值, 窗口可超过30秒, 首次或失败时为0",
			"MemPercent":     "内存使用百分比, 失败时保留0占位",
			"MemRSS":         "物理内存使用量",
			"MemVMS":         "虚拟内存使用量",
			"MemSwap":        "交换内存使用量",
			"NumGoroutine":   "Goroutine数量",
			"NumCgoCall":     "CGO调用次数",
			"GoMaxProcs":     "最大并发核心数",
			"NumConnections": "不再采集, 保留int(0)兼容占位, 不代表实际连接数",
			"NumOpenFiles":   "Linux近似FD数(int), 不支持或失败时为0",
			"FDUsed":         "Linux近似FD数, 包含采样及并发变化的偏差; 未采集时缺省",
			"FDSoftLimit":    "当前进程文件描述符软限制",
			"MemRSSBytes":    "当前进程物理内存字节数",
			"OSCollectedAt":  "进程操作系统字段本机采集时间, 最多30秒按需刷新",
			"ResourceErrors": "进程资源采集失败项, 旧字段零占位不代表实测零值",
		},
		"Memory": map[string]string{
			"TotalAlloc":           "程序启动后累计内存分配量",
			"HeapSys":              "堆内存虚拟占用大小",
			"HeapAlloc":            "当前堆内存分配字节数",
			"HeapInuse":            "正在使用的堆内存字节数",
			"HeapReleased":         "已释放回操作系统的堆内存",
			"HeapIdle":             "堆内存中空闲的字节数",
			"HeapObjects":          "堆中存活的对象数量",
			"StackInuse":           "栈内存使用字节数",
			"MSpanInuse":           "MSpan结构使用的内存",
			"MCacheInuse":          "MCache结构使用的内存",
			"FragmentationPercent": "堆内存碎片化百分比",
		},
		"BytesPool": map[string]string{
			"Capacity":    "字节池刻度: [最小值, 最大值]",
			"OutBytes":    "直接申请未被字节池回收的使用量",
			"OutCount":    "直接申请未被字节池回收的次数",
			"NewBytes":    "新分配的字节量",
			"ReusedBytes": "被复用的字节量",
			"TopPools":    "被复用的字节容量刻度TOP10",
		},
		"Scheduler": map[string]any{
			"GC": map[string]string{
				"NextGC":            "下次GC触发的堆分配字节阈值",
				"LastGC":            "上次GC发生的时间",
				"NumGC":             "GC累计次数",
				"NumForcedGC":       "手动触发的GC次数",
				"PauseTotalSeconds": "GC总暂停时间(秒)",
				"LastPauseMs":       "上次GC的暂停时间(毫秒)",
				"PauseAvgMs":        "GC平均暂停时间(毫秒)",
				"PauseMaxMs":        "最近5次GC的最大暂停时间(毫秒)",
				"PressureRatio":     "GC压力比率, (NextGC-HeapAlloc)/NextGC, 越接近0表示压力越大",
				"GCPerSecond":       "每秒GC次数",
				"PauseRecent":       "最近5次GC的暂停时间列表",
				"GCCPUFraction":     "GC占用CPU时间的比例",
				"LastGCAgo":         "距离上次GC的时间间隔",
			},
			"GoroutinesCreated":   "程序启动至今创建的 goroutine 总数, 持续增长但 live 不降(泄漏)",
			"GoroutinesNotInGo":   "正在 syscall/cgo 中的 goroutine 数(定位 I/O 瓶颈, DNS 慢查询, C 库阻塞)",
			"GoroutinesRunnable":  "就绪但未运行的 goroutine 数(CPU 不足预警! 大于 0 持续几秒 = 饥饿)",
			"GoroutinesRunning":   "当前正在 CPU 上执行的 goroutine 数(应小于等于 GOMAXPROCS, 大于: 说明异常)",
			"GoroutinesWaiting":   "等待资源的 goroutine 数(等待 I/O, channel 积压, 锁竞争等)",
			"ThreadsTotal":        "Go 运行时当前持有的 OS 线程数(结合 GOMAXPROCS 看 syscalls 开销)",
			"GoroutinesTotal":     "总存活 goroutine 数(Main.NumGoroutine)",
			"GOMAXPROCS":          "当前 GOMAXPROCS 值(Main.GoMaxProcs)",
			"SchedulingLatencies": "调度延迟分布, {TotalCount: 调度事件总计数, AverageMs: 平均调度延迟(毫秒), MaxMs: 最大调度延迟(毫秒), BucketCount: 直方图的桶数}",
			"HeapAllocsBytes":     "总分配字节数",
			"HeapFreesBytes":      "总释放字节数",
			"GoPoolFree":          "Go协程池的空闲容量",
			"GoPoolRunning":       "Go协程池正在运行的任务数",
			"GoPoolIdleWorkers":   "Go协程池的空闲工作协程数",
			"GoPoolTotalWorkers":  "Go协程池的总工作协程数",
		},
	}
}

// NetworkStatsDesc 网络资源累计指标注释.
func NetworkStatsDesc() map[string]any {
	return map[string]any{
		"CollectedAt": "网络资源本机采集时间, 最多30秒按需刷新",
		"Errors":      "网络资源采集失败项",
		"Interfaces": map[string]string{
			"BytesRecv":   "接口接收累计字节数",
			"BytesSent":   "接口发送累计字节数",
			"PacketsRecv": "接口接收累计包数",
			"PacketsSent": "接口发送累计包数",
			"ErrorsRecv":  "接口接收累计错误数",
			"ErrorsSent":  "接口发送累计错误数",
			"DropsRecv":   "接口接收累计丢弃数",
			"DropsSent":   "接口发送累计丢弃数",
		},
		"UDP": map[string]string{
			"InDatagrams":  "IPv4 UDP 接收累计数据报数",
			"OutDatagrams": "IPv4 UDP 发送累计数据报数",
			"InErrors":     "IPv4 UDP 接收累计错误数",
			"RcvbufErrors": "IPv4 UDP 接收缓冲区错误数",
			"SndbufErrors": "IPv4 UDP 发送缓冲区错误数",
			"NoPorts":      "IPv4 UDP 目标端口不存在次数",
		},
		"TCP": map[string]string{
			"OutSegs":         "TCP 发送累计段数",
			"RetransSegs":     "TCP 重传累计段数",
			"ListenOverflows": "TCP 监听溢出累计次数",
			"ListenDrops":     "TCP 监听丢弃累计次数",
		},
	}
}

// WebStatsDesc Web服务指标注释
func WebStatsDesc() map[string]string {
	return map[string]string{
		"DisableReduceMemoryUsage": "HTTP服务是否关闭了减少内存占用选项",
		"DisableKeepalive":         "HTTP服务是否关闭了keep-alive",
		"HTTPS":                    "是否启用了HTTPS",
		"BodyLimit":                "请求体大小限制",
	}
}
