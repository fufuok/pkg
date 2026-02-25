package queue

import (
	"sync"
	"testing"
	"time"
)

// TestQueue_Basic 测试队列基本操作
func TestQueue_Basic(t *testing.T) {
	q := New[int]()

	// 测试入队
	q.Enqueue(1)
	q.Enqueue(2)
	q.Enqueue(3)

	// 测试长度
	if l := q.Len(); l != 3 {
		t.Errorf("Expected length 3, got %d", l)
	}

	// 测试出队
	if v := q.Dequeue(); v != 1 {
		t.Errorf("Expected 1, got %d", v)
	}
	if v := q.Dequeue(); v != 2 {
		t.Errorf("Expected 2, got %d", v)
	}
	if v := q.Dequeue(); v != 3 {
		t.Errorf("Expected 3, got %d", v)
	}

	// 测试长度
	if l := q.Len(); l != 0 {
		t.Errorf("Expected length 0, got %d", l)
	}
}

// TestQueue_EmptyDequeue 测试空队列出队（应该阻塞）
func TestQueue_EmptyDequeue(t *testing.T) {
	q := New[int]()

	// 启动一个goroutine入队
	go func() {
		time.Sleep(100 * time.Millisecond)
		q.Enqueue(42)
	}()

	// 测试出队（应该等待并成功获取值）
	start := time.Now()
	v := q.Dequeue()
	duration := time.Since(start)

	if v != 42 {
		t.Errorf("Expected 42, got %d", v)
	}

	// 确保出队操作等待了一段时间（证明阻塞了）
	if duration < 50*time.Millisecond {
		t.Errorf("Dequeue should have blocked, but completed too quickly: %v", duration)
	}
}

// TestQueue_Len 测试队列长度计算
func TestQueue_Len(t *testing.T) {
	q := New[string]()

	// 空队列
	if l := q.Len(); l != 0 {
		t.Errorf("Expected length 0, got %d", l)
	}

	// 入队一个元素
	q.Enqueue("test1")
	if l := q.Len(); l != 1 {
		t.Errorf("Expected length 1, got %d", l)
	}

	// 入队多个元素
	q.Enqueue("test2")
	q.Enqueue("test3")
	if l := q.Len(); l != 3 {
		t.Errorf("Expected length 3, got %d", l)
	}

	// 出队一个元素
	q.Dequeue()
	if l := q.Len(); l != 2 {
		t.Errorf("Expected length 2, got %d", l)
	}

	// 出队所有元素
	q.Dequeue()
	q.Dequeue()
	if l := q.Len(); l != 0 {
		t.Errorf("Expected length 0, got %d", l)
	}
}

// TestQueue_RateState 测试速率统计
func TestQueue_RateState(t *testing.T) {
	q := New[int]()

	// 初始状态
	_, count, lastCount := q.RateState()
	if count != 0 {
		t.Errorf("Expected initial count 0, got %d", count)
	}
	if lastCount != 0 {
		t.Errorf("Expected initial lastCount 0, got %d", lastCount)
	}

	// 入队一些元素
	for i := 0; i < 10; i++ {
		q.Enqueue(i)
	}

	// 测试速率统计
	_, count, lastCount = q.RateState()
	if count != 10 {
		t.Errorf("Expected count 10, got %d", count)
	}

	// 等待一段时间后再次测试速率
	time.Sleep(100 * time.Millisecond)
	_, count, lastCount = q.RateState()
	if count != 10 {
		t.Errorf("Expected count still 10, got %d", count)
	}
}

// TestQueue_Concurrent 测试并发场景
func TestQueue_Concurrent(t *testing.T) {
	q := New[int]()
	var wg sync.WaitGroup

	// 启动多个生产者
	producerCount := 5
	itemsPerProducer := 100
	totalItems := producerCount * itemsPerProducer

	for i := 0; i < producerCount; i++ {
		wg.Add(1)
		go func(producerID int) {
			defer wg.Done()
			for j := 0; j < itemsPerProducer; j++ {
				q.Enqueue(producerID*itemsPerProducer + j)
			}
		}(i)
	}

	// 启动消费者
	var consumerWg sync.WaitGroup
	consumerWg.Add(1)
	received := make(map[int]bool)
	go func() {
		defer consumerWg.Done()
		for i := 0; i < totalItems; i++ {
			v := q.Dequeue()
			received[v] = true
		}
	}()

	// 等待所有生产者完成
	wg.Wait()

	// 等待消费者完成
	consumerWg.Wait()

	// 验证所有项目都被正确接收
	if len(received) != totalItems {
		t.Errorf("Expected to receive %d items, got %d", totalItems, len(received))
	}

	// 验证队列长度为0
	if l := q.Len(); l != 0 {
		t.Errorf("Expected length 0 after all operations, got %d", l)
	}
}

// TestQueue_GenericTypes 测试泛型类型
func TestQueue_GenericTypes(t *testing.T) {
	// 测试字符串类型
	stringQueue := New[string]()
	stringQueue.Enqueue("hello")
	if v := stringQueue.Dequeue(); v != "hello" {
		t.Errorf("Expected 'hello', got '%s'", v)
	}

	// 测试结构体类型
	type TestStruct struct {
		Value int
		Name  string
	}
	structQueue := New[TestStruct]()
	testStruct := TestStruct{Value: 42, Name: "test"}
	structQueue.Enqueue(testStruct)
	if v := structQueue.Dequeue(); v != testStruct {
		t.Errorf("Expected %+v, got %+v", testStruct, v)
	}
}
