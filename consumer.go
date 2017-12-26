package gonsq

import (
	"errors"
	"time"

	"strings"

	"strconv"

	"github.com/nsqio/go-nsq"
	"github.com/vaughan0/go-ini"
	"go.zhuzi.me/log"
)

// 使用者可以使用快速的配置进行事件的消费
// 配置
// 生产者
// [producer]
// nsqd地址
// nsqd=127.0.0.1:4151
// 消费者
// [consumer]
// nsqd连接地址
// nsqd=127.0.0.1:4151
// nsqlookupd连接地址
// nsqlookupd=127.0.0.1:4161,127.0.0.2:4161,127.0.0.3:4161
// max_flight=100
// concurrent=20
// channel=chan1

// consumer 消费者结构体
type consumer struct {
	isInit      bool
	Debug       bool
	channelName string
	concurrent  int
	maxInFlight int
	//addr 连接地址
	nsqdAddr       []string
	nsqLookupdAddr []string
	// 各个topic的worker
	topics map[string]*topicInfo
}

// topicInfo topic 信息结构体
type topicInfo struct {
	topic         string
	maxInFlight   int
	concurrentNum int
	config        *nsq.Config
	handler       nsq.HandlerFunc
	consumer      *nsq.Consumer
}

// Connect 连接
func (t *topicInfo) Connect(channelName string, nsqdAddr []string, nsqlookupdAddr []string, debug bool) {
	if len(nsqdAddr) == 0 && len(nsqlookupdAddr) == 0 {
		log.Warning("nsqd和nsqlookupd地址皆为空，跳过连接,topic:", t.topic)
		return
	}
	var (
		retryNum     = 0
		sleepSeconds = 0
		err          error
	)
	t.consumer, err = nsq.NewConsumer(t.topic, channelName, t.config)
	if err != nil {
		log.Errorf("新建nsq consumer失败，err:%s,topic:%s,channel:%s", err.Error(), t.topic, channelName)
		return
	}
	// 不断进行重连，直到连接成功
	for {
		if len(nsqlookupdAddr) > 0 {
			if len(nsqlookupdAddr) == 1 {
				err = t.consumer.ConnectToNSQLookupd(nsqlookupdAddr[0])
			} else {
				err = t.consumer.ConnectToNSQLookupds(nsqlookupdAddr)
			}
		} else {
			if len(nsqdAddr) == 1 {
				err = t.consumer.ConnectToNSQD(nsqdAddr[0])
			} else {
				err = t.consumer.ConnectToNSQDs(nsqdAddr)
			}
		}
		if err != nil {
			log.Warningf("连接nsqlookupd(addr:%v)/nsqd(addr:%v)失败,err:%s", nsqlookupdAddr, nsqdAddr, err.Error())
			retryNum++
			sleepSeconds = 5
			if retryNum%6 == 0 {
				sleepSeconds = 30
			}
			time.Sleep(time.Duration(sleepSeconds) * time.Second)
			continue
		}
		if debug {
			t.consumer.SetLogger(log.GetLogger(), nsq.LogLevelDebug)
		} else {
			t.consumer.SetLogger(log.GetLogger(), nsq.LogLevelWarning)
		}
		log.Infof("连接nsqlookupd(addr:%v)/nsqd(%v)成功", nsqlookupdAddr, nsqdAddr)
		break
	}

	err = nil
	return
}

// newConsumer 新建消费者
func newConsumer() consumer {
	return consumer{
		nsqdAddr:       make([]string, 0),
		nsqLookupdAddr: make([]string, 0),
		topics:         make(map[string]*topicInfo),
	}
}

// AddHandler 添加handler
func (c *consumer) AddHandler(topic string, handler nsq.HandlerFunc) {
	var (
		t  = &topicInfo{}
		ok bool
	)
	if t, ok = c.topics[topic]; !ok {
		t.concurrentNum = c.concurrent
		t.maxInFlight = c.maxInFlight
		t.config = nsq.NewConfig()
	}

	t.topic = topic
	t.handler = handler
	c.topics[topic] = t
}

// SetAddr 设置consumer地址
func (c *consumer) SetNsqlookupdAddr(node, addr string) {
	exist := false
	for _, v := range c.nsqLookupdAddr {
		if v == addr {
			exist = true
			break
		}
	}
	if !exist {
		c.nsqLookupdAddr = append(c.nsqLookupdAddr, addr)
	}
}

// SetMultiNsqlookupdAddr 设置多个consumer地址
func (c *consumer) SetMultiNsqlookupdAddr(node string, addrArr []string) {
	for _, v := range addrArr {
		c.SetNsqlookupdAddr(node, v)
	}
}

// SetNsqdAddr
func (c *consumer) SetNsqdAddr(node, addr string) {
	exist := false
	for _, v := range c.nsqdAddr {
		if v == addr {
			exist = true
			break
		}
	}
	if !exist {
		c.nsqdAddr = append(c.nsqdAddr, addr)
	}
}

// SetMultiNsqdAddr
func (c *consumer) SetMultiNsqdAddr(node string, addrArr []string) {
	for _, v := range addrArr {
		c.SetNsqdAddr(node, v)
	}
}

// Stop 停止
func (c *consumer) Stop(node string) {
	if topicInfo, ok := c.topics[node]; ok {
		topicInfo.consumer.Stop()
	}
}

// StopAll 停止全部
func (c *consumer) StopAll() {
	for k := range c.topics {
		c.topics[k].consumer.Stop()
	}
}

// Run 运行
func (c *consumer) Run() (err error) {
	if !c.isInit {
		err = errors.New("consumer not init")
		return
	}
	if len(c.nsqdAddr) == 0 && len(c.nsqLookupdAddr) == 0 {
		err = errors.New("nsqd addr or nsqlookupd address required")
		return
	}
	for _, topicInfo := range c.topics {
		go topicInfo.Connect(c.channelName, c.nsqdAddr, c.nsqLookupdAddr, c.Debug)
	}

	return
}

// Init 初始化
func (c *consumer) Init(configSection ini.Section, debug bool) (err error) {
	if nsqdAddr, ok := configSection["nsqd"]; ok {
		Consumer.nsqdAddr = strings.Split(nsqdAddr, ",")
	}
	if nsqlookupdArr, ok := configSection["nsqlookupd"]; ok {
		Consumer.nsqLookupdAddr = strings.Split(nsqlookupdArr, ",")
	}
	if maxInFlight, _ := strconv.Atoi(configSection["maxInFlight"]); maxInFlight > 0 {
		Consumer.maxInFlight = maxInFlight
	}
	if concurrent, _ := strconv.Atoi(configSection["concurrent"]); concurrent > 0 {
		Consumer.concurrent = concurrent
	}

	if Consumer.maxInFlight < 1 {
		Consumer.maxInFlight = 1
	}
	if Consumer.concurrent < 1 {
		Consumer.concurrent = 1
	}

	if Consumer.maxInFlight < Consumer.concurrent {
		err = errors.New("max_in_flight should exceed than concurrent")
		return
	}
	Consumer.isInit = true

	return
}

var (
	Consumer = newConsumer()
)
