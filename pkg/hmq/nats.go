package hmq

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hrpc"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// WithContext 给上下文添加 NATS 连接
func WithContext(ctx context.Context, n *Nats) context.Context {
	return &Context{
		Context: ctx,
		nats:    n,
	}
}

// Context 定义了 NATS 的上下文
type Context struct {
	context.Context
	nats *Nats
}

var contextType = reflect.TypeOf(&Context{})

// Value 返回Context的value
func (d *Context) Value(key any) any {
	if reflect.TypeOf(d) == key {
		return d
	}
	return d.Context.Value(key)
}

// FromContext 从上下文中获取 NATS 连接
func FromContext(ctx context.Context) (n *Nats, ok bool) {
	val := ctx.Value(contextType)
	if val == nil {
		return nil, false
	}
	return val.(*Context).nats, true
}

// //////////////////////////////////////////////////////////////////////////////////////////////////////////////////////
type Option func(*Nats)

func WithMiddleware(middlewares ...hrpc.HandlerMiddleware) Option {
	return func(s *Nats) {
		s.middleware = func(next hrpc.Handler) hrpc.Handler {
			for i := len(middlewares) - 1; i >= 0; i-- {
				next = middlewares[i](next)
			}
			return next
		}
	}
}

// NewNats 定义了 NATS 的配置
func NewNats(options ...Option) *Nats {
	ret := &Nats{
		stream:     make(map[string]struct{}),
		ackWait:    time.Second * 10,
		maxDeliver: 3,
		middleware: func(next hrpc.Handler) hrpc.Handler {
			return next
		},
	}
	for _, opt := range options {
		opt(ret)
	}
	return ret
}

// Nats 定义了 NATS 的连接
type Nats struct {
	conn       atomic.Pointer[nats.Conn]
	js         atomic.Pointer[jetstream.JetStream]
	stream     map[string]struct{}
	lock       sync.RWMutex
	cfg        *Config
	ackWait    time.Duration // 未返回ack 30秒后重发
	maxDeliver int           //	最大重试发送次数
	ServerName string
	Version    string
	config     *Config
	middleware func(next hrpc.Handler) hrpc.Handler
}

// SetConfig 设置配置
func (d *Nats) SetConfig(cfg *Config) error {
	if d.config.Equal(cfg) {
		return nil
	}

	old := d.conn.Load()
	defer func() {
		if old != nil {
			<-time.After(time.Second * 30)
			old.Close()
			hlog.Info("old etcd client closed")
		}
	}()

	if cfg == nil {
		if old != nil {
			conn := d.conn.Swap(nil)
			conn.Close()
		}
		d.config = nil
		return nil
	}

	d.config = cfg
	// 连接到 NATS 服务器
	var options []nats.Option
	if cfg.User != nil && cfg.Password != nil {
		options = append(options, nats.UserInfo(*cfg.User, *cfg.Password))
	}
	if cfg.NoRandomize != nil && *cfg.NoRandomize {
		options = append(options, nats.DontRandomize())
	}
	if cfg.NoEcho != nil && *cfg.NoEcho {
		options = append(options, nats.NoEcho())
	}
	if cfg.Name != nil {
		options = append(options, nats.Name(*cfg.Name))
	}
	if cfg.Secure != nil && *cfg.Secure {
		options = append(options, nats.TLSHandshakeFirst())
	}
	if cfg.AllowReconnect != nil && !*cfg.AllowReconnect {
		options = append(options, nats.NoReconnect())
	}
	if cfg.MaxReconnect != nil {
		options = append(options, nats.MaxReconnects(*cfg.MaxReconnect))
	}
	if cfg.ReconnectWait != nil {
		options = append(options, nats.ReconnectWait(*cfg.ReconnectWait))
	}
	if cfg.ReconnectJitter != nil && cfg.ReconnectJitterTLS != nil {
		options = append(options, nats.ReconnectJitter(*cfg.ReconnectJitter, *cfg.ReconnectJitterTLS))
	}
	if cfg.Timeout != nil {
		options = append(options, nats.Timeout(*cfg.Timeout))
	}
	if cfg.DrainTimeout != nil {
		options = append(options, nats.DrainTimeout(*cfg.DrainTimeout))
	}
	if cfg.FlusherTimeout != nil {
		options = append(options, nats.FlusherTimeout(*cfg.FlusherTimeout))
	}
	if cfg.PingInterval != nil {
		options = append(options, nats.PingInterval(*cfg.PingInterval))
	}
	if cfg.MaxPingsOut != nil {
		options = append(options, nats.MaxPingsOutstanding(*cfg.MaxPingsOut))
	}
	if cfg.ReconnectBufSize != nil {
		options = append(options, nats.ReconnectBufSize(*cfg.ReconnectBufSize))
	}
	if cfg.SubChanLen != nil {
		options = append(options, nats.SyncQueueLen(*cfg.SubChanLen))
	}
	if cfg.Token != nil {
		options = append(options, nats.Token(*cfg.Token))
	}
	if cfg.UseOldRequestStyle != nil && *cfg.UseOldRequestStyle {
		options = append(options, nats.UseOldRequestStyle())
	}
	if cfg.NoCallbacksAfterClientClose != nil && *cfg.NoCallbacksAfterClientClose {
		options = append(options, nats.NoCallbacksAfterClientClose())
	}
	if cfg.RetryOnFailedConnect != nil {
		options = append(options, nats.RetryOnFailedConnect(*cfg.RetryOnFailedConnect))
	}
	if cfg.Compression != nil {
		options = append(options, nats.Compression(*cfg.Compression))
	}
	if cfg.ProxyPath != nil {
		options = append(options, nats.ProxyPath(*cfg.ProxyPath))
	}
	if cfg.InboxPrefix != nil {
		options = append(options, nats.CustomInboxPrefix(*cfg.InboxPrefix))
	}
	if cfg.IgnoreAuthErrorAbort != nil && *cfg.IgnoreAuthErrorAbort {
		options = append(options, nats.IgnoreAuthErrorAbort())
	}
	if cfg.SkipHostLookup != nil && *cfg.SkipHostLookup {
		options = append(options, nats.SkipHostLookup())
	}

	nc, err := nats.Connect(
		strings.Join(cfg.Servers, ","),
		options...,
	)
	if err != nil {
		return herror.Wrap(err)
	}

	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return herror.Wrap(err)
	}

	d.conn.Store(nc)
	d.js.Store(&js)
	return nil
}

// Close 关闭 NATS 连接
func (n *Nats) Close() {
	conn, err := n.GetConn()
	if err != nil {
		return
	}
	conn.Close()
}

// Publish 发布消息到指定的主题
func (n *Nats) Publish(ctx context.Context, subject string, data []byte) error {
	conn, err := n.GetConn()
	if err != nil {
		return err
	}
	err = conn.Publish(subject, data)
	if err != nil {
		return err
	}
	return nil
}

// Publish 发布消息到指定的主题
func Publish[T any](ctx context.Context, subject string, msg *T) error {
	n, ok := FromContext(ctx)
	if !ok {
		return herror.NewError("nats not initialized")
	}
	jsonData, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	conn, err := n.GetConn()
	if err != nil {
		return err
	}
	return conn.Publish(subject, jsonData)
}

// Subscribe 订阅指定的主题
func (n *Nats) Subscribe(ctx context.Context, subject string, callback func(ctx context.Context, msg *nats.Msg) error) (*nats.Subscription, error) {
	conn, err := n.GetConn()
	if err != nil {
		return nil, err
	}
	subscription, err := conn.Subscribe(subject, func(msg *nats.Msg) {
		_, err := n.middleware(func(ctx context.Context, req any) (any, error) {
			return nil, callback(ctx, msg)
		})(context.TODO(), nil)
		if err != nil {
			herror.PrintStack(err)
		}
	})
	if err != nil {
		hlog.Error("subscribe failed, error: %s", err)
		return nil, err
	}
	return subscription, nil
}

// Subscribe 订阅指定的主题
func Subscribe[T any](ctx context.Context, subject string, callback func(ctx context.Context, msg *T) error) (*nats.Subscription, error) {
	n, ok := FromContext(ctx)
	if !ok {
		return nil, herror.NewError("nats not initialized")
	}

	subscription, err := n.Subscribe(ctx, subject, func(ctx context.Context, msg *nats.Msg) error {
		var data T
		err := json.Unmarshal(msg.Data, &data)
		if err != nil {
			return herror.Wrap(err)
		}
		return callback(ctx, &data)
	})
	if err != nil {
		return nil, err
	}
	return subscription, nil
}

type PublishOption func(*nats.Header)

const DurableHeader = "durable-header"

func WithPublishMsgId(msgId string) PublishOption {
	return func(h *nats.Header) {
		h.Set(jetstream.MsgIDHeader, msgId)
	}
}

func WithPublishDurable(msgId string) PublishOption {
	return func(h *nats.Header) {
		h.Set(DurableHeader, msgId)
	}
}

// JetStreamPublish 发布消息到指定的主题
func (n *Nats) JetStreamPublish(ctx context.Context, stream, subject string, data []byte, options ...PublishOption) (*jetstream.PubAck, error) {
	err := n.checkStream(ctx, stream, subject)
	if err != nil {
		return nil, err
	}

	jetStream, err := n.GetJetStream()
	if err != nil {
		return nil, err
	}

	msg := &nats.Msg{Subject: subject, Data: data, Header: nats.Header{}}
	msg.Header.Set(jetstream.MsgIDHeader, uuid.NewString())
	for _, opt := range options {
		opt(&msg.Header)
	}

	pubAck, err := jetStream.PublishMsg(ctx, msg)
	if err != nil {
		hlog.Error("publish failed, error: %s", err)
		return nil, err
	}
	return pubAck, nil
}

// JetStreamPublish 发布消息到指定的主题
func JetStreamPublish[T any](ctx context.Context, stream, subject string, msg *T, options ...PublishOption) (*jetstream.PubAck, error) {
	n, ok := FromContext(ctx)
	if !ok {
		return nil, herror.NewError("nats not initialized")
	}

	jsonData, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}
	pubAck, err := n.JetStreamPublish(context.TODO(), stream, subject, jsonData, options...)
	if err != nil {
		return nil, err
	}
	return pubAck, nil
}

func (n *Nats) GetJetStream() (jetstream.JetStream, error) {
	if n.js.Load() == nil {
		return nil, herror.NewError("nats not initialized")
	}
	return *n.js.Load(), nil
}

func (n *Nats) GetConn() (*nats.Conn, error) {
	if n.conn.Load() == nil {
		return nil, herror.NewError("nats not initialized")
	}
	return n.conn.Load(), nil
}

type SubscribeOption func(*jetstream.ConsumerConfig)

func WithSubscribeMaxDeliver(val int) SubscribeOption {
	return func(config *jetstream.ConsumerConfig) {
		config.MaxDeliver = val
	}
}

func WithSubscribeAckWait(val time.Duration) SubscribeOption {
	return func(config *jetstream.ConsumerConfig) {
		config.AckWait = val
	}
}

func WithSubscribeMaxAckPending(val int) SubscribeOption {
	return func(config *jetstream.ConsumerConfig) {
		config.MaxAckPending = val
	}
}

func WithSubscribeRateLimit(val uint64) SubscribeOption {
	return func(config *jetstream.ConsumerConfig) {
		config.RateLimit = val
	}
}

// JetStreamSubscribe 订阅指定的主题
func (n *Nats) JetStreamSubscribe(ctx context.Context, stream, subject, durable string, callback func(ctx context.Context, msgId string, msg jetstream.Msg) error, options ...SubscribeOption) error {
	err := n.checkStream(ctx, stream, subject)
	if err != nil {
		return err
	}
	jetStream, err := n.GetJetStream()
	if err != nil {
		return err
	}

	config := jetstream.ConsumerConfig{
		Durable:       durable,
		AckPolicy:     jetstream.AckExplicitPolicy,
		FilterSubject: subject,
		AckWait:       n.ackWait,    // 未返回ack 30秒后重发
		MaxDeliver:    n.maxDeliver, // 最大重试发送次数
	}
	for _, opt := range options {
		opt(&config)
	}
	consumer, err := jetStream.CreateOrUpdateConsumer(ctx, stream, config)

	if err != nil {
		return err
	}

	_, err = consumer.Consume(func(msg jetstream.Msg) {
		durableHeader := msg.Headers().Get(DurableHeader)
		if len(durableHeader) > 0 && durableHeader != durable {
			err = msg.Ack()
			if err != nil {
				hlog.Error("ack failed, error: %s", err)
				return
			}
			return
		}
		msgId := msg.Headers().Get(jetstream.MsgIDHeader)
		_, retErr := n.middleware(func(ctx context.Context, req any) (any, error) {
			return nil, callback(ctx, msgId, msg)
		})(context.TODO(), nil)
		if retErr != nil {
			hlog.Error("callback failed, error: %s", err)
			metadata, err := msg.Metadata()
			if err != nil {
				hlog.Error("metadata failed, error: %s", err)
				return
			}
			if int(metadata.NumDelivered) >= n.maxDeliver {
				n.saveErrorMessage(ctx, stream, subject, durable, msgId, msg.Data(), retErr.Error())
				err = msg.Ack()
				if err != nil {
					hlog.Error("ack failed, error: %s", err)
					return
				}
			}
			return
		}
		err = msg.Ack()
		if err != nil {
			hlog.Error("ack failed, error: %s", err)
			return
		}
	})
	if err != nil {
		hlog.Error("commit failed, error: %s", err)
		return err
	}
	return nil
}

// JetStreamSubscribe 订阅指定的主题
func JetStreamSubscribe[T any](ctx context.Context, stream, subject, durable string, callback func(ctx context.Context, msgId string, msg *T) error, options ...SubscribeOption) error {
	n, ok := FromContext(ctx)
	if !ok {
		return herror.NewError("nats not initialized")
	}

	err := n.JetStreamSubscribe(ctx, stream, subject, durable, func(ctx context.Context, msgId string, msg jetstream.Msg) error {
		var data T
		err := json.Unmarshal(msg.Data(), &data)
		if err != nil {
			return err
		}
		return callback(ctx, msgId, &data)
	}, options...)
	if err != nil {
		return err
	}
	return nil
}

// checkStream 检查指定的主题是否存在
func (n *Nats) checkStream(ctx context.Context, stream string, subject string) error {
	n.lock.RLock()
	_, ok := n.stream[stream+"_"+subject]
	n.lock.RUnlock()
	if ok {
		return nil
	}

	n.lock.Lock()
	defer n.lock.Unlock()
	_, ok = n.stream[stream+"_"+subject]
	if ok {
		return nil
	}

	jetStream, err := n.GetJetStream()
	if err != nil {
		return err
	}

	_, err = jetStream.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      stream,
		Subjects:  []string{subject},
		Retention: jetstream.InterestPolicy,
	})
	if err != nil {
		return err
	}
	n.stream[stream+"_"+subject] = struct{}{}
	return nil
}

// ErrorMessage 错误信息
type ErrorMessage struct {
	Stream  string `json:"stream"`  // 流名
	Subject string `json:"subject"` // 主题
	Durable string `json:"durable"` // 消费者
	MsgId   string `json:"msgId"`   // 消息ID
	Data    string `json:"data"`    // 消息数据
	Err     string `json:"err"`     // 错误信息
	Server  string `json:"server"`  // 服务器
	Retry   int    `json:"retry"`   //重试次数
}

const (
	ErrorMessage_Stream  = "nats-subscribe"
	ErrorMessage_Subject = "subscribe-error"
)

// saveErrorMessage 保存错误信息
func (n *Nats) saveErrorMessage(ctx context.Context, stream string, subject string, durable string, msgId string, data []byte, errString string) {
	msg := &ErrorMessage{
		Stream:  stream,
		Subject: subject,
		Durable: durable,
		MsgId:   msgId,
		Data:    string(data),
		Err:     errString,
		Server:  n.ServerName + " " + n.Version,
		Retry:   n.maxDeliver,
	}
	jsonData, err := json.Marshal(msg)
	if err != nil {
		hlog.Error("marshal failed, error: %s", err)
		return
	}

	jetStream, err := n.GetJetStream()
	if err != nil {
		hlog.Error("get jetstream failed, error: %s", err)
		return
	}
	_, err = jetStream.Publish(ctx, ErrorMessage_Subject, jsonData, jetstream.WithMsgID(uuid.NewString()))
	if err != nil {
		hlog.Error("publish failed, error: %s", err)
		return
	}
}

// ErrorMessageSubscribe 订阅错误信息
func (n *Nats) ErrorMessageSubscribe(ctx context.Context, callback func(ctx context.Context, msgId string, msg *ErrorMessage) error) error {
	err := n.checkStream(ctx, ErrorMessage_Stream, ErrorMessage_Subject)
	if err != nil {
		return err
	}
	jetStream, err := n.GetJetStream()
	if err != nil {
		return err
	}
	consumer, err := jetStream.CreateOrUpdateConsumer(ctx, ErrorMessage_Stream, jetstream.ConsumerConfig{
		Durable:       "store",
		AckPolicy:     jetstream.AckExplicitPolicy,
		FilterSubject: ErrorMessage_Subject,
		AckWait:       n.ackWait,    // 未返回ack 30秒后重发
		MaxDeliver:    n.maxDeliver, // 最大重试发送次数
	})
	if err != nil {
		return err
	}

	_, err = consumer.Consume(func(msg jetstream.Msg) {
		metadata, err := msg.Metadata()
		if err != nil {
			hlog.Error("metadata failed, error: %s", err)
			return
		}

		msgId := msg.Headers().Get(jetstream.MsgIDHeader)
		_, err = n.middleware(func(ctx context.Context, req any) (any, error) {
			var data ErrorMessage
			err := json.Unmarshal(msg.Data(), &data)
			if err != nil {
				return nil, herror.Wrap(err)
			}
			return nil, callback(ctx, msgId, &data)
		})(context.TODO(), nil)
		if err != nil {
			if int(metadata.NumDelivered) >= n.maxDeliver {
				hlog.Error("callback failed, error: %s", string(msg.Data()))

				err = msg.Ack()
				if err != nil {
					hlog.Error("ack failed, error: %s", err)
					return
				}
			}
			return
		}
		err = msg.Ack()
		if err != nil {
			hlog.Error("ack failed, error: %s", err)
			return
		}
	})
	if err != nil {
		hlog.Error("commit failed, error: %s", err)
		return err
	}
	return nil
}

// NewMiddleware 创建中间件
func (n *Nats) NewMiddleware() hrpc.HandlerMiddleware {
	return func(next hrpc.Handler) hrpc.Handler {
		return func(ctx context.Context, req any) (any, error) {
			return next(WithContext(ctx, n), req)
		}
	}
}
