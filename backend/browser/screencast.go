// screencast.go：/api/browser/stream 的 WebSocket 桥。
//
//	CDP  → 前端：Page.startScreencast 的 JPEG 帧（base64）+ 画面尺寸
//	前端 → CDP：鼠标/键盘/滚轮/导航（仅 ?control=1 时转发输入；默认只读镜像）
package browser

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 1 << 16,
	// 同源校验：抄 pty/stream 那套（Origin host 必须等于请求 Host）
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true
		}
		i := strings.Index(origin, "://")
		return i >= 0 && origin[i+3:] == r.Host
	},
}

// cdp 是到单个 page 目标的 CDP 连接；WriteJSON 非并发安全，故加锁串行写。
type cdp struct {
	ws *websocket.Conn
	mu sync.Mutex
	id int
}

func (c *cdp) send(method string, params map[string]any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.id++
	_ = c.ws.WriteJSON(map[string]any{"id": c.id, "method": method, "params": params})
}

func fail(front *websocket.Conn, msg string) {
	_ = front.WriteJSON(map[string]any{"type": "error", "msg": msg})
}

func atoiDefault(s string, d int) int {
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return d
}

// keyInfo 把 DOM KeyboardEvent.key 映射到 CDP 需要的 (code, windowsVirtualKeyCode, text)。
// 只覆盖非可打印的常用键；可打印字符走 Input.insertText，不经过这里。
func keyInfo(key string) (code string, vk int, text string) {
	switch key {
	case "Enter":
		return "Enter", 13, "\r"
	case "Tab":
		return "Tab", 9, "\t"
	case "Backspace":
		return "Backspace", 8, ""
	case "Delete":
		return "Delete", 46, ""
	case "Escape":
		return "Escape", 27, ""
	case "ArrowLeft":
		return "ArrowLeft", 37, ""
	case "ArrowUp":
		return "ArrowUp", 38, ""
	case "ArrowRight":
		return "ArrowRight", 39, ""
	case "ArrowDown":
		return "ArrowDown", 40, ""
	case "Home":
		return "Home", 36, ""
	case "End":
		return "End", 35, ""
	case "PageUp":
		return "PageUp", 33, ""
	case "PageDown":
		return "PageDown", 34, ""
	}
	return "", 0, ""
}

// Handler 处理 /api/browser/stream 的 WebSocket 升级与 CDP 桥接。
func Handler(c *gin.Context) {
	front, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer front.Close()

	if err := ensureChrome(); err != nil {
		fail(front, err.Error())
		return
	}
	wsURL, err := targetWS(c.Query("target")) // 空 = 第一个标签页
	if err != nil {
		fail(front, err.Error())
		return
	}
	back, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		fail(front, "连接 Chrome 失败: "+err.Error())
		return
	}
	defer back.Close()
	conn := &cdp{ws: back}
	control := c.Query("control") == "1"

	// gorilla 不支持并发写同一连接：帧 goroutine 与 pong 回包都经此串行化
	var fmu sync.Mutex
	writeFront := func(v any) error {
		fmu.Lock()
		defer fmu.Unlock()
		return front.WriteJSON(v)
	}

	quality := atoiDefault(c.Query("q"), 80) // JPEG 质量 10~100
	if quality < 10 {
		quality = 10
	} else if quality > 100 {
		quality = 100
	}
	conn.send("Page.enable", nil)
	conn.send("Page.startScreencast", map[string]any{
		"format": "jpeg", "quality": quality,
		// 放宽上限：让 2× 高 DPI 帧(≤2560×1600)原样传输，不被降采样
		"maxWidth": 2560, "maxHeight": 1600, "everyNthFrame": 1,
	})

	// CDP → 前端：转发 screencast 帧并回 ack（不 ack 则 Chrome 停止推帧）
	go func() {
		defer front.Close()
		for {
			_, data, err := back.ReadMessage()
			if err != nil {
				return
			}
			var msg struct {
				Method string `json:"method"`
				Params struct {
					Data      string `json:"data"`
					SessionID int    `json:"sessionId"`
					Metadata  struct {
						DeviceWidth  float64 `json:"deviceWidth"`
						DeviceHeight float64 `json:"deviceHeight"`
					} `json:"metadata"`
				} `json:"params"`
			}
			if json.Unmarshal(data, &msg) != nil || msg.Method != "Page.screencastFrame" {
				continue
			}
			conn.send("Page.screencastFrameAck", map[string]any{"sessionId": msg.Params.SessionID})
			if writeFront(map[string]any{
				"type": "frame",
				"data": msg.Params.Data,
				"w":    msg.Params.Metadata.DeviceWidth,
				"h":    msg.Params.Metadata.DeviceHeight,
			}) != nil {
				return
			}
		}
	}()

	// 前端 → CDP：导航任何模式都允许；鼠标/键盘仅 control 模式转发
	for {
		_, data, err := front.ReadMessage()
		if err != nil {
			return
		}
		var ev struct {
			Type      string  `json:"type"`
			Sub       string  `json:"sub"`
			X         float64 `json:"x"`
			Y         float64 `json:"y"`
			Button    string  `json:"button"`
			DeltaX    float64 `json:"deltaX"`
			DeltaY    float64 `json:"deltaY"`
			Key       string  `json:"key"`
			Text      string  `json:"text"`
			Modifiers int     `json:"modifiers"`
			URL       string  `json:"url"`
			T         float64 `json:"t"`
		}
		if json.Unmarshal(data, &ev) != nil {
			continue
		}
		if ev.Type == "ping" { // 测延迟：原样回带客户端时间戳
			writeFront(map[string]any{"type": "pong", "t": ev.T})
			continue
		}
		if ev.Type == "nav" && ev.URL != "" {
			conn.send("Page.navigate", map[string]any{"url": ev.URL})
			continue
		}
		if !control {
			continue
		}
		switch ev.Type {
		case "mouse":
			t := map[string]string{"down": "mousePressed", "up": "mouseReleased", "move": "mouseMoved"}[ev.Sub]
			if t == "" {
				continue
			}
			p := map[string]any{"type": t, "x": ev.X, "y": ev.Y, "modifiers": ev.Modifiers}
			if ev.Sub != "move" {
				btn := ev.Button
				if btn == "" {
					btn = "left"
				}
				p["button"] = btn
				p["buttons"] = 1
				p["clickCount"] = 1
			}
			conn.send("Input.dispatchMouseEvent", p)
		case "wheel":
			conn.send("Input.dispatchMouseEvent", map[string]any{
				"type": "mouseWheel", "x": ev.X, "y": ev.Y,
				"deltaX": ev.DeltaX, "deltaY": ev.DeltaY, "modifiers": ev.Modifiers,
			})
		case "key":
			// 可打印字符直接 insertText（最可靠，能写进输入框/contenteditable）
			if ev.Sub == "char" {
				if ev.Text != "" {
					conn.send("Input.insertText", map[string]any{"text": ev.Text})
				}
				continue
			}
			// 特殊键（回车/退格/方向键/带修饰键的组合）走 dispatchKeyEvent + 虚拟键码
			typ := "keyDown"
			if ev.Sub == "up" {
				typ = "keyUp"
			}
			code, vk, text := keyInfo(ev.Key)
			p := map[string]any{
				"type": typ, "key": ev.Key, "code": code,
				"windowsVirtualKeyCode": vk, "nativeVirtualKeyCode": vk,
				"modifiers": ev.Modifiers,
			}
			if text != "" {
				p["text"] = text
			}
			conn.send("Input.dispatchKeyEvent", p)
		}
	}
}
