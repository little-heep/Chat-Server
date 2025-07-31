package user
//客户端管理
import (
	"net"
	"sync"
	"time"
)

//客户端通信的信息json
type ChatMessage struct {
    Type      string `json:"type"`
    Content   string `json:"content"`
    ReceiveID string `json:"receiveid"`
    SendTime  string `json:"sendTime"`
    SendID    string `json:"sendid"`
}


// Client 结构体用于存储客户端连接信息

// FriendInfo 好友信息结构体
type FriendInfo struct {
	UserID string `json:"user_id"`
	Name   string `json:"name"`
	Status int    `json:"status"`
	Online bool   `json:"online"`
}

// FriendListMessage 好友列表消息结构体
type FriendListMessage struct {
	Type    string       `json:"type"`
	Friends []FriendInfo `json:"friends"`
}

// Client 结构体用于存储客户端连接信息
type Client struct {
	Conn        net.Conn    `json:"-"`
	ID          string      `json:"id"`
	IP          string      `json:"ip"`
	ConnectTime time.Time   `json:"connect_time"`
	LastActive  time.Time   `json:"last_active"`
	Friends     []FriendInfo `json:"friends"`
}

// ClientManager 用于管理所有客户端连接
type ClientManager struct {
	Clients    map[string]*Client
	Mutex      sync.RWMutex
}

var Manager = &ClientManager{
	Clients: make(map[string]*Client),
}