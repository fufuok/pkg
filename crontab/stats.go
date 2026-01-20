package crontab

import (
	"time"

	"github.com/fufuok/utils/xjson/jsongen"

	"github.com/fufuok/pkg/json"
)

func DataStats() *jsongen.Map {
	jss := jsongen.NewMap()
	jss.PutInt("jobs", int64(jobs.Size()))
	jobs.Range(func(name string, j *Job) bool {
		js := jsongen.NewMap()
		js.PutString("prev_run", j.Prev().Format(time.RFC3339))
		js.PutString("next_run", j.Next().Format(time.RFC3339))
		jss.PutMap(name, js)
		return true
	})
	return jss
}

func DataStatsJSON() json.RawMessage {
	return json.RawMessage(DataStats().Serialize(nil))
}
