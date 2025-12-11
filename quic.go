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

// Package quic 实现了基于QUIC协议的高性能、可靠的网络连接池管理系统
package quic

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/quic-go/quic-go"
)

const (
	// defaultMinCap 默认最小容量
	defaultMinCap = 1
	// defaultMaxCap 默认最大容量
	defaultMaxCap = 1
	// defaultMinIvl 默认最小间隔
	defaultMinIvl = 1 * time.Second
	// defaultMaxIvl 默认最大间隔
	defaultMaxIvl = 1 * time.Second
	// idReadTimeout ID读取超时时间
	idReadTimeout = 1 * time.Minute
	// idRetryInterval ID重试间隔
	idRetryInterval = 50 * time.Millisecond
	// acceptRetryInterval 接受连接重试间隔
	acceptRetryInterval = 50 * time.Millisecond
	// reconnectRetryInterval 重连重试间隔
	reconnectRetryInterval = 500 * time.Millisecond
	// intervalAdjustStep 间隔调整步长
	intervalAdjustStep = 100 * time.Millisecond
	// capacityAdjustLowRatio 容量调整低比率阈值
	capacityAdjustLowRatio = 0.2
	// capacityAdjustHighRatio 容量调整高比率阈值
	capacityAdjustHighRatio = 0.8
	// intervalLowThreshold 间隔调整低阈值
	intervalLowThreshold = 0.2
	// intervalHighThreshold 间隔调整高阈值
	intervalHighThreshold = 0.8
	// defaultALPN 默认应用层协议协商
	defaultALPN = "np-quic"
	// defaultStreamsPerConn 每个连接的默认流数
	defaultStreamsPerConn = 128
	// minConnsPerPool 每个池的最小连接数
	minConnsPerPool = 1
	// maxConnsPerPool 每个池的最大连接数
	maxConnsPerPool = 64
)

// Shard 连接分片，封装单个QUIC连接及其流管理
type Shard struct {
	// streams 存储流的映射表
	streams sync.Map
	// idChan 可用流ID通道
	idChan chan string
	// first 首次标志
	first atomic.Bool
	// quicConn QUIC连接
	quicConn atomic.Pointer[quic.Conn]
	// quicListener QUIC监听器
	quicListener atomic.Pointer[quic.Listener]
	// index 分片索引
	index int
	// maxStreams 此分片的最大流数
	maxStreams int
}

// Pool QUIC连接池结构体，用于管理QUIC流
type Pool struct {
	// shards 连接分片切片
	shards []*Shard
	// numShards 分片数量
	numShards int
	// idChan 全局可用流ID通道
	idChan chan string
	// tlsCode TLS安全模式代码
	tlsCode string
	// hostname 主机名
	hostname string
	// clientIP 客户端IP
	clientIP string
	// tlsConfig TLS配置
	tlsConfig *tls.Config
	// addrResolver 地址解析器
	addrResolver func() (string, error)
	// listenAddr 监听地址
	listenAddr string
	// errCount 错误计数
	errCount atomic.Int32
	// capacity 当前容量
	capacity atomic.Int32
	// minCap 最小容量
	minCap int
	// maxCap 最大容量
	maxCap int
	// interval 流创建间隔
	interval atomic.Int64
	// minIvl 最小间隔
	minIvl time.Duration
	// maxIvl 最大间隔
	maxIvl time.Duration
	// keepAlive 保活间隔
	keepAlive time.Duration
	// ctx 上下文
	ctx context.Context
	// cancel 取消函数
	cancel context.CancelFunc
}

// StreamConn 将QUIC流包装为接口
type StreamConn struct {
	*quic.Stream
	// conn QUIC连接
	conn *quic.Conn
	// localAddr 本地地址
	localAddr net.Addr
	// remoteAddr 远程地址
	remoteAddr net.Addr
}

// LocalAddr 返回本地地址
func (s *StreamConn) LocalAddr() net.Addr {
	return s.localAddr
}

// RemoteAddr 返回远程地址
func (s *StreamConn) RemoteAddr() net.Addr {
	return s.remoteAddr
}

// SetDeadline 设置读写截止时间
func (s *StreamConn) SetDeadline(t time.Time) error {
	if err := s.Stream.SetReadDeadline(t); err != nil {
		return err
	}
	return s.Stream.SetWriteDeadline(t)
}

// ConnectionState 返回TLS状态
func (s *StreamConn) ConnectionState() tls.ConnectionState {
	return s.conn.ConnectionState().TLS
}

// validateCapacity 验证并规范化容量参数
func validateCapacity(minCap, maxCap int) (int, int) {
	if minCap <= 0 {
		minCap = defaultMinCap
	}
	if maxCap <= 0 {
		maxCap = defaultMaxCap
	}
	if minCap > maxCap {
		minCap, maxCap = maxCap, minCap
	}
	return minCap, maxCap
}

// validateInterval 验证并规范化间隔参数
func validateInterval(minIvl, maxIvl time.Duration) (time.Duration, time.Duration) {
	if minIvl <= 0 {
		minIvl = defaultMinIvl
	}
	if maxIvl <= 0 {
		maxIvl = defaultMaxIvl
	}
	if minIvl > maxIvl {
		minIvl, maxIvl = maxIvl, minIvl
	}
	return minIvl, maxIvl
}

// min 返回两个整数中的较小值
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// max 返回两个整数中的较大值
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// minDuration 返回两个时间间隔中的较小值
func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

// maxDuration 返回两个时间间隔中的较大值
func maxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}

// calculateShardCount 计算所需的分片数量
func calculateShardCount(maxCap int) int {
	numShards := min(max((maxCap+defaultStreamsPerConn-1)/defaultStreamsPerConn, minConnsPerPool), maxConnsPerPool)
	return numShards
}

// createShards 创建并初始化分片切片
func createShards(numShards int) []*Shard {
	shards := make([]*Shard, numShards)
	for i := range shards {
		shards[i] = &Shard{
			streams:    sync.Map{},
			idChan:     make(chan string, defaultStreamsPerConn),
			index:      i,
			maxStreams: defaultStreamsPerConn,
		}
	}
	return shards
}

// buildTLSConfig 构建TLS配置
func buildTLSConfig(tlsCode, hostname string) *tls.Config {
	var tlsConfig *tls.Config
	switch tlsCode {
	case "0", "1":
		// 使用自签名证书（不验证）
		tlsConfig = &tls.Config{
			InsecureSkipVerify: true,
			NextProtos:         []string{defaultALPN},
			MinVersion:         tls.VersionTLS13,
		}
	case "2":
		// 使用验证证书（安全模式）
		tlsConfig = &tls.Config{
			InsecureSkipVerify: false,
			ServerName:         hostname,
			NextProtos:         []string{defaultALPN},
			MinVersion:         tls.VersionTLS13,
		}
	}
	return tlsConfig
}

// buildQUICConfig 构建QUIC配置
func buildQUICConfig(keepAlive time.Duration) *quic.Config {
	return &quic.Config{
		KeepAlivePeriod:    keepAlive,
		MaxIdleTimeout:     keepAlive * 3,
		MaxIncomingStreams: int64(defaultStreamsPerConn),
	}
}

// createStreamConn 从分片中创建StreamConn
func createStreamConn(shard *Shard, stream any) (*StreamConn, bool) {
	conn := shard.quicConn.Load()
	if conn == nil {
		return nil, false
	}
	return &StreamConn{
		Stream:     stream.(*quic.Stream),
		conn:       conn,
		localAddr:  conn.LocalAddr(),
		remoteAddr: conn.RemoteAddr(),
	}, true
}

// findStreamInShards 在所有分片中查找并删除流
func (p *Pool) findStreamInShards(id string, consumeShardChan bool) (*StreamConn, bool) {
	for _, shard := range p.shards {
		if stream, ok := shard.streams.LoadAndDelete(id); ok {
			if consumeShardChan {
				<-shard.idChan
			}
			if streamConn, ok := createStreamConn(shard, stream); ok {
				return streamConn, true
			}
		}
	}
	return nil, false
}

// flushShard 清空分片中的所有流
func (s *Shard) flushShard() {
	s.streams.Range(func(key, value any) bool {
		if stream, ok := value.(*quic.Stream); ok {
			stream.Close()
		}
		return true
	})
	s.streams = sync.Map{}
	s.idChan = make(chan string, s.maxStreams)
}

// closeShard 关闭分片的连接和监听器
func (s *Shard) closeShard() {
	if conn := s.quicConn.Swap(nil); conn != nil {
		conn.CloseWithError(0, "pool closed")
	}
	if listener := s.quicListener.Swap(nil); listener != nil {
		listener.Close()
	}
}

// NewClientPool 创建新的客户端QUIC池
// minCap 最小容量
// maxCap 最大容量
// minIvl 最小间隔
// maxIvl 最大间隔
// keepAlive 保活间隔
// tlsCode TLS安全模式代码
// hostname 主机名
// addrResolver 地址解析器
func NewClientPool(
	minCap, maxCap int,
	minIvl, maxIvl time.Duration,
	keepAlive time.Duration,
	tlsCode string,
	hostname string,
	addrResolver func() (string, error),
) *Pool {
	minCap, maxCap = validateCapacity(minCap, maxCap)
	minIvl, maxIvl = validateInterval(minIvl, maxIvl)
	numShards := calculateShardCount(maxCap)
	shards := createShards(numShards)

	pool := &Pool{
		shards:       shards,
		numShards:    numShards,
		idChan:       make(chan string, maxCap),
		tlsCode:      tlsCode,
		hostname:     hostname,
		addrResolver: addrResolver,
		minCap:       minCap,
		maxCap:       maxCap,
		minIvl:       minIvl,
		maxIvl:       maxIvl,
		keepAlive:    keepAlive,
	}
	pool.capacity.Store(int32(minCap))
	pool.interval.Store(int64(minIvl))
	pool.ctx, pool.cancel = context.WithCancel(context.Background())
	return pool
}

// NewServerPool 创建新的服务端QUIC池
// maxCap 最大容量
// clientIP 客户端IP
// tlsConfig TLS配置
// listenAddr 监听地址
// keepAlive 保活间隔
func NewServerPool(
	maxCap int,
	clientIP string,
	tlsConfig *tls.Config,
	listenAddr string,
	keepAlive time.Duration,
) *Pool {
	if maxCap <= 0 {
		maxCap = defaultMaxCap
	}
	if listenAddr == "" {
		return nil
	}

	numShards := calculateShardCount(maxCap)
	shards := createShards(numShards)

	pool := &Pool{
		shards:     shards,
		numShards:  numShards,
		idChan:     make(chan string, maxCap),
		clientIP:   clientIP,
		tlsConfig:  tlsConfig,
		listenAddr: listenAddr,
		maxCap:     maxCap,
		keepAlive:  keepAlive,
	}
	pool.ctx, pool.cancel = context.WithCancel(context.Background())
	return pool
}

// createStream 在指定分片上创建新的客户端流
// ctx 上下文
// globalChan 全局通道
func (s *Shard) createStream(ctx context.Context, globalChan chan string) bool {
	conn := s.quicConn.Load()
	if conn == nil {
		return false
	}

	// 打开新的流
	stream, err := conn.OpenStreamSync(ctx)
	if err != nil {
		s.quicConn.Store(nil)
		return false
	}

	// 发送握手字节
	if _, err := stream.Write([]byte{0x00}); err != nil {
		stream.Close()
		s.quicConn.Store(nil)
		return false
	}

	var id string

	// 接收流ID
	stream.SetReadDeadline(time.Now().Add(idReadTimeout))
	buf := make([]byte, 4)
	n, err := io.ReadFull(stream, buf)
	if err != nil || n != 4 {
		stream.Close()
		s.quicConn.Store(nil)
		return false
	}
	id = hex.EncodeToString(buf)
	stream.SetReadDeadline(time.Time{})

	// 建立映射并存入分片通道和全局通道
	s.streams.Store(id, stream)
	select {
	case s.idChan <- id:
		// 同时尝试放入全局通道
		select {
		case globalChan <- id:
			return true
		default:
			// 全局通道满
			<-s.idChan
			s.streams.Delete(id)
			stream.Close()
			return false
		}
	default:
		s.streams.Delete(id)
		stream.Close()
		return false
	}
}

// handleStream 处理新的服务端流
// stream 流
// globalChan 全局通道
// maxCap 最大容量
// globalActive 全局活跃函数
func (s *Shard) handleStream(stream *quic.Stream, globalChan chan string, maxCap int, globalActive func() int) {
	var streamClosed bool
	defer func() {
		if !streamClosed {
			(*stream).Close()
		}
	}()

	// 检查全局池是否已满
	if globalActive() >= maxCap {
		return
	}

	// 读取握手字节
	handshake := make([]byte, 1)
	if _, err := (*stream).Read(handshake); err != nil {
		return
	}

	// 生成流ID
	rawID, id, err := s.generateID()
	if err != nil {
		return
	}

	// 防止重复流ID
	if _, exist := s.streams.Load(id); exist {
		return
	}

	// 发送流ID给客户端
	if _, err := (*stream).Write(rawID); err != nil {
		return
	}

	// 尝试放入分片通道和全局通道
	select {
	case s.idChan <- id:
		select {
		case globalChan <- id:
			s.streams.Store(id, stream)
			streamClosed = true
		default:
			<-s.idChan
			return
		}
	default:
		return
	}
}

// establishConnection 为分片建立QUIC连接
// ctx 上下文
// addrResolver 地址解析器
// tlsCode TLS安全模式代码
// hostname 主机名
// keepAlive 保活间隔
func (s *Shard) establishConnection(ctx context.Context, addrResolver func() (string, error), tlsCode, hostname string, keepAlive time.Duration) error {
	conn := s.quicConn.Load()
	if conn != nil {
		if conn.Context().Err() == nil {
			return nil
		}
		s.quicConn.Store(nil)
	}

	targetAddr, err := addrResolver()
	if err != nil {
		return fmt.Errorf("establishConnection: address resolution failed: %w", err)
	}

	tlsConfig := buildTLSConfig(tlsCode, hostname)
	quicConfig := buildQUICConfig(keepAlive)

	newConn, err := quic.DialAddr(ctx, targetAddr, tlsConfig, quicConfig)
	if err != nil {
		return err
	}

	s.quicConn.Store(newConn)
	return nil
}

// startListener 为分片启动QUIC监听器
// listenAddr 监听地址
// tlsConfig TLS配置
// keepAlive 保活间隔
func (s *Shard) startListener(listenAddr string, tlsConfig *tls.Config, keepAlive time.Duration) error {
	if s.quicListener.Load() != nil {
		return nil
	}
	if tlsConfig == nil {
		return fmt.Errorf("startListener: server mode requires TLS config")
	}

	clonedTLS := tlsConfig.Clone()
	clonedTLS.NextProtos = []string{defaultALPN}
	clonedTLS.MinVersion = tls.VersionTLS13

	quicConfig := buildQUICConfig(keepAlive)
	newListener, err := quic.ListenAddr(listenAddr, clonedTLS, quicConfig)
	if err != nil {
		return fmt.Errorf("startListener[shard %d]: %w", s.index, err)
	}

	s.quicListener.Store(newListener)
	return nil
}

// ClientManager 客户端QUIC池管理器
func (p *Pool) ClientManager() {
	if p.cancel != nil {
		p.cancel()
	}
	p.ctx, p.cancel = context.WithCancel(context.Background())

	go p.manageAllShardsClient()
}

// manageAllShardsClient 管理所有分片的客户端连接和流创建
func (p *Pool) manageAllShardsClient() {
	for p.ctx.Err() == nil {
		p.adjustInterval()
		capacity := int(p.capacity.Load())

		// 计算当前总共有多少流
		totalStreams := 0
		for _, shard := range p.shards {
			totalStreams += len(shard.idChan)
		}

		need := capacity - totalStreams
		created := 0

		if need > 0 {
			// 按顺序填充分片
			for _, shard := range p.shards {
				if need <= 0 {
					break
				}

				// 确保分片连接已建立
				if err := shard.establishConnection(p.ctx, p.addrResolver, p.tlsCode, p.hostname, p.keepAlive); err != nil {
					time.Sleep(reconnectRetryInterval)
					continue
				}

				// 计算该分片可创建的流数量
				shardAvailable := shard.maxStreams - len(shard.idChan)
				if shardAvailable <= 0 {
					continue
				}

				// 本次在该分片创建的流数量
				toCreate := min(need, shardAvailable)

				var shardWg sync.WaitGroup
				results := make(chan int, toCreate)
				for range toCreate {
					shardWg.Add(1)
					go func() {
						defer shardWg.Done()
						if shard.createStream(p.ctx, p.idChan) {
							results <- 1
						}
					}()
				}
				shardWg.Wait()
				close(results)

				shardCreated := 0
				for r := range results {
					shardCreated += r
				}

				created += shardCreated
				need -= shardCreated
			}
		}

		p.adjustCapacity(created)

		select {
		case <-p.ctx.Done():
			return
		case <-time.After(time.Duration(p.interval.Load())):
		}
	}
}

// ServerManager 服务端QUIC池管理器
func (p *Pool) ServerManager() {
	if p.cancel != nil {
		p.cancel()
	}
	p.ctx, p.cancel = context.WithCancel(context.Background())

	// 为每个分片启动独立的监听器（在后台运行，不阻塞）
	for i := range p.shards {
		go func(shard *Shard) {
			p.manageShardServer(shard)
		}(p.shards[i])
	}
}

// manageShardServer 管理单个分片的服务端监听和流接收
// shard 分片
func (p *Pool) manageShardServer(shard *Shard) {
	// 启动分片的QUIC监听器
	if err := shard.startListener(p.listenAddr, p.tlsConfig, p.keepAlive); err != nil {
		return
	}

	// 接受QUIC连接
	for p.ctx.Err() == nil {
		listener := shard.quicListener.Load()
		if listener == nil {
			return
		}

		conn, err := listener.Accept(p.ctx)
		if err != nil {
			if p.ctx.Err() != nil {
				return
			}
			select {
			case <-p.ctx.Done():
				return
			case <-time.After(acceptRetryInterval):
			}
			continue
		}

		// 验证客户端IP
		if p.clientIP != "" {
			remoteAddr := conn.RemoteAddr().(*net.UDPAddr)
			if remoteAddr.IP.String() != p.clientIP {
				conn.CloseWithError(0, "unauthorized IP")
				continue
			}
		}

		// 存储连接并接受流
		shard.quicConn.Store(conn)

		go func(c *quic.Conn) {
			for p.ctx.Err() == nil {
				stream, err := (*c).AcceptStream(p.ctx)
				if err != nil {
					return
				}
				go shard.handleStream(stream, p.idChan, p.maxCap, p.Active)
			}
		}(conn)
	}
}

// OutgoingGet 根据ID获取可用流
// id 流ID
// timeout 超时时间
func (p *Pool) OutgoingGet(id string, timeout time.Duration) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(p.ctx, timeout)
	defer cancel()

	for {
		if streamConn, ok := p.findStreamInShards(id, true); ok {
			<-p.idChan
			return streamConn, nil
		}

		select {
		case <-time.After(idRetryInterval):
		case <-ctx.Done():
			return nil, fmt.Errorf("OutgoingGet: stream not found")
		}
	}
}

// IncomingGet 获取可用流并返回ID
// timeout 超时时间
func (p *Pool) IncomingGet(timeout time.Duration) (string, net.Conn, error) {
	ctx, cancel := context.WithTimeout(p.ctx, timeout)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			return "", nil, fmt.Errorf("IncomingGet: insufficient streams")
		case id := <-p.idChan:
			if streamConn, ok := p.findStreamInShards(id, true); ok {
				return id, streamConn, nil
			}
			continue
		}
	}
}

// Flush 清空池中的所有流
func (p *Pool) Flush() {
	var wg sync.WaitGroup
	for _, shard := range p.shards {
		wg.Add(1)
		go func(s *Shard) {
			defer wg.Done()
			s.flushShard()
		}(shard)
	}
	wg.Wait()

	// 重建全局通道
	p.idChan = make(chan string, p.maxCap)
}

// Close 关闭连接池并释放资源
func (p *Pool) Close() {
	if p.cancel != nil {
		p.cancel()
	}
	p.Flush()

	// 并行关闭所有分片的连接和监听器
	var wg sync.WaitGroup
	for _, shard := range p.shards {
		wg.Add(1)
		go func(s *Shard) {
			defer wg.Done()
			s.closeShard()
		}(shard)
	}
	wg.Wait()
}

// Ready 检查连接池是否已初始化
func (p *Pool) Ready() bool {
	return p.ctx != nil
}

// Active 获取当前活跃流数
func (p *Pool) Active() int {
	return len(p.idChan)
}

// ShardCount 获取连接分片数量
func (p *Pool) ShardCount() int {
	return p.numShards
}

// Capacity 获取当前池容量
func (p *Pool) Capacity() int {
	return int(p.capacity.Load())
}

// Interval 获取当前流创建间隔
func (p *Pool) Interval() time.Duration {
	return time.Duration(p.interval.Load())
}

// AddError 增加错误计数
func (p *Pool) AddError() {
	p.errCount.Add(1)
}

// ErrorCount 获取错误计数
func (p *Pool) ErrorCount() int {
	return int(p.errCount.Load())
}

// ResetError 重置错误计数
func (p *Pool) ResetError() {
	p.errCount.Store(0)
}

// adjustInterval 根据池使用情况动态调整流创建间隔
func (p *Pool) adjustInterval() {
	idle := len(p.idChan)
	capacity := int(p.capacity.Load())
	interval := time.Duration(p.interval.Load())

	if idle < int(float64(capacity)*intervalLowThreshold) && interval > p.minIvl {
		newInterval := maxDuration(interval-intervalAdjustStep, p.minIvl)
		p.interval.Store(int64(newInterval))
	}

	if idle > int(float64(capacity)*intervalHighThreshold) && interval < p.maxIvl {
		newInterval := minDuration(interval+intervalAdjustStep, p.maxIvl)
		p.interval.Store(int64(newInterval))
	}
}

// adjustCapacity 根据创建成功率动态调整池容量
func (p *Pool) adjustCapacity(created int) {
	capacity := int(p.capacity.Load())
	ratio := float64(created) / float64(capacity)

	if ratio < capacityAdjustLowRatio && capacity > p.minCap {
		p.capacity.Add(-1)
	}

	if ratio > capacityAdjustHighRatio && capacity < p.maxCap {
		p.capacity.Add(1)
	}
}

// generateID 生成唯一流ID
func (s *Shard) generateID() ([]byte, string, error) {
	if s.first.CompareAndSwap(false, true) {
		return []byte{0, 0, 0, 0}, "00000000", nil
	}

	rawID := make([]byte, 4)
	if _, err := rand.Read(rawID); err != nil {
		return nil, "", err
	}
	id := hex.EncodeToString(rawID)
	return rawID, id, nil
}
