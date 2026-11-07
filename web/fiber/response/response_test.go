package response

import (
	"testing"
)

// TestAPISuccessBytesData 验证 JSON 字节封装输出格式与单次内存分配保证.
func TestAPISuccessBytesData(t *testing.T) {
	payload := []byte(`{"ip":"1.1.1.1"}`)
	got := APISuccessBytesData(payload, 128)
	want := `{"ok":1,"code":0,"msg":"","data":{"ip":"1.1.1.1"},"count":128}`
	if string(got) != want {
		t.Fatalf("APISuccessBytesData = %s, want %s", got, want)
	}

	allocs := testing.AllocsPerRun(100, func() {
		_ = APISuccessBytesData(payload, 128)
	})
	if allocs != 1 {
		t.Fatalf("APISuccessBytesData allocs = %v, want 1", allocs)
	}
}

func BenchmarkAPISuccessBytesData(b *testing.B) {
	payload := []byte(`{"ip":"1.1.1.1","country":"中国","province":"四川"}`)
	b.ReportAllocs()
	for b.Loop() {
		_ = APISuccessBytesData(payload, 256)
	}
}
