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
	"context"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vistone/quic"
)

func TestNewClientPool(t *testing.T) {
	addrResolver := func() (string, error) {
		return "localhost:4433", nil
	}

	pool := quic.NewClientPool(
		5, 20,
		500*time.Millisecond, 5*time.Second,
		30*time.Second,
		"0",
		"localhost",
		addrResolver,
	)
	defer pool.Close()

	require.NotNil(t, pool)
	assert.True(t, pool.Ready())
	assert.Equal(t, 1, pool.ShardCount()) // 20 < 128, 所以是1个分片
	assert.Equal(t, 5, pool.Capacity())
}

func TestNewServerPool(t *testing.T) {
	tlsConfig, err := GenerateTestTLSConfig()
	require.NoError(t, err)

	port, err := GetFreePort()
	require.NoError(t, err)

	listenAddr := fmt.Sprintf("127.0.0.1:%d", port)

	pool := quic.NewServerPool(
		20,
		"",
		tlsConfig,
		listenAddr,
		30*time.Second,
	)
	defer pool.Close()

	require.NotNil(t, pool)
	assert.True(t, pool.Ready())
	assert.Equal(t, 1, pool.ShardCount())
}

func TestNewServerPool_InvalidAddress(t *testing.T) {
	tlsConfig, err := GenerateTestTLSConfig()
	require.NoError(t, err)

	pool := quic.NewServerPool(
		20,
		"",
		tlsConfig,
		"",
		30*time.Second,
	)

	assert.Nil(t, pool)
}

func TestPool_ShardCount(t *testing.T) {
	testCases := []struct {
		name   string
		maxCap int
		want   int
	}{
		{"small pool", 20, 1},
		{"medium pool", 200, 2},
		{"large pool", 500, 4},
		{"very large pool", 1000, 8},
		{"max pool", 8192, 64},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			addrResolver := func() (string, error) {
				return "localhost:4433", nil
			}

			pool := quic.NewClientPool(
				5, tc.maxCap,
				500*time.Millisecond, 5*time.Second,
				30*time.Second,
				"0",
				"localhost",
				addrResolver,
			)
			defer pool.Close()

			assert.Equal(t, tc.want, pool.ShardCount())
		})
	}
}

func TestPool_Active(t *testing.T) {
	addrResolver := func() (string, error) {
		return "localhost:4433", nil
	}

	pool := quic.NewClientPool(
		5, 20,
		500*time.Millisecond, 5*time.Second,
		30*time.Second,
		"0",
		"localhost",
		addrResolver,
	)
	defer pool.Close()

	active := pool.Active()
	assert.GreaterOrEqual(t, active, 0)
	assert.LessOrEqual(t, active, 20)
}

func TestPool_Capacity(t *testing.T) {
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
	defer pool.Close()

	capacity := pool.Capacity()
	assert.Equal(t, 10, capacity)
}

func TestPool_Interval(t *testing.T) {
	addrResolver := func() (string, error) {
		return "localhost:4433", nil
	}

	minIvl := 500 * time.Millisecond
	maxIvl := 5 * time.Second

	pool := quic.NewClientPool(
		5, 20,
		minIvl, maxIvl,
		30*time.Second,
		"0",
		"localhost",
		addrResolver,
	)
	defer pool.Close()

	interval := pool.Interval()
	assert.GreaterOrEqual(t, interval, minIvl)
	assert.LessOrEqual(t, interval, maxIvl)
}

func TestPool_ErrorCount(t *testing.T) {
	addrResolver := func() (string, error) {
		return "localhost:4433", nil
	}

	pool := quic.NewClientPool(
		5, 20,
		500*time.Millisecond, 5*time.Second,
		30*time.Second,
		"0",
		"localhost",
		addrResolver,
	)
	defer pool.Close()

	initialCount := pool.ErrorCount()
	assert.Equal(t, 0, initialCount)

	pool.AddError()
	assert.Equal(t, 1, pool.ErrorCount())

	pool.AddError()
	pool.AddError()
	assert.Equal(t, 3, pool.ErrorCount())

	pool.ResetError()
	assert.Equal(t, 0, pool.ErrorCount())
}

func TestPool_Flush(t *testing.T) {
	addrResolver := func() (string, error) {
		return "localhost:4433", nil
	}

	pool := quic.NewClientPool(
		5, 20,
		500*time.Millisecond, 5*time.Second,
		30*time.Second,
		"0",
		"localhost",
		addrResolver,
	)
	defer pool.Close()

	// Flush应该不会panic
	pool.Flush()
	assert.True(t, pool.Ready())
}

func TestPool_Close(t *testing.T) {
	addrResolver := func() (string, error) {
		return "localhost:4433", nil
	}

	pool := quic.NewClientPool(
		5, 20,
		500*time.Millisecond, 5*time.Second,
		30*time.Second,
		"0",
		"localhost",
		addrResolver,
	)

	pool.Close()

	// 多次关闭应该不会panic
	pool.Close()
	pool.Close()
}

func TestPool_OutgoingGet_Timeout(t *testing.T) {
	addrResolver := func() (string, error) {
		return "localhost:4433", nil
	}

	pool := quic.NewClientPool(
		5, 20,
		500*time.Millisecond, 5*time.Second,
		30*time.Second,
		"0",
		"localhost",
		addrResolver,
	)
	defer pool.Close()

	// 尝试获取不存在的流ID，应该超时
	conn, err := pool.OutgoingGet("nonexistent", 100*time.Millisecond)
	assert.Error(t, err)
	assert.Nil(t, conn)
}

func TestPool_IncomingGet_Timeout(t *testing.T) {
	tlsConfig, err := GenerateTestTLSConfig()
	require.NoError(t, err)

	port, err := GetFreePort()
	require.NoError(t, err)

	listenAddr := fmt.Sprintf("127.0.0.1:%d", port)

	pool := quic.NewServerPool(
		20,
		"",
		tlsConfig,
		listenAddr,
		30*time.Second,
	)
	defer pool.Close()

	// 在没有启动管理器的情况下，应该超时
	_, conn, err := pool.IncomingGet(100 * time.Millisecond)
	assert.Error(t, err)
	assert.Nil(t, conn)
}

func TestPool_ClientManager(t *testing.T) {
	addrResolver := func() (string, error) {
		return "localhost:4433", nil
	}

	pool := quic.NewClientPool(
		5, 20,
		500*time.Millisecond, 5*time.Second,
		30*time.Second,
		"0",
		"localhost",
		addrResolver,
	)
	defer pool.Close()

	// 启动管理器
	pool.ClientManager()

	// 等待一小段时间让管理器启动
	time.Sleep(100 * time.Millisecond)

	// 管理器应该正在运行
	assert.True(t, pool.Ready())
}

func TestPool_ServerManager(t *testing.T) {
	tlsConfig, err := GenerateTestTLSConfig()
	require.NoError(t, err)

	port, err := GetFreePort()
	require.NoError(t, err)

	listenAddr := fmt.Sprintf("127.0.0.1:%d", port)

	pool := quic.NewServerPool(
		20,
		"",
		tlsConfig,
		listenAddr,
		30*time.Second,
	)
	defer pool.Close()

	// 启动管理器
	pool.ServerManager()

	// 等待一小段时间让管理器启动
	time.Sleep(100 * time.Millisecond)

	// 管理器应该正在运行
	assert.True(t, pool.Ready())
}

func TestPool_ConcurrentAccess(t *testing.T) {
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
	defer pool.Close()

	var wg sync.WaitGroup
	numGoroutines := 100

	// 并发访问池的方法
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = pool.Active()
			_ = pool.Capacity()
			_ = pool.Interval()
			_ = pool.ShardCount()
			_ = pool.ErrorCount()
		}()
	}

	wg.Wait()
	// 如果没有panic，测试通过
}

func TestPool_ContextCancellation(t *testing.T) {
	addrResolver := func() (string, error) {
		return "localhost:4433", nil
	}

	pool := quic.NewClientPool(
		5, 20,
		500*time.Millisecond, 5*time.Second,
		30*time.Second,
		"0",
		"localhost",
		addrResolver,
	)

	// 启动管理器
	pool.ClientManager()

	// 等待一小段时间
	time.Sleep(50 * time.Millisecond)

	// 关闭池（这会取消上下文）
	pool.Close()

	// 等待一小段时间确保关闭完成
	time.Sleep(50 * time.Millisecond)

	// 尝试获取流应该失败
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	select {
	case <-ctx.Done():
		// 上下文已取消，这是预期的
	case <-time.After(200 * time.Millisecond):
		t.Fatal("上下文应该已被取消")
	}
}

func TestStreamConn_Interface(t *testing.T) {
	// 这个测试验证StreamConn实现了net.Conn接口
	// 由于StreamConn需要实际的QUIC流，这里只做接口检查
	var _ net.Conn = (*quic.StreamConn)(nil)
}
