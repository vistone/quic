// Copyright (c) 2025, vistone
// All rights reserved.
//
// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions are met:
//
// 1. Redistributions of source code must retain the above copyright notice, this
//    list of conditions and the following disclaimer.
//
// 2. Redistributions in binary form must reproduce the above copyright notice,
//    this list of conditions and the following disclaimer in the documentation
//    and/or other materials provided with the distribution.
//
// 3. Neither the name of the copyright holder nor the names of its
//    contributors may be used to endorse or promote products derived from
//    this software without specific prior written permission.
//
// THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS"
// AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
// IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
// DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS BE LIABLE
// FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
// DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR
// SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER
// CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY,
// OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
// OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.

package test

import (
	"sync"
	"testing"
	"time"

	"github.com/vistone/quic"
)

func BenchmarkPool_Active(b *testing.B) {
	addrResolver := func() (string, error) {
		return "localhost:4433", nil
	}

	pool := quic.NewClientPool(
		10, 100,
		100*time.Millisecond, 1*time.Second,
		30*time.Second,
		"0",
		"localhost",
		addrResolver,
	)
	defer pool.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = pool.Active()
	}
}

func BenchmarkPool_Capacity(b *testing.B) {
	addrResolver := func() (string, error) {
		return "localhost:4433", nil
	}

	pool := quic.NewClientPool(
		10, 100,
		100*time.Millisecond, 1*time.Second,
		30*time.Second,
		"0",
		"localhost",
		addrResolver,
	)
	defer pool.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = pool.Capacity()
	}
}

func BenchmarkPool_ConcurrentAccess(b *testing.B) {
	addrResolver := func() (string, error) {
		return "localhost:4433", nil
	}

	pool := quic.NewClientPool(
		50, 500,
		100*time.Millisecond, 1*time.Second,
		30*time.Second,
		"0",
		"localhost",
		addrResolver,
	)
	defer pool.Close()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = pool.Active()
			_ = pool.Capacity()
			_ = pool.Interval()
			_ = pool.ShardCount()
			_ = pool.ErrorCount()
		}
	})
}

func BenchmarkPool_ErrorOperations(b *testing.B) {
	addrResolver := func() (string, error) {
		return "localhost:4433", nil
	}

	pool := quic.NewClientPool(
		10, 100,
		100*time.Millisecond, 1*time.Second,
		30*time.Second,
		"0",
		"localhost",
		addrResolver,
	)
	defer pool.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pool.AddError()
		if i%10 == 0 {
			pool.ResetError()
		}
	}
}

func BenchmarkPool_ConcurrentErrorOperations(b *testing.B) {
	addrResolver := func() (string, error) {
		return "localhost:4433", nil
	}

	pool := quic.NewClientPool(
		50, 500,
		100*time.Millisecond, 1*time.Second,
		30*time.Second,
		"0",
		"localhost",
		addrResolver,
	)
	defer pool.Close()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			pool.AddError()
			_ = pool.ErrorCount()
		}
	})
}

// TestStress_ConcurrentAccess 压力测试：大量并发访问
func TestStress_ConcurrentAccess(t *testing.T) {
	addrResolver := func() (string, error) {
		return "localhost:4433", nil
	}

	pool := quic.NewClientPool(
		100, 1000,
		100*time.Millisecond, 1*time.Second,
		30*time.Second,
		"0",
		"localhost",
		addrResolver,
	)
	defer pool.Close()

	const numGoroutines = 1000
	const operationsPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	start := time.Now()

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < operationsPerGoroutine; j++ {
				_ = pool.Active()
				_ = pool.Capacity()
				_ = pool.Interval()
				_ = pool.ShardCount()
				_ = pool.ErrorCount()
				pool.AddError()
			}
		}()
	}

	wg.Wait()

	duration := time.Since(start)
	t.Logf("完成 %d 个goroutine，每个 %d 次操作，总耗时: %v", numGoroutines, operationsPerGoroutine, duration)
	t.Logf("平均每次操作耗时: %v", duration/(numGoroutines*operationsPerGoroutine))
}

// TestStress_MultiplePools 压力测试：创建多个池
func TestStress_MultiplePools(t *testing.T) {
	const numPools = 100

	pools := make([]*quic.Pool, numPools)

	for i := 0; i < numPools; i++ {
		addrResolver := func() (string, error) {
			return "localhost:4433", nil
		}

		pool := quic.NewClientPool(
			10, 50,
			500*time.Millisecond, 5*time.Second,
			30*time.Second,
			"0",
			"localhost",
			addrResolver,
		)
		pools[i] = pool
	}

	// 清理
	for _, pool := range pools {
		pool.Close()
	}
}

// TestStress_RapidCreateClose 压力测试：快速创建和关闭池
func TestStress_RapidCreateClose(t *testing.T) {
	const iterations = 100

	for i := 0; i < iterations; i++ {
		addrResolver := func() (string, error) {
			return "localhost:4433", nil
		}

		pool := quic.NewClientPool(
			10, 50,
			500*time.Millisecond, 5*time.Second,
			30*time.Second,
			"0",
			"localhost",
			addrResolver,
		)

		_ = pool.Active()
		_ = pool.Capacity()
		_ = pool.ShardCount()

		pool.Close()
	}
}

// TestStress_ErrorCountUnderLoad 压力测试：高负载下的错误计数
func TestStress_ErrorCountUnderLoad(t *testing.T) {
	addrResolver := func() (string, error) {
		return "localhost:4433", nil
	}

	pool := quic.NewClientPool(
		100, 1000,
		100*time.Millisecond, 1*time.Second,
		30*time.Second,
		"0",
		"localhost",
		addrResolver,
	)
	defer pool.Close()

	const numGoroutines = 500
	const errorsPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < errorsPerGoroutine; j++ {
				pool.AddError()
			}
		}()
	}

	wg.Wait()

	// 验证错误计数
	errorCount := pool.ErrorCount()
	expected := numGoroutines * errorsPerGoroutine
	if errorCount != expected {
		t.Errorf("错误计数不匹配: 期望 %d, 实际 %d", expected, errorCount)
	}

	pool.ResetError()
	if pool.ErrorCount() != 0 {
		t.Errorf("重置后错误计数应该为0，实际为 %d", pool.ErrorCount())
	}
}
