package message

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
)

// Message 消息结构 - 参考 MetaGPT 和 AutoGen 的设计
type Message struct {
	ID          string                 `json:"id"`
	Type        MessageType            `json:"type"`
	From        string                 `json:"from"`         // 发送者 Agent ID
	To          string                 `json:"to"`           // 接收者 Agent ID (空表示广播)
	TaskID      string                 `json:"task_id"`      // 关联的任务 ID
	SessionID   string                 `json:"session_id"`   // 会话 ID
	Content     map[string]interface{} `json:"content"`      // 消息内容
	Metadata    map[string]interface{} `json:"metadata"`     // 元数据
	Timestamp   int64                  `json:"timestamp"`
	TTL         int                    `json:"ttl"`          // 消息存活时间(秒)
	Priority    int                    `json:"priority"`     // 优先级 0-9
}

type MessageType string

const (
	// 任务相关
	MessageTypeTaskAssign   MessageType = "task.assign"     // 分配任务
	MessageTypeTaskStart    MessageType = "task.start"      // 开始任务
	MessageTypeTaskProgress MessageType = "task.progress"   // 任务进度
	MessageTypeTaskComplete MessageType = "task.complete"   // 任务完成
	MessageTypeTaskFail     MessageType = "task.fail"       // 任务失败

	// 协作相关
	MessageTypeRequest  MessageType = "collab.request"   // 请求协助
	MessageTypeResponse MessageType = "collab.response"  // 响应请求
	MessageTypeBroadcast MessageType = "collab.broadcast" // 广播消息

	// 系统相关
	MessageTypeHeartbeat MessageType = "sys.heartbeat"  // 心跳
	MessageTypeRegister  MessageType = "sys.register"   // 注册
	MessageTypeStatus    MessageType = "sys.status"     // 状态更新
	MessageTypeConfig    MessageType = "sys.config"     // 配置更新

	// 人机交互
	MessageTypeUserInput  MessageType = "user.input"   // 用户输入
	MessageTypeUserFeedback MessageType = "user.feedback" // 用户反馈
)

// MessageBus 消息总线接口
type MessageBus interface {
	// 发布/订阅
	Publish(ctx context.Context, msg *Message) error
	Subscribe(subject string, handler MessageHandler) (Subscription, error)
	SubscribeQueue(subject, queue string, handler MessageHandler) (Subscription, error)

	// 请求/响应
	Request(ctx context.Context, subject string, msg *Message, timeout time.Duration) (*Message, error)
	Reply(msg *Message, reply *Message) error

	// 流处理
	PublishStream(subject string, msgs <-chan *Message) error
	SubscribeStream(subject string, handler MessageHandler) (Subscription, error)

	// 管理
	Close() error
	Health() error
}

// MessageHandler 消息处理函数
type MessageHandler func(msg *Message) error

// Subscription 订阅接口
type Subscription interface {
	Unsubscribe() error
}

// NATSMessageBus NATS 实现的消息总线
type NATSMessageBus struct {
	conn      *nats.Conn
	js        nats.JetStreamContext
	subscriptions []Subscription
	mu        sync.RWMutex
}

// NewNATSMessageBus 创建 NATS 消息总线
func NewNATSMessageBus(url string) (*NATSMessageBus, error) {
	// 连接 NATS
	conn, err := nats.Connect(url,
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(10),
		nats.ReconnectWait(time.Second),
		nats.DisconnectErrHandler(func(nc *nats.Conn, err error) {
			log.Printf("NATS disconnected: %v", err)
		}),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			log.Printf("NATS reconnected to %s", nc.ConnectedUrl())
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to NATS: %w", err)
	}

	// 创建 JetStream 上下文
	js, err := conn.JetStream()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to create JetStream context: %w", err)
	}

	// 初始化流
	if err := initStreams(js); err != nil {
		log.Printf("Failed to init streams: %v", err)
	}

	return &NATSMessageBus{
		conn: conn,
		js:   js,
	}, nil
}

// initStreams 初始化 JetStream 流
func initStreams(js nats.JetStreamContext) error {
	streams := []nats.StreamConfig{
		{
			Name:     "TASKS",
			Subjects: []string{"task.>", "collab.>"},
			Retention: nats.WorkQueuePolicy,
			MaxMsgs:  10000,
			MaxAge:   24 * time.Hour,
		},
		{
			Name:     "SYSTEM",
			Subjects: []string{"sys.>"},
			Retention: nats.LimitsPolicy,
			MaxMsgs:  1000,
			MaxAge:   1 * time.Hour,
		},
		{
			Name:     "USER",
			Subjects: []string{"user.>"},
			Retention: nats.LimitsPolicy,
			MaxMsgs:  1000,
			MaxAge:   24 * time.Hour,
		},
	}

	for _, cfg := range streams {
		_, err := js.AddStream(&cfg)
		if err != nil && err != nats.ErrStreamNameAlreadyInUse {
			return fmt.Errorf("failed to create stream %s: %w", cfg.Name, err)
		}
	}

	return nil
}

// Publish 发布消息
func (b *NATSMessageBus) Publish(ctx context.Context, msg *Message) error {
	if msg.ID == "" {
		msg.ID = generateID()
	}
	if msg.Timestamp == 0 {
		msg.Timestamp = time.Now().UnixMilli()
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	subject := b.buildSubject(msg)

	// 根据消息类型选择发布方式
	switch msg.Type {
	case MessageTypeTaskAssign, MessageTypeTaskStart, MessageTypeTaskProgress,
		 MessageTypeTaskComplete, MessageTypeTaskFail:
		// 任务消息使用 JetStream 持久化
		_, err = b.js.Publish(subject, data)
	case MessageTypeHeartbeat:
		// 心跳消息不持久化
		err = b.conn.Publish(subject, data)
	default:
		// 默认使用 JetStream
		_, err = b.js.Publish(subject, data)
	}

	if err != nil {
		return fmt.Errorf("failed to publish message: %w", err)
	}

	return nil
}

// Subscribe 订阅消息
func (b *NATSMessageBus) Subscribe(subject string, handler MessageHandler) (Subscription, error) {
	sub, err := b.conn.Subscribe(subject, func(m *nats.Msg) {
		msg, err := parseMessage(m.Data)
		if err != nil {
			log.Printf("Failed to parse message: %v", err)
			return
		}

		if err := handler(msg); err != nil {
			log.Printf("Handler error: %v", err)
		}
	})
	if err != nil {
		return nil, err
	}

	b.mu.Lock()
	b.subscriptions = append(b.subscriptions, &natsSubscription{sub: sub})
	b.mu.Unlock()

	return &natsSubscription{sub: sub}, nil
}

// SubscribeQueue 队列订阅 (负载均衡)
func (b *NATSMessageBus) SubscribeQueue(subject, queue string, handler MessageHandler) (Subscription, error) {
	sub, err := b.conn.QueueSubscribe(subject, queue, func(m *nats.Msg) {
		msg, err := parseMessage(m.Data)
		if err != nil {
			log.Printf("Failed to parse message: %v", err)
			return
		}

		if err := handler(msg); err != nil {
			log.Printf("Handler error: %v", err)
		}
	})
	if err != nil {
		return nil, err
	}

	return &natsSubscription{sub: sub}, nil
}

// Request 请求-响应模式
func (b *NATSMessageBus) Request(ctx context.Context, subject string, msg *Message, timeout time.Duration) (*Message, error) {
	data, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal message: %w", err)
	}

	resp, err := b.conn.RequestWithContext(ctx, subject, data)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	return parseMessage(resp.Data)
}

// Reply 回复消息
func (b *NATSMessageBus) Reply(msg *Message, reply *Message) error {
	// NATS 自动处理 reply-to
	return b.Publish(context.Background(), reply)
}

// PublishStream 发布流消息
func (b *NATSMessageBus) PublishStream(subject string, msgs <-chan *Message) error {
	go func() {
		for msg := range msgs {
			if err := b.Publish(context.Background(), msg); err != nil {
				log.Printf("Failed to publish stream message: %v", err)
			}
		}
	}()
	return nil
}

// SubscribeStream 订阅流消息
func (b *NATSMessageBus) SubscribeStream(subject string, handler MessageHandler) (Subscription, error) {
	// 使用 JetStream 消费者
	sub, err := b.js.Subscribe(subject, func(m *nats.Msg) {
		msg, err := parseMessage(m.Data)
		if err != nil {
			log.Printf("Failed to parse message: %v", err)
			m.Nak()
			return
		}

		if err := handler(msg); err != nil {
			log.Printf("Handler error: %v", err)
			m.Nak()
			return
		}

		m.Ack()
	}, nats.Durable("consumer-"+generateID()), nats.ManualAck())

	if err != nil {
		return nil, err
	}

	return &natsSubscription{sub: sub}, nil
}

// Close 关闭连接
func (b *NATSMessageBus) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	// 取消所有订阅
	for _, sub := range b.subscriptions {
		sub.Unsubscribe()
	}

	b.conn.Close()
	return nil
}

// Health 健康检查
func (b *NATSMessageBus) Health() error {
	if b.conn.IsConnected() {
		return nil
	}
	return fmt.Errorf("NATS not connected")
}

// buildSubject 构建消息主题
func (b *NATSMessageBus) buildSubject(msg *Message) string {
	// 主题格式: <type>.<task_id>.<from>.<to>
	// 例如: task.assign.task-123.dev-1.dev-2

	if msg.To != "" {
		return fmt.Sprintf("%s.%s.%s.%s", msg.Type, msg.TaskID, msg.From, msg.To)
	}
	return fmt.Sprintf("%s.%s.%s", msg.Type, msg.TaskID, msg.From)
}

// natsSubscription NATS 订阅实现
type natsSubscription struct {
	sub *nats.Subscription
}

func (s *natsSubscription) Unsubscribe() error {
	return s.sub.Unsubscribe()
}

// parseMessage 解析消息
func parseMessage(data []byte) (*Message, error) {
	var msg Message
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

// generateID 生成唯一 ID
func generateID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// RedisMessageBus Redis 实现的消息总线 (简化版)
type RedisMessageBus struct {
	// TODO: 实现 Redis 版本
}

// InMemoryMessageBus 内存消息总线 (用于测试)
type InMemoryMessageBus struct {
	subscribers map[string][]MessageHandler
	mu          sync.RWMutex
}

func NewInMemoryMessageBus() *InMemoryMessageBus {
	return &InMemoryMessageBus{
		subscribers: make(map[string][]MessageHandler),
	}
}

func (b *InMemoryMessageBus) Publish(ctx context.Context, msg *Message) error {
	b.mu.RLock()
	handlers := b.subscribers[string(msg.Type)]
	b.mu.RUnlock()

	for _, handler := range handlers {
		go func(h MessageHandler) {
			if err := h(msg); err != nil {
				log.Printf("Handler error: %v", err)
			}
		}(handler)
	}

	return nil
}

func (b *InMemoryMessageBus) Subscribe(subject string, handler MessageHandler) (Subscription, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.subscribers[subject] = append(b.subscribers[subject], handler)

	return &inMemorySubscription{bus: b, subject: subject, handler: handler}, nil
}

func (b *InMemoryMessageBus) SubscribeQueue(subject, queue string, handler MessageHandler) (Subscription, error) {
	return b.Subscribe(subject, handler)
}

func (b *InMemoryMessageBus) Request(ctx context.Context, subject string, msg *Message, timeout time.Duration) (*Message, error) {
	return nil, fmt.Errorf("not implemented")
}

func (b *InMemoryMessageBus) Reply(msg *Message, reply *Message) error {
	return fmt.Errorf("not implemented")
}

func (b *InMemoryMessageBus) PublishStream(subject string, msgs <-chan *Message) error {
	return fmt.Errorf("not implemented")
}

func (b *InMemoryMessageBus) SubscribeStream(subject string, handler MessageHandler) (Subscription, error) {
	return nil, fmt.Errorf("not implemented")
}

func (b *InMemoryMessageBus) Close() error {
	return nil
}

func (b *InMemoryMessageBus) Health() error {
	return nil
}

type inMemorySubscription struct {
	bus     *InMemoryMessageBus
	subject string
	handler MessageHandler
}

func (s *inMemorySubscription) Unsubscribe() error {
	s.bus.mu.Lock()
	defer s.bus.mu.Unlock()

	handlers := s.bus.subscribers[s.subject]
	for i, h := range handlers {
		// 简单的比较，实际应该用更可靠的方式
		if fmt.Sprintf("%p", h) == fmt.Sprintf("%p", s.handler) {
			s.bus.subscribers[s.subject] = append(handlers[:i], handlers[i+1:]...)
			break
		}
	}
	return nil
}
