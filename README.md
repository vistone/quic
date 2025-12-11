# QUIC 连接池库

[![Go Reference](https://pkg.go.dev/badge/github.com/vistone/quic.svg)](https://pkg.go.dev/github.com/vistone/quic)
[![License](https://img.shields.io/badge/License-BSD_3--Clause-blue.svg)](https://opensource.org/licenses/BSD-3-Clause)
[![Version](https://img.shields.io/badge/version-1.0.0-blue.svg)](https://github.com/vistone/quic/releases/tag/v1.0.0)

适用于 Go 应用程序的高性能、可靠的 QUIC 流池管理系统。

## 版本信息

**当前版本**: v1.0.0

**Go 版本要求**: Go 1.21+

**最新更新**: 2025-12-11

查看 [CHANGELOG](CHANGELOG.md) 了解版本历史。

## 目录

- [特性](#特性)
- [安装](#安装)
- [核心概念](#核心概念)
- [工作流程](#工作流程)
  - [客户端工作流程](#客户端工作流程)
  - [服务端工作流程](#服务端工作流程)
- [使用场景](#使用场景)
- [快速开始](#快速开始)
- [详细使用方法](#详细使用方法)
  - [客户端池使用](#客户端池使用)
  - [服务端池使用](#服务端池使用)
  - [流ID管理](#流id管理)
- [API 参考](#api-参考)
- [配置说明](#配置说明)
- [安全特性](#安全特性)
- [性能优化](#性能优化)
- [故障排除](#故障排除)
- [许可证](#许可证)

## 特性

- **分片架构**：自动管理多个QUIC连接（1-64个分片），每个连接支持最多128个流
- **无锁设计**：使用原子操作和sync.Map实现高并发性能
- **自动连接管理**：自动建立连接、重连和保活
- **动态容量调整**：根据使用情况自动调整池容量和流创建间隔
- **流ID管理**：4字节随机ID，编码为8字符十六进制字符串
- **双向通信**：支持客户端主动创建流和服务端被动接收流
- **TLS安全**：支持多种TLS安全模式
- **IP白名单**：服务端可限制允许连接的客户端IP

## 安装

### 最新版本 (v1.0.0)

```bash
go get github.com/vistone/quic@v1.0.0
```

### 开发版本

```bash
go get github.com/vistone/quic@latest
```

### 特定版本

```bash
go get github.com/vistone/quic@v1.0.0
```

## 核心概念

### 分片（Shard）
- 每个分片代表一个QUIC连接
- 每个分片最多支持128个并发流
- 池根据`maxCap`自动计算分片数量：`min(max((maxCap + 127) / 128, 1), 64)`

### 流（Stream）
- 每个流是一个独立的QUIC流，可以双向通信
- 每个流有一个唯一的8字符十六进制ID（由4字节随机数编码）
- 流存储在分片的`sync.Map`中，通过ID快速查找

### 流ID
- 服务端生成：4字节随机数，编码为8字符十六进制字符串（如："a1b2c3d4"）
- 客户端接收：通过`createStream`从服务端接收流ID
- 客户端使用：通过`OutgoingGet(id, timeout)`根据ID获取流

## 工作流程

### 客户端工作流程

```
1. 创建客户端池
   └─> NewClientPool() 
       ├─> 计算分片数量
       ├─> 创建分片数组
       └─> 初始化容量和间隔

2. 启动管理器
   └─> ClientManager()
       └─> 启动 manageAllShardsClient() (后台goroutine)

3. 管理器循环（manageAllShardsClient）
   ├─> 调整流创建间隔（根据使用率）
   ├─> 计算需要创建的流数量
   ├─> 遍历所有分片：
   │   ├─> 建立QUIC连接（如果需要）
   │   ├─> 计算分片可用容量
   │   └─> 创建流（并发）
   │       └─> createStream()
   │           ├─> 打开QUIC流
   │           ├─> 发送握手字节 0x00
   │           ├─> 接收4字节流ID
   │           ├─> 存储流到分片
   │           └─> 放入全局通道
   ├─> 调整池容量（根据成功率）
   └─> 等待间隔时间后继续

4. 获取流
   └─> OutgoingGet(streamID, timeout)
       ├─> 在所有分片中查找流ID
       ├─> 从全局通道取出ID
       └─> 返回StreamConn（实现net.Conn接口）
```

### 服务端工作流程

```
1. 创建服务端池
   └─> NewServerPool()
       ├─> 计算分片数量
       ├─> 创建分片数组
       └─> 初始化监听地址

2. 启动管理器
   └─> ServerManager()
       └─> 为每个分片启动 manageShardServer() (后台goroutine)

3. 每个分片管理器循环（manageShardServer）
   ├─> 启动QUIC监听器
   ├─> 接受QUIC连接
   │   ├─> 验证客户端IP（如果设置了）
   │   └─> 为每个连接启动goroutine接受流
   │       └─> 循环接受流
   │           └─> handleStream()
   │               ├─> 检查池容量
   │               ├─> 读取握手字节
   │               ├─> 生成4字节流ID
   │               ├─> 发送流ID给客户端
   │               ├─> 存储流到分片
   │               └─> 放入全局通道

4. 获取流
   └─> IncomingGet(timeout)
       ├─> 从全局通道获取流ID
       ├─> 在所有分片中查找流
       └─> 返回(streamID, StreamConn, error)
```

## 使用场景

### 1. 客户端主动连接场景
**适用场景**：客户端需要主动向服务端发起连接
- **工作方式**：客户端池自动创建流，客户端通过已知的流ID获取流
- **典型应用**：
  - API客户端
  - 数据采集客户端
  - 实时通信客户端

### 2. 服务端被动接收场景
**适用场景**：服务端等待客户端连接
- **工作方式**：服务端池监听连接，自动处理客户端发起的流，服务端通过`IncomingGet`获取可用流
- **典型应用**：
  - Web服务器
  - API服务器
  - 代理服务器

### 3. 双向通信场景
**适用场景**：需要客户端和服务端都能主动发起通信
- **工作方式**：同时使用客户端池和服务端池
- **典型应用**：
  - P2P应用
  - 实时协作应用
  - 双向数据同步

## 快速开始

### 客户端示例

```go
package main

import (
	"log"
	"time"
	"github.com/vistone/quic"
)

func main() {
	// 1. 创建地址解析器
	addrResolver := func() (string, error) {
		return "localhost:4433", nil
	}

	// 2. 创建客户端池
	clientPool := quic.NewClientPool(
		5, 20,                              // 最小/最大容量
		500*time.Millisecond, 5*time.Second, // 最小/最大间隔
		30*time.Second,                     // 保活周期
		"0",                                // TLS模式（0=InsecureSkipVerify）
		"localhost",                        // 主机名
		addrResolver,                       // 地址解析函数
	)
	defer clientPool.Close()

	// 3. 启动客户端管理器（必须在goroutine中运行）
	go clientPool.ClientManager()

	// 4. 等待池初始化（让管理器有时间创建流）
	time.Sleep(2 * time.Second)

	// 5. 获取流（需要知道流ID，通常从服务端获取）
	// 注意：实际使用中，流ID应该通过其他方式（如HTTP API）从服务端获取
	streamID := "a1b2c3d4" // 示例ID
	stream, err := clientPool.OutgoingGet(streamID, 10*time.Second)
	if err != nil {
		log.Fatalf("获取流失败: %v", err)
	}
	defer stream.Close()

	// 6. 使用流进行通信
	data := []byte("Hello from client")
	if _, err := stream.Write(data); err != nil {
		log.Fatalf("写入失败: %v", err)
	}
	log.Printf("成功发送 %d 字节", len(data))
}
```

### 服务端示例

```go
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"log"
	"math/big"
	"net"
	"time"

	"github.com/vistone/quic"
)

// 生成测试用TLS配置
func generateTLSConfig() (*tls.Config, error) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{Organization: []string{"Test"}},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
		DNSNames:     []string{"localhost"},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return nil, err
	}

	return &tls.Config{
		Certificates: []tls.Certificate{{
			Certificate: [][]byte{certDER},
			PrivateKey:  privateKey,
		}},
		MinVersion: tls.VersionTLS13,
		NextProtos: []string{"np-quic"},
	}, nil
}

func main() {
	// 1. 生成TLS配置
	tlsConfig, err := generateTLSConfig()
	if err != nil {
		log.Fatalf("生成TLS配置失败: %v", err)
	}

	// 2. 创建服务端池
	serverPool := quic.NewServerPool(
		20,                    // 最大容量
		"",                    // 客户端IP限制（""表示允许任何IP）
		tlsConfig,             // TLS配置
		"0.0.0.0:4433",       // 监听地址
		30*time.Second,        // 保活周期
	)
	defer serverPool.Close()

	// 3. 启动服务端管理器（必须在goroutine中运行）
	go serverPool.ServerManager()

	log.Printf("服务器启动，包含 %d 个分片", serverPool.ShardCount())

	// 4. 循环获取可用流
	for {
		// 从池中获取流（会阻塞直到有可用流）
		id, stream, err := serverPool.IncomingGet(30 * time.Second)
		if err != nil {
			log.Printf("获取流失败: %v", err)
			continue
		}

		// 5. 在goroutine中处理流
		go handleStream(id, stream)
	}
}

func handleStream(id string, stream net.Conn) {
	defer stream.Close()
	log.Printf("收到新流: %s", id)

	// 读取客户端数据
	buf := make([]byte, 1024)
	n, err := stream.Read(buf)
	if err != nil {
		log.Printf("读取错误: %v", err)
		return
	}

	log.Printf("收到数据: %s", string(buf[:n]))

	// 发送响应
	response := []byte("Hello from server")
	if _, err := stream.Write(response); err != nil {
		log.Printf("写入错误: %v", err)
		return
	}

	log.Printf("已发送响应到流: %s", id)
}
```

## 详细使用方法

### 客户端池使用

#### 1. 创建客户端池

```go
pool := quic.NewClientPool(
    minCap, maxCap,        // 容量范围
    minIvl, maxIvl,        // 流创建间隔范围
    keepAlive,             // 保活周期
    tlsCode,               // TLS模式："0"=InsecureSkipVerify, "1"=自签名, "2"=已验证
    hostname,              // 主机名（用于TLS验证）
    addrResolver,          // 地址解析函数
)
```

**参数说明**：
- `minCap, maxCap`：池的最小和最大容量（流数量）
- `minIvl, maxIvl`：流创建尝试之间的最小和最大间隔
- `keepAlive`：QUIC连接保活周期
- `tlsCode`：TLS安全模式
  - `"0"`：InsecureSkipVerify（仅测试）
  - `"1"`：自签名证书（开发环境）
  - `"2"`：已验证证书（生产环境）
- `hostname`：用于TLS证书验证的主机名
- `addrResolver`：返回目标服务器地址的函数

#### 2. 启动管理器

```go
go pool.ClientManager()
```

**重要**：必须在goroutine中调用，因为管理器会在后台持续运行。

#### 3. 等待初始化

```go
time.Sleep(2 * time.Second) // 等待管理器创建初始流
```

#### 4. 获取流

```go
stream, err := pool.OutgoingGet(streamID, timeout)
if err != nil {
    // 处理错误
}
defer stream.Close()
```

**注意**：客户端需要知道流ID才能获取流。流ID通常通过以下方式获取：
- 通过HTTP API从服务端获取
- 通过其他通信渠道获取
- 在双向通信场景中，客户端也可以创建服务端池来接收流

### 服务端池使用

#### 1. 准备TLS配置

```go
// 方式1：加载证书文件
cert, err := tls.LoadX509KeyPair("cert.pem", "key.pem")
tlsConfig := &tls.Config{
    Certificates: []tls.Certificate{cert},
}

// 方式2：生成自签名证书（测试用）
tlsConfig, err := generateTestTLSConfig()
```

#### 2. 创建服务端池

```go
pool := quic.NewServerPool(
    maxCap,        // 最大容量
    clientIP,      // 客户端IP限制（""表示允许任何IP）
    tlsConfig,     // TLS配置（必需）
    listenAddr,    // 监听地址（如"0.0.0.0:4433"）
    keepAlive,     // 保活周期
)
```

#### 3. 启动管理器

```go
go pool.ServerManager()
```

**重要**：必须在goroutine中调用。

#### 4. 获取流

```go
for {
    id, stream, err := pool.IncomingGet(30 * time.Second)
    if err != nil {
        // 处理错误（可能是超时）
        continue
    }
    
    // 在goroutine中处理流
    go handleStream(id, stream)
}
```

### 流ID管理

#### 流ID格式
- **长度**：8个字符
- **格式**：十六进制字符串（0-9, a-f）
- **示例**：`"a1b2c3d4"`, `"00000000"`（第一个流）

#### 客户端获取流ID的方式

**方式1：通过HTTP API**
```go
// 客户端请求服务端获取流ID
resp, err := http.Get("https://server.com/api/stream-id")
var streamID string
json.NewDecoder(resp.Body).Decode(&streamID)

// 使用流ID获取流
stream, err := clientPool.OutgoingGet(streamID, 10*time.Second)
```

**方式2：双向通信**
```go
// 客户端也创建服务端池接收流
serverPool := quic.NewServerPool(...)
go serverPool.ServerManager()

// 从服务端池获取流（包含流ID）
id, stream, err := serverPool.IncomingGet(timeout)
// 现在可以使用这个ID在客户端池中获取对应的流
```

**方式3：预共享流ID**
```go
// 如果客户端和服务端有预共享的流ID列表
streamIDs := []string{"id1", "id2", "id3"}
stream, err := clientPool.OutgoingGet(streamIDs[0], timeout)
```

## API 参考

### 创建池

#### NewClientPool
```go
func NewClientPool(
    minCap, maxCap int,
    minIvl, maxIvl time.Duration,
    keepAlive time.Duration,
    tlsCode string,
    hostname string,
    addrResolver func() (string, error),
) *Pool
```

创建客户端QUIC池。

#### NewServerPool
```go
func NewServerPool(
    maxCap int,
    clientIP string,
    tlsConfig *tls.Config,
    listenAddr string,
    keepAlive time.Duration,
) *Pool
```

创建服务端QUIC池。如果`listenAddr`为空，返回`nil`。

### 管理器

#### ClientManager
```go
func (p *Pool) ClientManager()
```

启动客户端管理器。必须在goroutine中调用。

#### ServerManager
```go
func (p *Pool) ServerManager()
```

启动服务端管理器。必须在goroutine中调用。

### 获取流

#### OutgoingGet
```go
func (p *Pool) OutgoingGet(id string, timeout time.Duration) (net.Conn, error)
```

根据流ID获取流。用于客户端池。
- `id`：8字符十六进制流ID
- `timeout`：超时时间
- 返回：`net.Conn`接口（实际类型为`*StreamConn`）

#### IncomingGet
```go
func (p *Pool) IncomingGet(timeout time.Duration) (string, net.Conn, error)
```

获取可用流并返回流ID。用于服务端池。
- `timeout`：超时时间
- 返回：`(streamID, stream, error)`

### 池状态

#### Active
```go
func (p *Pool) Active() int
```

返回当前可用流数量（池中空闲的流）。

#### Capacity
```go
func (p *Pool) Capacity() int
```

返回当前目标容量（动态调整后的容量）。

#### Interval
```go
func (p *Pool) Interval() time.Duration
```

返回当前流创建间隔（动态调整后的间隔）。

#### ShardCount
```go
func (p *Pool) ShardCount() int
```

返回分片数量（QUIC连接数）。

#### Ready
```go
func (p *Pool) Ready() bool
```

检查池是否已初始化。

### 错误管理

#### AddError
```go
func (p *Pool) AddError()
```

增加错误计数（原子操作）。

#### ErrorCount
```go
func (p *Pool) ErrorCount() int
```

获取当前错误计数。

#### ResetError
```go
func (p *Pool) ResetError()
```

重置错误计数。

### 资源管理

#### Flush
```go
func (p *Pool) Flush()
```

清空池中的所有流（关闭所有流并重置）。

#### Close
```go
func (p *Pool) Close()
```

关闭连接池，释放所有资源（取消上下文、关闭连接、清空流）。

### StreamConn 接口

`StreamConn`实现了`net.Conn`接口，提供以下方法：

```go
type StreamConn struct {
    *quic.Stream
    conn       *quic.Conn
    localAddr  net.Addr
    remoteAddr net.Addr
}

// 实现 net.Conn 接口
func (s *StreamConn) LocalAddr() net.Addr
func (s *StreamConn) RemoteAddr() net.Addr
func (s *StreamConn) SetDeadline(t time.Time) error
func (s *StreamConn) ConnectionState() tls.ConnectionState
```

## 配置说明

### 容量配置

| 容量范围 | 分片数 | 适用场景 |
|---------|--------|----------|
| 1-128 | 1 | 小型应用、测试 |
| 129-256 | 2 | 中等流量服务 |
| 257-512 | 4 | 高流量API |
| 513-1024 | 8 | 极高并发 |
| 1025-4096 | 16-32 | 企业级规模 |
| 4097-8192 | 33-64 | 大规模部署 |

**分片计算公式**：`min(max((maxCap + 127) / 128, 1), 64)`

### 间隔配置

| 场景 | minIvl | maxIvl | 说明 |
|------|--------|--------|------|
| 高频应用 | 100ms | 1s | 快速响应需求 |
| 通用应用 | 500ms | 5s | 平衡性能和资源 |
| 批处理 | 2s | 10s | 低频率、大批量 |

### TLS模式

| 模式 | 代码 | 安全级别 | 使用场景 |
|------|------|----------|----------|
| InsecureSkipVerify | "0" | 低 | 内部网络、测试 |
| 自签名证书 | "1" | 中 | 开发、测试环境 |
| 已验证证书 | "2" | 高 | 生产、公共网络 |

## 安全特性

### 客户端IP限制

服务端可以限制允许连接的客户端IP：

```go
// 只允许特定IP连接
serverPool := quic.NewServerPool(
    20,
    "192.168.1.100",  // 只允许这个IP
    tlsConfig,
    "0.0.0.0:4433",
    30*time.Second,
)

// 允许任何IP连接
serverPool := quic.NewServerPool(
    20,
    "",  // 空字符串表示允许任何IP
    tlsConfig,
    "0.0.0.0:4433",
    30*time.Second,
)
```

### TLS安全模式

生产环境应使用已验证证书：

```go
// 生产环境配置
tlsConfig := &tls.Config{
    Certificates: []tls.Certificate{cert},
    MinVersion:   tls.VersionTLS13,
}

clientPool := quic.NewClientPool(
    minCap, maxCap,
    minIvl, maxIvl,
    keepAlive,
    "2",  // 已验证证书模式
    "example.com",
    addrResolver,
)
```

## 性能优化

### 1. 合理设置容量

```go
// 根据实际并发需求设置
expectedConcurrent := 100
peakConcurrent := 200

pool := quic.NewClientPool(
    expectedConcurrent,      // minCap
    peakConcurrent * 2,       // maxCap (留有余量)
    500*time.Millisecond,
    5*time.Second,
    30*time.Second,
    "2",
    "example.com",
    addrResolver,
)
```

### 2. 监控池状态

```go
go func() {
    ticker := time.NewTicker(10 * time.Second)
    defer ticker.Stop()
    for range ticker.C {
        active := pool.Active()
        capacity := pool.Capacity()
        utilization := float64(active) / float64(capacity)
        log.Printf("池状态: %d/%d (%.1f%%)", active, capacity, utilization*100)
    }
}()
```

### 3. 错误处理

```go
// 监控错误率
go func() {
    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()
    for range ticker.C {
        errors := pool.ErrorCount()
        if errors > 100 {
            log.Printf("警告: 错误计数较高: %d", errors)
            pool.ResetError()
        }
    }
}()
```

## 故障排除

### 常见问题

#### 1. 连接超时
**症状**：无法建立QUIC连接
**解决方案**：
- 检查网络连接
- 验证服务器地址和端口
- 确保防火墙允许UDP流量
- 检查NAT/防火墙UDP超时设置

#### 2. TLS握手失败
**症状**：连接因证书错误失败
**解决方案**：
- 验证证书有效性
- 检查主机名是否匹配
- 确保支持TLS 1.3
- 测试环境可使用模式"1"（自签名）

#### 3. 流获取超时
**症状**：`IncomingGet`或`OutgoingGet`超时
**解决方案**：
- 检查池容量是否足够
- 验证连接是否正常
- 检查流是否正确关闭（避免泄漏）
- 增加超时时间

#### 4. 高错误率
**症状**：频繁的流创建失败
**解决方案**：
- 检查QUIC连接稳定性
- 监控网络丢包
- 验证服务器是否正常
- 使用`ErrorCount()`跟踪错误

### 调试技巧

```go
// 启用详细日志
log.SetFlags(log.LstdFlags | log.Lmicroseconds)

// 监控池状态
go func() {
    for {
        time.Sleep(5 * time.Second)
        log.Printf("池状态: Active=%d, Capacity=%d, Shards=%d, Errors=%d",
            pool.Active(), pool.Capacity(), pool.ShardCount(), pool.ErrorCount())
    }
}()
```

## 许可证

版权所有 (c) 2025, vistone。根据 BSD 3-Clause 许可证授权。
详见 [LICENSE](LICENSE) 文件了解详情。
