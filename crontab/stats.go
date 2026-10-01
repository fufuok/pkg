package crontab

import (
	"time"

	"github.com/fufuok/cron"
	"github.com/fufuok/pkg/xjson/jsongen"

	"github.com/fufuok/pkg/json"
)

// DataStats 返回任务数及调度时间, 只取一次调度器快照, 避免逐任务复制整张条目表.
// 任务表与调度器之间不是原子快照; 并发注册、停止时允许短暂缺项或零时间, 不用于调度决策.
func DataStats() *jsongen.Map {
	jss := jsongen.NewMap()
	jss.PutInt("jobs", int64(jobs.Size()))
	snapshot := crontab.Entries()
	entries := make(map[cron.EntryID]cron.Entry, len(snapshot))
	for _, entry := range snapshot {
		entries[entry.ID] = entry
	}
	jobs.Range(func(name string, j *Job) bool {
		// 按对象的 EntryID 匹配, 同名旧条目不得混入新任务; 停止对象仍保持原来的零值规则.
		var previous, next time.Time
		if j.IsRunning() {
			entry := entries[j.id]
			previous, next = entry.Prev, entry.Next
		}
		js := jsongen.NewMap()
		js.PutString("prev_run", previous.Format(time.RFC3339))
		js.PutString("next_run", next.Format(time.RFC3339))
		jss.PutMap(name, js)
		return true
	})
	return jss
}

// DataStatsJSON 将同一次 DataStats 结果序列化为 JSON, 字段和时间格式保持一致.
func DataStatsJSON() json.RawMessage {
	return json.RawMessage(DataStats().Serialize(nil))
}
