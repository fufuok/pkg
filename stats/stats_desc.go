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
			"InternalIPv4":   "内网IPv4地址",
			"ExternalIPv4":   "外网IPv4地址",
			"NumCpus":        "CPU核心数",
			"LoadAvg":        "系统负载(1分钟, 5分钟, 15分钟)",
			"CPUPercent":     "系统整体CPU使用率(所有核心的平均值)",
			"MemTotal":       "总内存大小",
			"MemAvailable":   "可用内存大小",
			"MemUsed":        "已使用内存大小",
			"MemUsedPercent": "内存使用百分比",
		},
	}
}

// MetricStatsDesc 主程序运行指标注释
func MetricStatsDesc() map[string]any {
	return map[string]any{
		"Main": map[string]string{
			"ProcessPid":     "进程ID",
			"NumThreads":     "线程数量",
			"CPUPercent":     "当前进程CPU使用率(相对于单个CPU核心)",
			"MemPercent":     "内存使用百分比",
			"MemRSS":         "物理内存使用量",
			"MemVMS":         "虚拟内存使用量",
			"MemSwap":        "交换内存使用量",
			"NumGoroutine":   "Goroutine数量",
			"NumCgoCall":     "CGO调用次数",
			"GoMaxProcs":     "最大并发核心数",
			"NumConnections": "网络连接数",
			"NumOpenFiles":   "打开文件描述符数",
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

// WebStatsDesc Web服务指标注释
func WebStatsDesc() map[string]string {
	return map[string]string{
		"DisableReduceMemoryUsage": "HTTP服务是否关闭了减少内存占用选项",
		"DisableKeepalive":         "HTTP服务是否关闭了keep-alive",
		"HTTPS":                    "是否启用了HTTPS",
		"BodyLimit":                "请求体大小限制",
	}
}
