package middleware

func CacheStats() map[string]any {
	m := make(map[string]any)
	if whitelistLRU != nil {
		m["Whitelist"] = whitelistLRU.Metrics()
	}
	if blacklistLRU != nil {
		m["Blacklist"] = blacklistLRU.Metrics()
	}
	return m
}
