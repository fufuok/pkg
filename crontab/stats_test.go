package crontab

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/fufuok/pkg/assert"
)

// TestDataStatsSnapshot 验证统计按登记对象的 EntryID 取时间, 保持公开 JSON 字段和零值规则.
func TestDataStatsSnapshot(t *testing.T) {
	for _, state := range []string{"active", "stopped", "missing_entry", "same_name_old_entry"} {
		t.Run(state, func(t *testing.T) {
			testWithSchedulerBubble(t, func(t *testing.T) {
				job, err := AddJob(t.Context(), "stats_job", "@every 24h", &MockRunner{})
				if err != nil {
					t.Fatal(err)
				}
				var previous, next time.Time
				switch state {
				case "stopped":
					job.Stop()
					// 模拟 Range 已观察到停止对象的窗口, 对外仍应显示零时间.
					jobs.Store(job.Name(), job)
				case "missing_entry":
					crontab.Remove(job.id)
				case "same_name_old_entry":
					old := job
					jobs.Delete(old.Name())
					job, err = AddJob(t.Context(), old.Name(), "@every 23h", &MockRunner{})
					if err != nil {
						t.Fatal(err)
					}
					defer old.Stop()
				}
				if state == "active" || state == "same_name_old_entry" {
					entry := crontab.Entry(job.id)
					previous, next = entry.Prev, entry.Next
				}
				var got map[string]any
				if err := json.Unmarshal(DataStatsJSON(), &got); err != nil {
					t.Fatal(err)
				}
				assert.Equal(t, map[string]any{
					"jobs": float64(1),
					job.Name(): map[string]any{
						"prev_run": previous.Format(time.RFC3339),
						"next_run": next.Format(time.RFC3339),
					},
				}, got)
			})
		})
	}
}

// BenchmarkDataStats 衡量不同任务数下的统计成本, 使用真实调度器但不触发业务回调.
func BenchmarkDataStats(b *testing.B) {
	for _, count := range []int{1, 100, 1000} {
		b.Run(strconv.Itoa(count), func(b *testing.B) {
			previousCron, previousJobs := crontab, jobs
			previousLevel := zerolog.GlobalLevel()
			<-previousCron.Stop().Done()
			zerolog.SetGlobalLevel(zerolog.Disabled)
			initMain()
			b.Cleanup(func() {
				<-crontab.Stop().Done()
				jobs.Range(func(_ string, job *Job) bool {
					job.Stop()
					return true
				})
				crontab, jobs = previousCron, previousJobs
				zerolog.SetGlobalLevel(previousLevel)
				previousCron.Start()
			})
			for i := range count {
				if _, err := AddJob(context.Background(), "stats_"+strconv.Itoa(i), "@every 24h", &MockRunner{}); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportAllocs()
			for b.Loop() {
				DataStats()
			}
		})
	}
}
