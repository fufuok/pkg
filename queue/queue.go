package queue

import (
	"github.com/fufuok/cache/xsync"

	"github.com/fufuok/pkg/kit"
)

// Queue 封装了 UMPSCQueue 和计数器
type Queue[T any] struct {
	queue        *xsync.UMPSCQueue[T]
	queueRate    *kit.RateState
	enqueueCount *kit.UCounter
	dequeueCount *kit.UCounter
}

// New 创建一个新的 Queue 实例
func New[T any]() *Queue[T] {
	return &Queue[T]{
		queue:        xsync.NewUMPSCQueue[T](),
		queueRate:    kit.NewRateState(),
		enqueueCount: kit.NewUCounter(),
		dequeueCount: kit.NewUCounter(),
	}
}

// Enqueue 向队列中添加数据并更新计数器 (多生产者: 安全)
func (q *Queue[T]) Enqueue(value T) {
	q.enqueueCount.Add(1)
	q.queue.Enqueue(value)
}

// EnqueueCount 返回此队列实例创建以来的累计 Enqueue 调用数, 出队不会扣减.
// 只读取既有分片计数器, 不清零计数, 不推进 RateState 的速率采样窗口.
// 并发读写安全, 但不提供各分片同一时刻的原子快照; 计数在实际发布元素前递增,
// 因此不能用它判断当前可出队数量或消费完成量. 读取成本与分片数成正比.
func (q *Queue[T]) EnqueueCount() uint64 {
	return q.enqueueCount.Load()
}

// Dequeue 从队列中获取数据并更新计数器 (仅单消费者!!!)
func (q *Queue[T]) Dequeue() T {
	value := q.queue.Dequeue()
	q.dequeueCount.Add(1)
	return value
}

// Len 返回当前队列长度
func (q *Queue[T]) Len() uint64 {
	enq := q.enqueueCount.Load()
	deq := q.dequeueCount.Load()
	if enq < deq {
		return 0
	}
	return enq - deq
}

// RateState 返回入队速率, 当前计数, 上一次计数
func (q *Queue[T]) RateState() (rate float64, enqueueCount, lastEnqueueCount uint64) {
	enqueueCount = q.enqueueCount.Load()
	rate, lastEnqueueCount = q.queueRate.RateWithLastCount(q.enqueueCount.Load())
	return
}
