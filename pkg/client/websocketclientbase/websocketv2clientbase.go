package websocketclientbase

import (
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"github.com/huobirdcenter/huobi_golang/internal/gzip"
	"github.com/huobirdcenter/huobi_golang/internal/model"
	"github.com/huobirdcenter/huobi_golang/internal/requestbuilder"
	"github.com/huobirdcenter/huobi_golang/logging/applogger"
	"github.com/huobirdcenter/huobi_golang/pkg/model/auth"
	"github.com/huobirdcenter/huobi_golang/pkg/model/base"
)

const (
	websocketV2Path = "/ws/v2"
)

var (
	instanceId uint64 = 0
)

func GetInstanceName(t interface{}) string {
	id := atomic.AddUint64(&instanceId, 1)
	typeOf := reflect.TypeOf(t)

	return fmt.Sprintf("%s#%d", typeOf.String(), id)
}

// It will be invoked after websocket v2 authentication response received
type AuthenticationV2ResponseHandler func(resp *auth.WebSocketV2AuthenticationResponse)

// The base class that responsible to get data from websocket authentication v2
type WebSocketV2ClientBase struct {
	host string
	conn *websocket.Conn

	name string

	authenticationResponseHandler AuthenticationV2ResponseHandler
	messageHandler                MessageHandler
	responseHandler               ResponseHandler

	stopReadChannel   chan int
	stopTickerChannel chan int
	ticker            *time.Ticker
	lastReceivedTime  time.Time
	sendMutex         *sync.Mutex

	requestBuilder *requestbuilder.WebSocketV2RequestBuilder
}

// Initializer
func (p *WebSocketV2ClientBase) Init(name string, accessKey string, secretKey string, host string) *WebSocketV2ClientBase {
	p.name = name
	p.host = host
	p.stopReadChannel = make(chan int, 1)
	p.stopTickerChannel = make(chan int, 1)
	p.requestBuilder = new(requestbuilder.WebSocketV2RequestBuilder).Init(accessKey, secretKey, host, websocketV2Path)
	p.sendMutex = &sync.Mutex{}

	return p
}

// Set callback handler
func (p *WebSocketV2ClientBase) SetHandler(authHandler AuthenticationV2ResponseHandler, msgHandler MessageHandler, repHandler ResponseHandler) {
	p.authenticationResponseHandler = authHandler
	p.messageHandler = msgHandler
	p.responseHandler = repHandler
}

// Connect to websocket server
// if autoConnect is true, then the connection can be re-connect if no data received after the pre-defined timeout
func (p *WebSocketV2ClientBase) Connect(autoConnect bool) {
	// initialize last received time as now
	p.lastReceivedTime = time.Now()

	// connect to websocket
	p.connectWebSocket()

	// start ticker to manage connection
	if autoConnect {
		p.startTicker()
	}
}

// Send data to websocket server
func (p *WebSocketV2ClientBase) Send(data string) {
	if p.conn == nil {
		applogger.Error("name=%s conn=%p WebSocket sent error: no connection available", p.name, p.conn)
		return
	}

	p.sendMutex.Lock()
	err := p.conn.WriteMessage(websocket.TextMessage, []byte(data))
	p.sendMutex.Unlock()

	if err != nil {
		applogger.Error("name=%s conn=%p WebSocket sent error: data=%s, error=%s", p.name, p.conn, data, err)
	}
}

// Close the connection to server
func (p *WebSocketV2ClientBase) Close() {
	p.stopTicker()
	p.disconnectWebSocket()
}

// connect to server
func (p *WebSocketV2ClientBase) connectWebSocket() {
	var err error
	url := fmt.Sprintf("wss://%s%s", p.host, websocketV2Path)
	applogger.Debug("name=%s WebSocket connecting...", p.name)
	p.conn, _, err = websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		applogger.Error("name=%s WebSocket connected error: %s", p.name, err)
		return
	}
	applogger.Info("name=%s conn=%p WebSocket connected", p.name, p.conn)

	// start loop to read and handle message
	p.startReadLoop()

	// send authentication if connect to websocket successfully
	auth, err := p.requestBuilder.Build()
	if err != nil {
		applogger.Error("name=%s conn=%p Signature generated error: %s", p.name, p.conn, err)
		return
	}
	p.Send(auth)
	applogger.Info("name=%s conn=%p WebSocket sent authentication", p.name, p.conn)
}

// disconnect with server
func (p *WebSocketV2ClientBase) disconnectWebSocket() {
	if p.conn == nil {
		return
	}

	// start a new goroutine to send stop signal
	go p.stopReadLoop()

	applogger.Debug("name=%s conn=%p WebSocket disconnecting...", p.name, p.conn)
	err := p.conn.Close()
	if err != nil {
		applogger.Error("name=%s conn=%p WebSocket disconnect error: %s", p.name, p.conn, err)
		return
	}

	applogger.Info("name=%s conn=%p WebSocket disconnected", p.name, p.conn)
}

// initialize a ticker and start a goroutine tickerLoop()
func (p *WebSocketV2ClientBase) startTicker() {
	p.ticker = time.NewTicker(TimerIntervalSecond * time.Second)

	go p.tickerLoop()
}

// stop ticker and stop the goroutine
func (p *WebSocketV2ClientBase) stopTicker() {
	p.ticker.Stop()
	p.stopTickerChannel <- 1
}

// defines a for loop that will run based on ticker's frequency
// It checks the last data that received from server, if it is longer than the threshold,
// it will force disconnect server and connect again.
func (p *WebSocketV2ClientBase) tickerLoop() {
	applogger.Debug("name=%s conn=%p tickerLoop started", p.name, p.conn)
	for {
		select {
		// start a goroutine readLoop()
		case <-p.stopTickerChannel:
			applogger.Debug("name=%s conn=%p tickerLoop stopped", p.name, p.conn)
			return

		// Receive tick from tickChannel
		case <-p.ticker.C:
			elapsedSecond := time.Now().Sub(p.lastReceivedTime).Seconds()
			applogger.Debug("name=%s conn=%p WebSocket received data %f sec ago", p.name, p.conn, elapsedSecond)

			if elapsedSecond > ReconnectWaitSecond {
				applogger.Info("name=%s conn=%p WebSocket reconnect...", p.name, p.conn)
				p.disconnectWebSocket()
				p.connectWebSocket()
			}
		}
	}
}

// start a goroutine readLoop()
func (p *WebSocketV2ClientBase) startReadLoop() {
	go p.readLoop()
}

// stop the goroutine readLoop()
func (p *WebSocketV2ClientBase) stopReadLoop() {
	p.stopReadChannel <- 1
}

// defines a for loop to read data from server
// it will stop once it receives the signal from stopReadChannel
func (p *WebSocketV2ClientBase) readLoop() {
	applogger.Debug("name=%s conn=%p readLoop started", p.name, p.conn)
	for {
		select {
		// Receive data from stopChannel
		case <-p.stopReadChannel:
			applogger.Debug("name=%s conn=%p readLoop stopped", p.name, p.conn)
			return

		default:
			if p.conn == nil {
				applogger.Error("name=%s conn=%p Read error: no connection available", p.name, p.conn)
				time.Sleep(TimerIntervalSecond * time.Second)
				continue
			}

			msgType, buf, err := p.conn.ReadMessage()
			if err != nil {
				applogger.Error("name=%s conn=%p Read error: %s", p.name, p.conn, err)
				time.Sleep(TimerIntervalSecond * time.Second)
				continue
			}

			p.lastReceivedTime = time.Now()

			// decompress gzip data if it is binary message
			var message string
			if msgType == websocket.BinaryMessage {
				message, err = gzip.GZipDecompress(buf)
				if err != nil {
					applogger.Error("name=%s conn=%p UnGZip data error: %s", p.name, p.conn, err)
					return
				}
			} else if msgType == websocket.TextMessage {
				message = string(buf)
			}

			// Try to pass as PingV2Message
			// If it is Ping then respond Pong
			pingV2Msg := model.ParsePingV2Message(message)
			if pingV2Msg.IsPing() {
				applogger.Debug("name=%s conn=%p Received Ping: %d", p.name, p.conn, pingV2Msg.Data.Timestamp)
				pongMsg := fmt.Sprintf("{\"action\": \"pong\", \"data\": { \"ts\": %d } }", pingV2Msg.Data.Timestamp)
				p.Send(pongMsg)
				applogger.Debug("name=%s conn=%p Respond  Pong: %d", p.name, p.conn, pingV2Msg.Data.Timestamp)
			} else {
				// Try to pass as websocket v2 authentication response
				// If it is then invoke authentication handler
				wsV2Resp := base.ParseWSV2Resp(message)
				if wsV2Resp != nil {
					switch wsV2Resp.Action {
					case "req":
						authResp := auth.ParseWSV2AuthResp(message)
						if authResp != nil && p.authenticationResponseHandler != nil {
							p.authenticationResponseHandler(authResp)
						}

					case "sub", "push":
						{
							result, err := p.messageHandler(message)
							if err != nil {
								applogger.Error("name=%s conn=%p Handle message error: %s", p.name, p.conn, err)
								continue
							}
							if p.responseHandler != nil {
								p.responseHandler(result)
							}
						}
					}
				}
			}
		}
	}
}
